package sub2api

import (
	"context"
	"net/http"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

// AdminReady uses the caller's JWT, never the module's machine admin key.
func (c *Client) AdminReady(ctx context.Context, access string, info ports.ClientInfo) error {
	var status struct {
		Required *bool `json:"required"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/admin/compliance", access, nil, &status, info); err != nil {
		return err
	}
	if status.Required == nil {
		return badResponse()
	}
	if *status.Required {
		return &Error{Status: http.StatusLocked, Reason: "ADMIN_COMPLIANCE_ACK_REQUIRED"}
	}
	return nil
}
