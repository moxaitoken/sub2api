package auth

import (
	"context"
	"strings"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// DoBearer validates the main site's access token on every request. It never
// creates a module session or stores the main site's access/refresh tokens.
func (s *Service) DoBearer(ctx context.Context, token string, info ports.ClientInfo, fn func(Principal) error) error {
	if !s.Administrator || s.Upstream == nil || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n") {
		return ports.ErrUnauthenticated
	}
	me, err := s.identity(ctx, ports.Tokens{AccessToken: token}, info)
	if err != nil {
		return err
	}
	// Fail closed when the adapter cannot enforce the main admin restrictions.
	gate, ok := s.Upstream.(interface {
		AdminReady(context.Context, string, ports.ClientInfo) error
	})
	if !ok {
		return ports.ErrUnavailable
	}
	if err := gate.AdminReady(ctx, token, info); err != nil {
		return err
	}
	return fn(Principal{Me: me})
}
