package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// CreationCapabilities lets extensions refuse unsafe old servers before creating resources.
func (h *AccountHandler) CreationCapabilities(c *gin.Context) {
	response.Success(c, gin.H{"schedulable_on_create": true})
}
