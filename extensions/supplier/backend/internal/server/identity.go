package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerIdentityRoutes(mux *http.ServeMux) {
	s.registerOnboarding(mux)
	s.registerUsage(mux)
	mux.HandleFunc("POST /api/supplier/v1/auth/login", s.localLogin)
	mux.HandleFunc("POST /api/supplier/v1/auth/logout", s.localLogout)
	mux.HandleFunc("GET /api/supplier/v1/me", s.localAuthorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, csrf string) error {
		s.reply(w, 200, map[string]any{"principal": map[string]string{"kind": "supplier", "id": record.Supplier.ID, "login_account": record.Login}, "supplier": record.Supplier, "csrf_token": csrf})
		return nil
	}))
	mux.HandleFunc("GET /api/supplier/v1/accounts", s.localAuthorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		return s.identityAccounts(w, r, record.Supplier.ID)
	}))
	admin := &Server{service: s.cfg.Admin, cfg: s.cfg, cookieName: "__Host-supplier_admin", challengeName: "__Host-supplier_admin_challenge", attempts: make(map[string]bucket)}
	if s.cfg.InsecureLocalhost {
		admin.cookieName = "supplier_admin"
		admin.challengeName = "supplier_admin_challenge"
	}
	admin.registerGroupAccess(mux)
	admin.registerFinance(mux)
	admin.registerAdminResources(mux)
	mux.HandleFunc("POST /api/supplier/admin/v1/auth/login", admin.login)
	mux.HandleFunc("POST /api/supplier/admin/v1/auth/login/2fa", admin.twoFA)
	mux.HandleFunc("POST /api/supplier/admin/v1/auth/logout", admin.logout)
	mux.HandleFunc("GET /api/supplier/admin/v1/me", admin.authorizeAdmin(false, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		admin.reply(w, 200, map[string]any{"principal": map[string]string{"kind": "admin", "id": strconv.FormatInt(p.Me.User.ID, 10), "email": p.Me.User.Email}, "csrf_token": p.Me.CSRFToken})
		return nil
	}))
	mux.HandleFunc("GET /api/supplier/admin/v1/suppliers", admin.authorizeAdmin(false, admin.managedSuppliers))
	mux.HandleFunc("POST /api/supplier/admin/v1/suppliers", admin.authorizeAdmin(true, admin.createSupplier))
	mux.HandleFunc("GET /api/supplier/admin/v1/suppliers/{id}", admin.authorizeAdmin(false, admin.managedSupplier))
	mux.HandleFunc("PUT /api/supplier/admin/v1/suppliers/{id}/status", admin.authorizeAdmin(true, admin.supplierChange(false)))
	mux.HandleFunc("POST /api/supplier/admin/v1/suppliers/{id}/credential-reset", admin.authorizeAdmin(true, admin.supplierChange(true)))
	mux.HandleFunc("GET /api/supplier/admin/v1/suppliers/{id}/accounts", admin.authorizeAdmin(false, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		if !uuid.MatchString(id) {
			return ports.ErrNotFound
		}
		if _, err := s.cfg.Suppliers.Store.ManagedSupplier(r.Context(), id); err != nil {
			return err
		}
		return s.identityAccounts(w, r, id)
	}))
}
func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	info, err := s.client(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.allowLogin("local/ip/" + info.IP) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, problem(429, "LOGIN_RATE_LIMIT"))
		return
	}
	var in struct {
		Login    string `json:"login_account"`
		Password string `json:"password"`
	}
	if err = decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	normalized, _ := auth.NormalizeLogin(in.Login)
	if !s.allowLogin("local/account/" + auth.Hash(normalized)) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, problem(429, "LOGIN_RATE_LIMIT"))
		return
	}
	result, err := s.service.LocalLogin(r.Context(), in.Login, in.Password, info)
	in.Password = ""
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loginReply(w, result)
}
func (s *Server) localAuthorize(write bool, fn func(http.ResponseWriter, *http.Request, ports.SupplierRecord, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := s.client(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		err = s.service.LocalDo(r.Context(), s.cookie(r, s.cookieName), r.Header.Get("X-CSRF-Token"), write, info, func(record ports.SupplierRecord, csrf string) error { return fn(w, r, record, csrf) })
		if err != nil {
			s.fail(w, r, err)
		}
	}
}
func (s *Server) localLogout(w http.ResponseWriter, r *http.Request) {
	cookie := s.cookie(r, s.cookieName)
	if cookie == "" {
		s.clearCookie(w, s.cookieName)
		s.reply(w, 200, map[string]bool{"logged_out": true})
		return
	}
	info, err := s.client(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err = s.service.LocalLogout(r.Context(), cookie, r.Header.Get("X-CSRF-Token"), info); err != nil {
		s.fail(w, r, err)
		return
	}
	s.clearCookie(w, s.cookieName)
	s.reply(w, 200, map[string]bool{"logged_out": true})
}
func (s *Server) identityAccounts(w http.ResponseWriter, r *http.Request, id string) error {
	if s.cfg.Accounts == nil || s.cfg.Accounts.Store == nil {
		return problem(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
	}
	page, size, err := accountPage(r)
	if err != nil {
		return err
	}
	rows, total, err := s.cfg.Accounts.Store.Accounts(r.Context(), id, page, size)
	if err != nil {
		return err
	}
	if r.URL.Query().Get("include_metrics") == "1" {
		s.cfg.Accounts.EnrichAccounts(r.Context(), id, rows)
	}
	s.reply(w, 200, map[string]any{"items": rows, "total": total, "page": page, "page_size": size, "pages": (total + int64(size) - 1) / int64(size)})
	return nil
}
func (s *Server) managedSuppliers(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
	page, size, err := accountPage(r)
	if err != nil {
		return err
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if len([]rune(search)) > 100 {
		return problem(400, "INVALID_REQUEST")
	}
	rows, total, err := s.cfg.Suppliers.Store.ManagedSuppliers(r.Context(), search, page, size)
	if err != nil {
		return err
	}
	s.reply(w, 200, map[string]any{"items": rows, "total": total, "page": page, "page_size": size, "pages": (total + int64(size) - 1) / int64(size)})
	return nil
}
func (s *Server) managedSupplier(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
	id := r.PathValue("id")
	if !uuid.MatchString(id) {
		return ports.ErrNotFound
	}
	row, err := s.cfg.Suppliers.Store.ManagedSupplier(r.Context(), id)
	if err != nil {
		return err
	}
	s.reply(w, 200, row)
	return nil
}
func (s *Server) createSupplier(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	key, err := accountKey(r)
	if err != nil {
		return err
	}
	var in struct {
		Name     string `json:"name"`
		Login    string `json:"login_account"`
		Password string `json:"initial_password"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	out, replay, err := s.cfg.Suppliers.Mutate(r.Context(), p.Me.User.ID, "supplier.create", "", key, in.Name, in.Login, in.Password, "", 0)
	in.Password = ""
	if err != nil {
		return err
	}
	status := 201
	if replay {
		status = 200
	}
	s.reply(w, status, out)
	return nil
}
func (s *Server) supplierChange(reset bool) func(http.ResponseWriter, *http.Request, auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		if !uuid.MatchString(id) {
			return ports.ErrNotFound
		}
		key, err := accountKey(r)
		if err != nil {
			return err
		}
		version, err := accountVersion(r)
		if err != nil {
			return err
		}
		action, password, status := "supplier.status", "", ""
		if reset {
			var in struct {
				Password string `json:"new_password"`
			}
			if err = decode(w, r, &in); err != nil {
				return err
			}
			action = "supplier.reset"
			password = in.Password
		} else {
			var in struct {
				Enabled *bool `json:"enabled"`
			}
			if err = decode(w, r, &in); err != nil {
				return err
			}
			if in.Enabled == nil {
				return problem(400, "INVALID_REQUEST")
			}
			status = "disabled"
			if *in.Enabled {
				status = "active"
			}
		}
		out, _, err := s.cfg.Suppliers.Mutate(r.Context(), p.Me.User.ID, action, id, key, "", "", password, status, version)
		password = ""
		if err != nil {
			return err
		}
		s.reply(w, 200, out)
		return nil
	}
}
