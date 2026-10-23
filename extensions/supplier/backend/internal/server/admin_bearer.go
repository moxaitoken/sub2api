package server

import (
	"net/http"
	"strings"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
)

func (s *Server) authorizeAdmin(write bool, fn func(http.ResponseWriter, *http.Request, auth.Principal) error) http.HandlerFunc {
	legacy := s.authorize(write, fn)
	return func(w http.ResponseWriter, r *http.Request) {
		if _, present := r.Header["Authorization"]; !present {
			legacy(w, r)
			return
		}
		// An invalid explicit Bearer credential must never fall back to a cookie.
		parts := strings.Split(r.Header.Get("Authorization"), " ")
		if len(r.Header.Values("Authorization")) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			s.fail(w, r, problem(401, "AUTHENTICATION_REQUIRED"))
			return
		}
		info, err := s.client(r)
		if err == nil {
			err = s.service.DoBearer(r.Context(), parts[1], info, func(p auth.Principal) error { return fn(w, r, p) })
		}
		if err != nil {
			s.fail(w, r, err)
		}
	}
}
