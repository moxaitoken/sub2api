package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
)

func (s *Server) registerUsage(mux *http.ServeMux) {
	s.registerResourceUsage(mux, "/api/supplier/v1", s.localAuthorize)
}
func (s *Server) registerResourceUsage(mux *http.ServeMux, prefix string, authorize resourceAuthorizer) {
	mux.HandleFunc("GET "+prefix+"/usage", authorize(false, func(w http.ResponseWriter, r *http.Request, record ports.SupplierRecord, _ string) error {
		if s.cfg.Usage == nil {
			return ports.ErrUnavailable
		}
		// A supplier identifier is never accepted from the browser.
		for k, values := range r.URL.Query() {
			if len(values) != 1 || (k != "period" && k != "account_id" && k != "page" && k != "page_size") {
				return problem(400, "INVALID_REQUEST")
			}
		}
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "today"
		}
		if period != "today" && period != "7d" && period != "30d" && period != "all" {
			return problem(400, "INVALID_REQUEST")
		}
		id := r.URL.Query().Get("account_id")
		if id != "" && !uuid.MatchString(id) {
			return ports.ErrNotFound
		}
		page, size, e := accountPage(r)
		if e != nil {
			return e
		}
		out, e := s.cfg.Usage.Read(r.Context(), record.Supplier.ID, ports.UsageQuery{Period: period, AccountID: id, Page: page, Size: size})
		if e != nil {
			return e
		}
		s.reply(w, 200, out)
		return nil
	}))
}
