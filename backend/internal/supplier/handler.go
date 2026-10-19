package supplier

import (
	"context"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

const contextKeySupplier = "supplier.current"

// Handler serves both the supplier console (/supplier) and its admin side.
type Handler struct {
	svc *Service
}

// NewHandler builds the HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RequireSupplier resolves the authenticated user to an active supplier. It must run after
// JWT authentication.
func (h *Handler) RequireSupplier(c *gin.Context) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		c.Abort()
		return
	}
	sup, err := h.svc.ResolveActive(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	c.Set(contextKeySupplier, sup)
	c.Next()
}

func currentSupplier(c *gin.Context) *Supplier {
	sup, _ := c.MustGet(contextKeySupplier).(*Supplier)
	return sup
}

// ---------------------------------------------------------------------------
// Supplier console
// ---------------------------------------------------------------------------

// Me returns the current supplier profile.
// GET /api/v1/supplier/me
func (h *Handler) Me(c *gin.Context) {
	response.Success(c, currentSupplier(c))
}

// ListProxies GET /api/v1/supplier/proxies
func (h *Handler) ListProxies(c *gin.Context) {
	proxies, err := h.svc.ListProxies(c.Request.Context(), currentSupplier(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, proxies)
}

type proxyRequest struct {
	Name     string  `json:"name"`
	Protocol string  `json:"protocol" binding:"omitempty,oneof=http https socks5 socks5h"`
	Host     string  `json:"host"`
	Port     int     `json:"port" binding:"omitempty,min=1,max=65535"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

func (r proxyRequest) input() ProxyInput {
	return ProxyInput{
		Name:     strings.TrimSpace(r.Name),
		Protocol: r.Protocol,
		Host:     strings.TrimSpace(r.Host),
		Port:     r.Port,
		Username: r.Username,
		Password: r.Password,
	}
}

// CreateProxy POST /api/v1/supplier/proxies
func (h *Handler) CreateProxy(c *gin.Context) {
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || req.Protocol == "" || strings.TrimSpace(req.Host) == "" || req.Port == 0 {
		response.BadRequest(c, "name, protocol, host and port are required")
		return
	}
	proxy, err := h.svc.CreateProxy(c.Request.Context(), currentSupplier(c), req.input())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, proxy)
}

// UpdateProxy PUT /api/v1/supplier/proxies/:id
func (h *Handler) UpdateProxy(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxy, err := h.svc.UpdateProxy(c.Request.Context(), currentSupplier(c), id, req.input())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, proxy)
}

// DeleteProxy DELETE /api/v1/supplier/proxies/:id
func (h *Handler) DeleteProxy(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteProxy(c.Request.Context(), currentSupplier(c), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// TestProxy POST /api/v1/supplier/proxies/:id/test
func (h *Handler) TestProxy(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.TestProxy(c.Request.Context(), currentSupplier(c), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListAccounts GET /api/v1/supplier/accounts
func (h *Handler) ListAccounts(c *gin.Context) {
	accounts, err := h.svc.ListAccounts(c.Request.Context(), currentSupplier(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, accounts)
}

type authURLRequest struct {
	Mode    string `json:"mode" binding:"required,oneof=oauth setup-token"`
	ProxyID *int64 `json:"proxy_id"`
}

// GenerateAuthURL POST /api/v1/supplier/accounts/auth-url
func (h *Handler) GenerateAuthURL(c *gin.Context) {
	var req authURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.svc.StartAuth(c.Request.Context(), currentSupplier(c), req.Mode, nonZero(req.ProxyID))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type exchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name"`
	ProxyID   *int64 `json:"proxy_id"`
	AccountID int64  `json:"account_id"`
}

// ExchangeCode POST /api/v1/supplier/accounts/exchange-code
// Creates a new account, or re-authorizes account_id when set.
func (h *Handler) ExchangeCode(c *gin.Context) {
	var req exchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.svc.CompleteAuth(c.Request.Context(), currentSupplier(c), AuthInput{
		SessionID: req.SessionID,
		Code:      strings.TrimSpace(req.Code),
		Name:      req.Name,
		ProxyID:   nonZero(req.ProxyID),
		AccountID: req.AccountID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, account)
}

type cookieAuthRequest struct {
	Mode       string `json:"mode" binding:"required,oneof=oauth setup-token"`
	SessionKey string `json:"session_key" binding:"required"`
	Name       string `json:"name"`
	ProxyID    *int64 `json:"proxy_id"`
	AccountID  int64  `json:"account_id"`
}

// CookieAuth POST /api/v1/supplier/accounts/cookie-auth
func (h *Handler) CookieAuth(c *gin.Context) {
	var req cookieAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.svc.CookieAuth(c.Request.Context(), currentSupplier(c), AuthInput{
		Mode:       req.Mode,
		SessionKey: strings.TrimSpace(req.SessionKey),
		Name:       req.Name,
		ProxyID:    nonZero(req.ProxyID),
		AccountID:  req.AccountID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, account)
}

type updateAccountRequest struct {
	Name       string `json:"name"`
	ProxyID    *int64 `json:"proxy_id"`
	ClearProxy bool   `json:"clear_proxy"`
}

// UpdateAccount PUT /api/v1/supplier/accounts/:id
func (h *Handler) UpdateAccount(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.svc.UpdateAccount(c.Request.Context(), currentSupplier(c), id, AccountUpdate{
		Name:       req.Name,
		ProxyID:    nonZero(req.ProxyID),
		ClearProxy: req.ClearProxy,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, account)
}

// DeleteAccount DELETE /api/v1/supplier/accounts/:id
func (h *Handler) DeleteAccount(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteAccount(c.Request.Context(), currentSupplier(c), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// UsageSummary GET /api/v1/supplier/usage/summary
func (h *Handler) UsageSummary(c *gin.Context) {
	h.usageSummary(c, currentSupplier(c).ID)
}

// UsageDaily GET /api/v1/supplier/usage/daily
func (h *Handler) UsageDaily(c *gin.Context) {
	h.usageDaily(c, currentSupplier(c).ID)
}

// UsageByAccount GET /api/v1/supplier/usage/accounts
func (h *Handler) UsageByAccount(c *gin.Context) {
	h.usageByAccount(c, currentSupplier(c).ID)
}

// ListSettlements GET /api/v1/supplier/settlements
func (h *Handler) ListSettlements(c *gin.Context) {
	h.listSettlements(c, currentSupplier(c).ID)
}

// GetSettlement GET /api/v1/supplier/settlements/:id
func (h *Handler) GetSettlement(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	st, err := h.svc.GetSettlement(c.Request.Context(), currentSupplier(c).ID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, st)
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

// AdminListSuppliers GET /api/v1/admin/suppliers
func (h *Handler) AdminListSuppliers(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.svc.ListSuppliers(c.Request.Context(),
		pagination.PaginationParams{Page: page, PageSize: pageSize}, c.Query("search"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

// AdminGetSupplier GET /api/v1/admin/suppliers/:id
func (h *Handler) AdminGetSupplier(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	sup, err := h.svc.GetSupplier(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sup)
}

type supplierRequest struct {
	UserID             int64  `json:"user_id"`
	Email              string `json:"email"`
	Name               string `json:"name" binding:"required"`
	Status             string `json:"status"`
	DefaultConcurrency int    `json:"default_concurrency"`
	MaxAccounts        int    `json:"max_accounts"`
	Notes              string `json:"notes"`
}

// AdminCreateSupplier POST /api/v1/admin/suppliers
// The user is identified by user_id or email.
func (h *Handler) AdminCreateSupplier(c *gin.Context) {
	var req supplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sup, err := h.svc.CreateSupplier(c.Request.Context(), SupplierInput{
		UserID:             req.UserID,
		Email:              req.Email,
		Name:               req.Name,
		DefaultConcurrency: req.DefaultConcurrency,
		MaxAccounts:        req.MaxAccounts,
		Notes:              req.Notes,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sup)
}

// AdminUpdateSupplier PUT /api/v1/admin/suppliers/:id
func (h *Handler) AdminUpdateSupplier(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var req supplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sup, err := h.svc.UpdateSupplier(c.Request.Context(), id, SupplierInput{
		Name:               req.Name,
		Status:             req.Status,
		DefaultConcurrency: req.DefaultConcurrency,
		MaxAccounts:        req.MaxAccounts,
		Notes:              req.Notes,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sup)
}

// AdminListAccounts GET /api/v1/admin/supplier-accounts?supplier_id=&review_status=
func (h *Handler) AdminListAccounts(c *gin.Context) {
	supplierID, _ := strconv.ParseInt(c.Query("supplier_id"), 10, 64)
	reviewStatus := c.Query("review_status")
	if reviewStatus != "" && reviewStatus != ReviewPending && reviewStatus != ReviewApproved && reviewStatus != ReviewRejected {
		response.BadRequest(c, "invalid review_status")
		return
	}
	accounts, err := h.svc.ListSupplierAccounts(c.Request.Context(), supplierID, reviewStatus)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, accounts)
}

type reviewRequest struct {
	Action         string   `json:"action" binding:"required,oneof=approve reject"`
	Note           string   `json:"note"`
	GroupIDs       *[]int64 `json:"group_ids"`
	Concurrency    *int     `json:"concurrency" binding:"omitempty,min=1"`
	Priority       *int     `json:"priority" binding:"omitempty,min=0"`
	RateMultiplier *float64 `json:"rate_multiplier" binding:"omitempty,min=0"`
}

// AdminReviewAccount POST /api/v1/admin/supplier-accounts/:id/review
func (h *Handler) AdminReviewAccount(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var req reviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	err := h.svc.ReviewAccount(c.Request.Context(), adminID(c), id, ReviewInput{
		Action:         req.Action,
		Note:           req.Note,
		GroupIDs:       req.GroupIDs,
		Concurrency:    req.Concurrency,
		Priority:       req.Priority,
		RateMultiplier: req.RateMultiplier,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"reviewed": true})
}

type attachRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required,min=1,max=500"`
}

// AdminAttachAccounts POST /api/v1/admin/suppliers/:id/attach-accounts
func (h *Handler) AdminAttachAccounts(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	var req attachRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.svc.AttachAccounts(c.Request.Context(), adminID(c), id, req.AccountIDs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"attached": len(req.AccountIDs)})
}

// AdminUsageSummary GET /api/v1/admin/suppliers/:id/usage/summary
func (h *Handler) AdminUsageSummary(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		h.usageSummary(c, id)
	}
}

// AdminUsageDaily GET /api/v1/admin/suppliers/:id/usage/daily
func (h *Handler) AdminUsageDaily(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		h.usageDaily(c, id)
	}
}

// AdminUsageByAccount GET /api/v1/admin/suppliers/:id/usage/accounts
func (h *Handler) AdminUsageByAccount(c *gin.Context) {
	if id, ok := paramID(c, "id"); ok {
		h.usageByAccount(c, id)
	}
}

// AdminListSettlements GET /api/v1/admin/supplier-settlements?supplier_id=&status=
func (h *Handler) AdminListSettlements(c *gin.Context) {
	supplierID, _ := strconv.ParseInt(c.Query("supplier_id"), 10, 64)
	h.listSettlements(c, supplierID)
}

// AdminGetSettlement GET /api/v1/admin/supplier-settlements/:id
func (h *Handler) AdminGetSettlement(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	st, err := h.svc.GetSettlement(c.Request.Context(), 0, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, st)
}

type settlementRequest struct {
	SupplierID  int64  `json:"supplier_id" binding:"required,min=1"`
	PeriodStart string `json:"period_start" binding:"required"`
	PeriodEnd   string `json:"period_end" binding:"required"`
	Notes       string `json:"notes"`
}

func (r settlementRequest) period() (time.Time, time.Time, error) {
	start, err := parseBound(r.PeriodStart, false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseBound(r.PeriodEnd, true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

// AdminPreviewSettlement POST /api/v1/admin/supplier-settlements/preview
func (h *Handler) AdminPreviewSettlement(c *gin.Context) {
	var req settlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	start, end, err := req.period()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	st, err := h.svc.PreviewSettlement(c.Request.Context(), req.SupplierID, start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, st)
}

// AdminCreateSettlement POST /api/v1/admin/supplier-settlements
func (h *Handler) AdminCreateSettlement(c *gin.Context) {
	var req settlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	start, end, err := req.period()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	st, err := h.svc.CreateSettlement(c.Request.Context(), adminID(c), req.SupplierID, start, end, req.Notes)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, st)
}

// AdminConfirmSettlement POST /api/v1/admin/supplier-settlements/:id/confirm
func (h *Handler) AdminConfirmSettlement(c *gin.Context) {
	h.settlementAction(c, h.svc.ConfirmSettlement)
}

// AdminMarkSettlementPaid POST /api/v1/admin/supplier-settlements/:id/mark-paid
func (h *Handler) AdminMarkSettlementPaid(c *gin.Context) {
	h.settlementAction(c, h.svc.MarkSettlementPaid)
}

// AdminVoidSettlement POST /api/v1/admin/supplier-settlements/:id/void
func (h *Handler) AdminVoidSettlement(c *gin.Context) {
	h.settlementAction(c, h.svc.VoidSettlement)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func (h *Handler) usageSummary(c *gin.Context, supplierID int64) {
	filter, ok := parseUsageFilter(c)
	if !ok {
		return
	}
	summary, err := h.svc.UsageSummary(c.Request.Context(), supplierID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *Handler) usageDaily(c *gin.Context, supplierID int64) {
	filter, ok := parseUsageFilter(c)
	if !ok {
		return
	}
	rows, err := h.svc.UsageDaily(c.Request.Context(), supplierID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rows)
}

func (h *Handler) usageByAccount(c *gin.Context, supplierID int64) {
	filter, ok := parseUsageFilter(c)
	if !ok {
		return
	}
	rows, err := h.svc.UsageByAccount(c.Request.Context(), supplierID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rows)
}

func (h *Handler) listSettlements(c *gin.Context, supplierID int64) {
	status := c.Query("status")
	switch status {
	case "", SettlementDraft, SettlementConfirmed, SettlementPaid, SettlementVoid:
	default:
		response.BadRequest(c, "invalid status")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.svc.ListSettlements(c.Request.Context(), supplierID, status,
		pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) settlementAction(c *gin.Context, action func(ctx context.Context, id int64) (*Settlement, error)) {
	id, ok := paramID(c, "id")
	if !ok {
		return
	}
	st, err := action(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, st)
}

// parseUsageFilter reads start/end/account_id/model. Without start/end it defaults to the
// last 7 days including today.
func parseUsageFilter(c *gin.Context) (UsageFilter, bool) {
	var f UsageFilter
	var err error
	if raw := c.Query("start"); raw != "" {
		if f.Start, err = parseBound(raw, false); err != nil {
			response.ErrorFrom(c, err)
			return f, false
		}
	} else {
		f.Start = timezone.Today().AddDate(0, 0, -6)
	}
	if raw := c.Query("end"); raw != "" {
		if f.End, err = parseBound(raw, true); err != nil {
			response.ErrorFrom(c, err)
			return f, false
		}
	} else {
		f.End = timezone.Now()
	}
	if raw := c.Query("account_id"); raw != "" {
		if f.AccountID, err = strconv.ParseInt(raw, 10, 64); err != nil || f.AccountID <= 0 {
			response.BadRequest(c, "invalid account_id")
			return f, false
		}
	}
	f.Model = strings.TrimSpace(c.Query("model"))
	return f, true
}

// parseBound accepts RFC3339 timestamps or YYYY-MM-DD dates in the configured timezone.
// A date used as an end bound is inclusive, i.e. it means the start of the next day.
func parseBound(raw string, isEnd bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, timezone.Location())
	if err != nil {
		return time.Time{}, infraerrors.BadRequest("SUPPLIER_INVALID_TIME", "time must be RFC3339 or YYYY-MM-DD")
	}
	if isEnd {
		day = day.AddDate(0, 0, 1)
	}
	return day, nil
}

func paramID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}

func adminID(c *gin.Context) int64 {
	subject, _ := servermiddleware.GetAuthSubjectFromContext(c)
	return subject.UserID
}
