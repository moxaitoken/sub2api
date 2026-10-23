package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
)

func (s *Server) registerAdminResources(mux *http.ServeMux) {
	authorize := func(write bool, fn resourceHandler) http.HandlerFunc {
		return s.authorizeAdmin(write, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			id := r.PathValue("supplier_id")
			if !uuid.MatchString(id) {
				return ports.ErrNotFound
			}
			record, e := s.cfg.Suppliers.Store.LocalSupplier(r.Context(), id)
			if e != nil {
				return e
			}
			// Resource ownership remains the supplier; the audited operator is the real admin.
			return fn(w, r.WithContext(ports.WithAdminActor(r.Context(), p.Me.User.ID)), record, "")
		})
	}
	prefix := "/api/supplier/admin/v1/suppliers/{supplier_id}"
	s.registerResourceOnboarding(mux, prefix, authorize)
	s.registerResourceUsage(mux, prefix, authorize)
}
