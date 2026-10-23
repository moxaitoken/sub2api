package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
)

type resourceHandler = func(http.ResponseWriter, *http.Request, ports.SupplierRecord, string) error
type resourceAuthorizer = func(bool, resourceHandler) http.HandlerFunc

func (s *Server) registerOnboarding(mux *http.ServeMux) {
	s.registerResourceOnboarding(mux, "/api/supplier/v1", s.localAuthorize)
}
func (s *Server) registerResourceOnboarding(mux *http.ServeMux, prefix string, authorize resourceAuthorizer) {
	mux.HandleFunc("DELETE "+prefix+"/proxies/{id}", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		if e := s.cfg.Accounts.DeleteOwnProxy(r.Context(), record, r.PathValue("id")); e != nil {
			return e
		}
		s.reply(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("GET "+prefix+"/onboarding/options", authorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		groups, e := s.cfg.Accounts.Groups(r.Context())
		if e != nil {
			return e
		}
		proxies, e := s.cfg.Accounts.OwnProxies(r.Context(), record.Supplier.ID)
		if e != nil {
			return e
		}
		s.reply(w, 200, map[string]any{"groups": groups, "proxies": proxies})
		return nil
	}))
	mux.HandleFunc("GET "+prefix+"/proxies", authorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		items, e := s.cfg.Accounts.OwnProxies(r.Context(), record.Supplier.ID)
		if e != nil {
			return e
		}
		s.reply(w, 200, map[string]any{"items": items})
		return nil
	}))
	mux.HandleFunc("POST "+prefix+"/proxies", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		key, e := accountKey(r)
		if e != nil {
			return e
		}
		var in ports.ProxyInput
		if e = decode(w, r, &in); e != nil {
			return e
		}
		p, e := s.cfg.Accounts.AddProxy(r.Context(), record, in, key)
		in.Address = ""
		if e != nil {
			return e
		}
		s.reply(w, 201, p)
		return nil
	}))
	mux.HandleFunc("POST "+prefix+"/proxies/{id}/test", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		p, e := s.cfg.Accounts.RetestOwnProxy(r.Context(), record.Supplier.ID, r.PathValue("id"))
		if e != nil {
			return e
		}
		s.reply(w, 200, p)
		return nil
	}))
	mux.HandleFunc("POST "+prefix+"/oauth/sessions", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		key, e := accountKey(r)
		if e != nil {
			return e
		}
		var in ports.BeginOnboarding
		if e = decode(w, r, &in); e != nil {
			return e
		}
		if in.ProxyID != "" && !uuid.MatchString(in.ProxyID) {
			return problem(400, "INVALID_PROXY")
		}
		out, e := s.cfg.Accounts.BeginLocal(r.Context(), record, in, key)
		if e != nil {
			return e
		}
		s.reply(w, 201, out)
		return nil
	}))
	mux.HandleFunc("POST "+prefix+"/oauth/sessions/{id}/complete", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		id := r.PathValue("id")
		if !uuid.MatchString(id) {
			return ports.ErrNotFound
		}
		key, e := accountKey(r)
		if e != nil {
			return e
		}
		var in struct {
			Code string `json:"authorization_code"`
		}
		if e = decode(w, r, &in); e != nil {
			return e
		}
		out, e := s.cfg.Accounts.CompleteLocal(r.Context(), record.Supplier.ID, id, in.Code, key)
		in.Code = ""
		if out.ID != "" {
			s.reply(w, 202, out)
			return nil
		}
		if e != nil {
			return e
		}
		return ports.ErrUnavailable
	}))
	mux.HandleFunc("DELETE "+prefix+"/oauth/sessions/{id}", authorize(true, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		store, ok := s.cfg.Accounts.Store.(ports.OnboardingStore)
		if !ok {
			return ports.ErrUnavailable
		}
		if e := store.CancelOnboarding(r.Context(), record.Supplier.ID, r.PathValue("id")); e != nil {
			return e
		}
		s.reply(w, 200, map[string]bool{"cancelled": true})
		return nil
	}))
	mux.HandleFunc("GET "+prefix+"/operations/{id}", authorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if e := s.accountReady(); e != nil {
			return e
		}
		if !uuid.MatchString(r.PathValue("id")) {
			return ports.ErrNotFound
		}
		store, ok := s.cfg.Accounts.Store.(ports.OnboardingStore)
		if !ok {
			return ports.ErrUnavailable
		}
		op, e := store.LocalAccountOperation(r.Context(), record.Supplier.ID, r.PathValue("id"))
		if e != nil {
			return e
		}
		s.reply(w, 200, op)
		return nil
	}))
}
