package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
	"unicode/utf8"
)

func (s *Server) registerFinance(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/supplier/admin/v1/suppliers/{id}/settlements", s.authorizeAdmin(true, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		key, e := accountKey(r)
		if e != nil {
			return e
		}
		var in ports.SettlementInput
		if e = decode(w, r, &in); e != nil {
			return e
		}
		if s.cfg.Finance == nil {
			return ports.ErrUnavailable
		}
		out, replay, e := s.cfg.Finance.Settle(r.Context(), p.Me.User.ID, r.PathValue("id"), key, in)
		if e != nil {
			return e
		}
		status := 201
		if replay {
			status = 200
		}
		s.reply(w, status, out)
		return nil
	}))
	mux.HandleFunc("GET /api/supplier/admin/v1/finance/ledger", s.authorizeAdmin(false, func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
		for k, v := range r.URL.Query() {
			if len(v) != 1 || (k != "supplier_id" && k != "search" && k != "page" && k != "page_size") {
				return problem(400, "INVALID_REQUEST")
			}
		}
		id := r.URL.Query().Get("supplier_id")
		if id != "" {
			if !uuid.MatchString(id) {
				return ports.ErrNotFound
			}
			if _, e := s.cfg.Suppliers.Store.ManagedSupplier(r.Context(), id); e != nil {
				return e
			}
		}
		page, size, e := accountPage(r)
		if e != nil {
			return e
		}
		search := r.URL.Query().Get("search")
		if utf8.RuneCountInString(search) > 200 {
			return problem(400, "INVALID_REQUEST")
		}
		if s.cfg.Finance == nil {
			return ports.ErrUnavailable
		}
		out, e := s.cfg.Finance.Store.Finance(r.Context(), ports.FinanceQuery{SupplierID: id, Search: search, Page: page, Size: size})
		if e != nil {
			return e
		}
		s.reply(w, 200, out)
		return nil
	}))
	mux.HandleFunc("GET /api/supplier/admin/v1/finance/settlements/{id}", s.authorizeAdmin(false, func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		if s.cfg.Finance == nil {
			return ports.ErrUnavailable
		}
		out, e := s.cfg.Finance.Store.Settlement(r.Context(), r.PathValue("id"))
		if e != nil {
			return e
		}
		s.reply(w, 200, out)
		return nil
	}))
}
