package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerGroupAccess(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/supplier/admin/v1/groups/access", s.authorizeAdmin(false, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if p.Me.User.Role != "admin" {
			return ports.ErrForbidden
		}
		if s.cfg.Accounts == nil || s.cfg.Accounts.Store == nil {
			return ports.ErrUnavailable
		}
		store, ok := s.cfg.Accounts.Store.(ports.GroupAccessLister)
		if !ok {
			return ports.ErrUnavailable
		}
		items, e := store.ListGroupAccess(r.Context())
		if e != nil {
			return e
		}
		s.reply(w, 200, map[string]any{"items": items})
		return nil
	}))
	handler := func(write bool) http.HandlerFunc {
		return s.authorizeAdmin(write, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			if p.Me.User.Role != "admin" {
				return ports.ErrForbidden
			}
			if s.cfg.Accounts == nil {
				return ports.ErrUnavailable
			}
			store, ok := s.cfg.Accounts.Store.(ports.GroupAccessStore)
			if !ok {
				return ports.ErrUnavailable
			}
			id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if e != nil || id <= 0 {
				return problem(400, "INVALID_GROUP")
			}
			if !write {
				x, e := store.GroupAccess(r.Context(), id)
				if e != nil {
					return e
				}
				s.reply(w, 200, x)
				return nil
			}
			var in struct {
				Enabled           *bool   `json:"enabled"`
				SupplierGroupName *string `json:"supplier_group_name"`
			}
			if e = decode(w, r, &in); e != nil {
				return e
			}
			if in.Enabled == nil {
				return problem(400, "INVALID_REQUEST")
			}
			current, e := store.GroupAccess(r.Context(), id)
			if e != nil {
				return e
			}
			name := current.SupplierGroupName
			if in.SupplierGroupName != nil {
				name = strings.TrimSpace(*in.SupplierGroupName)
				if len([]rune(name)) > 100 {
					return problem(400, "INVALID_SUPPLIER_GROUP_NAME")
				}
			}
			version, e := strconv.ParseInt(r.Header.Get("If-Match"), 10, 64)
			if e != nil || version < 0 {
				return problem(400, "VERSION_REQUIRED")
			}
			up, ok := s.cfg.Accounts.Upstream.(ports.OnboardingUpstream)
			if !ok {
				return ports.ErrUnavailable
			}
			if *in.Enabled {
				g, e := up.AccountGroup(r.Context(), id)
				if e != nil {
					return e
				}
				if g.ID != id || g.Platform != "anthropic" || g.Status != "active" {
					return problem(400, "GROUP_NOT_ELIGIBLE")
				}
			}
			x, e := store.SetGroupAccess(r.Context(), ports.GroupAccess{GroupID: id, Enabled: *in.Enabled, Version: version, SupplierGroupName: name}, p.Me.User.ID)
			if e != nil {
				return e
			}
			s.reply(w, 200, x)
			return nil
		})
	}
	mux.HandleFunc("GET /api/supplier/admin/v1/groups/{id}/access", handler(false))
	mux.HandleFunc("PUT /api/supplier/admin/v1/groups/{id}/access", handler(true))
}
