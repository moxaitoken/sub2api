package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type accountRuntimeSample struct {
	RPM            *int    `json:"rpm"`
	Concurrency    *int    `json:"concurrency"`
	Tokens         *string `json:"tokens"`
	UncachedTokens *string `json:"uncached_tokens"`
}

// GetRuntimeBatch is a read-only administrator projection. Unlike the legacy
// account DTO, failed Redis/DB reads are nil rather than false zero values.
func (h *AccountHandler) GetRuntimeBatch(c *gin.Context) {
	var input struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.AccountIDs) == 0 || len(input.AccountIDs) > 100 {
		response.BadRequest(c, "Expected 1 to 100 account_ids")
		return
	}
	for _, id := range input.AccountIDs {
		if id <= 0 {
			response.BadRequest(c, "Invalid account_id")
			return
		}
	}
	ids := normalizeInt64IDList(input.AccountIDs)
	if h.adminService == nil {
		response.Error(c, 503, "Account runtime unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	accounts, err := h.adminService.GetAccountsByIDs(ctx, ids)
	if err != nil {
		response.Error(c, 503, "Account runtime unavailable")
		return
	}
	now := time.Now().UTC()
	values := map[string]*accountRuntimeSample{}
	valid, rpmIDs := []int64{}, []int64{}
	allowed := map[int64]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	for _, a := range accounts {
		if a == nil || !allowed[a.ID] || !a.IsAnthropicOAuthOrSetupToken() {
			continue
		}
		valid = append(valid, a.ID)
		values[strconv.FormatInt(a.ID, 10)] = &accountRuntimeSample{}
		if a.GetBaseRPM() > 0 {
			rpmIDs = append(rpmIDs, a.ID)
		}
	}
	if h.concurrencyService != nil {
		if counts, e := h.concurrencyService.GetRuntimeConcurrency(ctx, valid); e == nil {
			for id, value := range counts {
				if v := values[strconv.FormatInt(id, 10)]; v != nil && value >= 0 {
					n := value
					v.Concurrency = &n
				}
			}
		}
	}
	if reader, ok := h.rpmCache.(service.RuntimeRPMReader); ok && len(rpmIDs) > 0 {
		if counts, e := reader.GetRuntimeRPM(ctx, rpmIDs); e == nil {
			// A slow Redis read spanning the minute boundary cannot be labelled
			// with this sample's earlier minute. Preserve unknown instead.
			if !time.Now().UTC().Truncate(time.Minute).Equal(now.Truncate(time.Minute)) {
				counts = nil
			}
			for id, value := range counts {
				if v := values[strconv.FormatInt(id, 10)]; v != nil && value >= 0 {
					n := value
					v.RPM = &n
				}
			}
		}
	}
	if h.accountUsageService != nil {
		if counts, e := h.accountUsageService.GetAccountRuntimeTokens(ctx, valid, now.Add(-time.Minute), now); e == nil {
			for id, value := range counts {
				if v := values[strconv.FormatInt(id, 10)]; v != nil {
					v.Tokens = &value.Tokens
					v.UncachedTokens = &value.UncachedTokens
				}
			}
		}
	}
	response.Success(c, gin.H{"sampled_at": now, "rpm_window": "current_minute", "token_window_seconds": 60, "accounts": values})
}
