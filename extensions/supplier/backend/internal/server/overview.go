package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/http"
)

func (s *Server) accountOverview(w http.ResponseWriter, r *http.Request, supplier string) error {
	if s.cfg.Accounts == nil {
		return ports.ErrUnavailable
	}
	value, err := s.cfg.Accounts.Overview(r.Context(), supplier)
	if err != nil {
		return err
	}
	s.reply(w, 200, value)
	return nil
}
