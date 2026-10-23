package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// This fork must be upgraded through tested images built from our repository.
// Block before handlers (including idempotent replay) can replace its binary.
// Keep upstream update internals intact to minimize future merge conflicts.
func denyInPlaceBinaryReplacement(c *gin.Context) {
	response.ErrorWithDetails(c, http.StatusForbidden,
		"In-place updates and rollbacks are disabled for this custom build; deploy a tested image built from our repository.",
		"IN_PLACE_UPDATE_DISABLED", nil)
	c.Abort()
}
