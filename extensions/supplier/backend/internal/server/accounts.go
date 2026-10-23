package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func accountPage(r *http.Request) (int, int, error) {
	page, size := 1, 20
	for name, dest := range map[string]*int{"page": &page, "page_size": &size} {
		if raw := r.URL.Query().Get(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				return 0, 0, problem(400, "INVALID_PAGE")
			}
			*dest = n
		}
	}
	if size > 100 || page > 100000 {
		return 0, 0, problem(400, "INVALID_PAGE")
	}
	return page, size, nil
}
func accountKey(r *http.Request) (string, error) {
	key := r.Header.Get("Idempotency-Key")
	if !uuid.MatchString(key) {
		return "", problem(400, "IDEMPOTENCY_KEY_REQUIRED")
	}
	return strings.ToLower(key), nil
}
func accountVersion(r *http.Request) (int64, error) {
	raw := strings.Trim(r.Header.Get("If-Match"), `"`)
	if raw == "" {
		return 0, problem(428, "IF_MATCH_REQUIRED")
	}
	v, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || v <= 0 {
		return 0, problem(400, "INVALID_VERSION")
	}
	return v, nil
}
func (s *Server) accountReady() error {
	if s.cfg.Accounts == nil {
		return problem(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
	}
	return nil
}
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if err := s.accountReady(); err != nil {
		return err
	}
	page, size, err := accountPage(r)
	if err != nil {
		return err
	}
	items, total, err := s.cfg.Accounts.Store.Accounts(r.Context(), p.Me.Supplier.ID, page, size)
	if err != nil {
		return err
	}
	s.reply(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "pages": (total + int64(size) - 1) / int64(size)})
	return nil
}
func (s *Server) beginOAuth(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if err := s.accountReady(); err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	result, err := s.cfg.Accounts.Begin(r.Context(), p.Me.Supplier.ID, p.Me.User.ID, in.Name)
	if err != nil {
		return err
	}
	s.reply(w, 201, result)
	return nil
}
func (s *Server) completeOAuth(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if err := s.accountReady(); err != nil {
		return err
	}
	id := r.PathValue("id")
	if !uuid.MatchString(id) {
		return ports.ErrNotFound
	}
	key, err := accountKey(r)
	if err != nil {
		return err
	}
	var in struct {
		Code string `json:"authorization_code"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	result, err := s.cfg.Accounts.Complete(r.Context(), p.Me.Supplier.ID, p.Me.User.ID, id, in.Code, key)
	in.Code = ""
	if result.ID != "" {
		s.reply(w, 202, result)
		return nil
	}
	if err != nil {
		return err
	}
	return ports.ErrUnavailable
}
func (s *Server) reviewAccounts(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if p.Me.User.Role != "admin" {
		return ports.ErrForbidden
	}
	if err := s.accountReady(); err != nil {
		return err
	}
	page, size, err := accountPage(r)
	if err != nil {
		return err
	}
	items, total, err := s.cfg.Accounts.Store.ReviewAccounts(r.Context(), page, size)
	if err != nil {
		return err
	}
	s.reply(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "pages": (total + int64(size) - 1) / int64(size)})
	return nil
}
func (s *Server) changeAccount(approve bool) func(http.ResponseWriter, *http.Request, auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if approve && p.Me.User.Role != "admin" {
			return ports.ErrForbidden
		}
		if err := s.accountReady(); err != nil {
			return err
		}
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
		var in struct {
			Reason string `json:"reason"`
		}
		if err = decode(w, r, &in); err != nil {
			return err
		}
		if len([]rune(in.Reason)) > 500 {
			return problem(400, "INVALID_REQUEST")
		}
		info, err := s.client(r)
		if err != nil {
			return err
		}
		verify := func(ctx context.Context) error {
			profile, err := s.service.Upstream.Profile(ctx, p.Tokens.AccessToken, info)
			if err != nil {
				return err
			}
			if profile.ID != p.Me.User.ID || profile.Role != "admin" || profile.Status != "active" {
				return ports.ErrForbidden
			}
			return nil
		}
		result, err := s.cfg.Accounts.Change(r.Context(), p.Me.Supplier.ID, p.Me.User, id, key, version, approve, in.Reason, verify)
		if result.ID != "" {
			s.reply(w, 202, result)
			return nil
		}
		if err != nil {
			return err
		}
		return ports.ErrUnavailable
	}
}
func (s *Server) getAccountOperation(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if err := s.accountReady(); err != nil {
		return err
	}
	id := r.PathValue("id")
	if !uuid.MatchString(id) {
		return ports.ErrNotFound
	}
	result, err := s.cfg.Accounts.Store.AccountOperation(r.Context(), p.Me.Supplier.ID, p.Me.User.ID, id)
	if err != nil {
		return err
	}
	s.reply(w, 200, result)
	return nil
}
