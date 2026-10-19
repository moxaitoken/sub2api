package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Auth modes for Claude accounts, matching the upstream account types.
const (
	AuthModeOAuth      = domain.AccountTypeOAuth
	AuthModeSetupToken = domain.AccountTypeSetupToken
)

const (
	defaultAccountPriority = 1
	maxRangeDays           = 400
)

// ClaudeOAuth is the subset of *service.OAuthService the supplier flow needs.
type ClaudeOAuth interface {
	GenerateAuthURL(ctx context.Context, proxyID *int64) (*service.GenerateAuthURLResult, error)
	GenerateSetupTokenURL(ctx context.Context, proxyID *int64) (*service.GenerateAuthURLResult, error)
	ExchangeCode(ctx context.Context, input *service.ExchangeCodeInput) (*service.TokenInfo, error)
	CookieAuth(ctx context.Context, input *service.CookieAuthInput) (*service.TokenInfo, error)
}

// Service holds the supplier business rules. Account and proxy writes go through the
// upstream AdminService so all upstream validation, cache invalidation and scheduler
// snapshot updates still apply.
type Service struct {
	repo          Repository
	admin         service.AdminService
	oauth         ClaudeOAuth
	sessions      *sessionOwners
	validateProxy func(ctx context.Context, host string) error
	now           func() time.Time
}

// NewService builds the supplier service.
func NewService(repo Repository, admin service.AdminService, oauth *service.OAuthService) *Service {
	return newService(repo, admin, oauth)
}

func newService(repo Repository, admin service.AdminService, oauth ClaudeOAuth) *Service {
	return &Service{
		repo:          repo,
		admin:         admin,
		oauth:         oauth,
		sessions:      newSessionOwners(),
		validateProxy: validatePublicProxyHost,
		now:           time.Now,
	}
}

// ---------------------------------------------------------------------------
// Supplier identity
// ---------------------------------------------------------------------------

// ResolveActive returns the active supplier behind a user, or a 403-mapped error.
func (s *Service) ResolveActive(ctx context.Context, userID int64) (*Supplier, error) {
	sup, err := s.repo.GetSupplierByUserID(ctx, userID)
	if infraerrors.IsNotFound(err) {
		return nil, ErrNotSupplier
	}
	if err != nil {
		return nil, err
	}
	if sup.Status != StatusActive {
		return nil, ErrSupplierDisabled
	}
	return sup, nil
}

// ---------------------------------------------------------------------------
// Proxies
// ---------------------------------------------------------------------------

// ProxyInput is the supplier-editable part of a proxy. Nil pointers keep current values.
type ProxyInput struct {
	Name     string
	Protocol string
	Host     string
	Port     int
	Username *string
	Password *string
}

func (s *Service) ListProxies(ctx context.Context, sup *Supplier) ([]ProxyView, error) {
	ids, err := s.repo.ListProxyIDs(ctx, sup.ID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []ProxyView{}, nil
	}
	proxies, err := s.admin.GetProxiesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountAccountsByProxy(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyView, 0, len(proxies))
	for i := range proxies {
		out = append(out, toProxyView(&proxies[i], counts[proxies[i].ID]))
	}
	return out, nil
}

func (s *Service) CreateProxy(ctx context.Context, sup *Supplier, in ProxyInput) (*ProxyView, error) {
	if err := s.validateProxy(ctx, in.Host); err != nil {
		return nil, err
	}
	proxy, err := s.admin.CreateProxy(ctx, &service.CreateProxyInput{
		Name:     in.Name,
		Protocol: in.Protocol,
		Host:     in.Host,
		Port:     in.Port,
		Username: deref(in.Username),
		Password: deref(in.Password),
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.LinkProxy(ctx, proxy.ID, sup.ID); err != nil {
		// Do not leave an orphan proxy the supplier cannot see or delete.
		_ = s.admin.DeleteProxy(ctx, proxy.ID)
		return nil, err
	}
	view := toProxyView(proxy, 0)
	return &view, nil
}

func (s *Service) UpdateProxy(ctx context.Context, sup *Supplier, proxyID int64, in ProxyInput) (*ProxyView, error) {
	if err := s.requireProxy(ctx, sup, proxyID); err != nil {
		return nil, err
	}
	if in.Host != "" {
		if err := s.validateProxy(ctx, in.Host); err != nil {
			return nil, err
		}
	}
	proxy, err := s.admin.UpdateProxy(ctx, proxyID, &service.UpdateProxyInput{
		Name:     in.Name,
		Protocol: in.Protocol,
		Host:     in.Host,
		Port:     in.Port,
		Username: in.Username,
		Password: in.Password,
	})
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountAccountsByProxy(ctx, []int64{proxyID})
	if err != nil {
		return nil, err
	}
	view := toProxyView(proxy, counts[proxyID])
	return &view, nil
}

func (s *Service) DeleteProxy(ctx context.Context, sup *Supplier, proxyID int64) error {
	if err := s.requireProxy(ctx, sup, proxyID); err != nil {
		return err
	}
	return s.admin.DeleteProxy(ctx, proxyID)
}

func (s *Service) TestProxy(ctx context.Context, sup *Supplier, proxyID int64) (*service.ProxyTestResult, error) {
	if err := s.requireProxy(ctx, sup, proxyID); err != nil {
		return nil, err
	}
	return s.admin.TestProxy(ctx, proxyID)
}

// requireProxy fails with ErrProxyNotFound unless the proxy belongs to sup, so other
// suppliers' and admin proxies are indistinguishable from missing ones.
func (s *Service) requireProxy(ctx context.Context, sup *Supplier, proxyID int64) error {
	owner, err := s.repo.GetProxyOwner(ctx, proxyID)
	if err != nil {
		return err
	}
	if owner != sup.ID {
		return ErrProxyNotFound
	}
	return nil
}

// requireOptionalProxy validates a proxy selection that may be empty.
func (s *Service) requireOptionalProxy(ctx context.Context, sup *Supplier, proxyID *int64) error {
	if proxyID == nil || *proxyID == 0 {
		return nil
	}
	return s.requireProxy(ctx, sup, *proxyID)
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

func (s *Service) ListAccounts(ctx context.Context, sup *Supplier) ([]AccountView, error) {
	links, err := s.repo.ListAccountLinks(ctx, sup.ID, "")
	if err != nil {
		return nil, err
	}
	return s.accountViews(ctx, links)
}

func (s *Service) accountViews(ctx context.Context, links []AccountLink) ([]AccountView, error) {
	if len(links) == 0 {
		return []AccountView{}, nil
	}
	ids := make([]int64, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.AccountID)
	}
	accounts, err := s.admin.GetAccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.Account, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			byID[acc.ID] = acc
		}
	}
	out := make([]AccountView, 0, len(links))
	for i := range links {
		if acc, ok := byID[links[i].AccountID]; ok {
			out = append(out, toAccountView(acc, &links[i]))
		}
	}
	return out, nil
}

// StartAuth begins a Claude authorization. The returned session can only be completed by
// the same supplier.
func (s *Service) StartAuth(ctx context.Context, sup *Supplier, mode string, proxyID *int64) (*service.GenerateAuthURLResult, error) {
	if err := s.requireOptionalProxy(ctx, sup, proxyID); err != nil {
		return nil, err
	}
	var (
		result *service.GenerateAuthURLResult
		err    error
	)
	switch mode {
	case AuthModeOAuth:
		result, err = s.oauth.GenerateAuthURL(ctx, proxyID)
	case AuthModeSetupToken:
		result, err = s.oauth.GenerateSetupTokenURL(ctx, proxyID)
	default:
		return nil, ErrInvalidAuthMode
	}
	if err != nil {
		return nil, err
	}
	s.sessions.put(result.SessionID, sup.ID, mode, s.now())
	return result, nil
}

// AuthInput completes an authorization. With AccountID set, the credentials of that
// existing account are replaced (re-authorization); otherwise a new account is created.
type AuthInput struct {
	Mode       string
	SessionID  string
	Code       string
	SessionKey string
	Name       string
	ProxyID    *int64
	AccountID  int64
}

// CompleteAuth exchanges an authorization code from StartAuth.
func (s *Service) CompleteAuth(ctx context.Context, sup *Supplier, in AuthInput) (*AccountView, error) {
	mode, ok := s.sessions.lookup(in.SessionID, sup.ID, s.now())
	if !ok {
		return nil, ErrOAuthSessionNotFound
	}
	in.Mode = mode
	if err := s.preflightAuth(ctx, sup, in); err != nil {
		return nil, err
	}
	token, err := s.oauth.ExchangeCode(ctx, &service.ExchangeCodeInput{
		SessionID: in.SessionID,
		Code:      in.Code,
		ProxyID:   in.ProxyID,
	})
	if err != nil {
		// Keep the session so a mistyped code can be retried.
		return nil, err
	}
	s.sessions.drop(in.SessionID)
	return s.saveAuthorizedAccount(ctx, sup, in, token)
}

// CookieAuth authorizes with a claude.ai session key instead of the browser flow.
func (s *Service) CookieAuth(ctx context.Context, sup *Supplier, in AuthInput) (*AccountView, error) {
	scope := map[string]string{AuthModeOAuth: "full", AuthModeSetupToken: "inference"}[in.Mode]
	if scope == "" {
		return nil, ErrInvalidAuthMode
	}
	if err := s.preflightAuth(ctx, sup, in); err != nil {
		return nil, err
	}
	token, err := s.oauth.CookieAuth(ctx, &service.CookieAuthInput{
		SessionKey: in.SessionKey,
		ProxyID:    in.ProxyID,
		Scope:      scope,
	})
	if err != nil {
		return nil, err
	}
	return s.saveAuthorizedAccount(ctx, sup, in, token)
}

// preflightAuth runs the ownership and quota checks before the upstream token exchange so
// a rejected request never consumes an authorization code.
func (s *Service) preflightAuth(ctx context.Context, sup *Supplier, in AuthInput) error {
	if err := s.requireOptionalProxy(ctx, sup, in.ProxyID); err != nil {
		return err
	}
	if in.AccountID > 0 {
		return s.requireAccount(ctx, sup, in.AccountID)
	}
	if strings.TrimSpace(in.Name) == "" {
		return infraerrors.BadRequest("SUPPLIER_ACCOUNT_NAME_REQUIRED", "account name is required")
	}
	if sup.MaxAccounts > 0 {
		count, err := s.repo.CountActiveAccounts(ctx, sup.ID)
		if err != nil {
			return err
		}
		if count >= int64(sup.MaxAccounts) {
			return ErrAccountLimitReached
		}
	}
	return nil
}

func (s *Service) saveAuthorizedAccount(ctx context.Context, sup *Supplier, in AuthInput, token *service.TokenInfo) (*AccountView, error) {
	credentials, extra, err := claudeCredentials(token)
	if err != nil {
		return nil, err
	}

	if in.AccountID > 0 {
		input := &service.UpdateAccountInput{Type: in.Mode, Credentials: credentials, Extra: extra}
		if in.ProxyID != nil {
			input.ProxyID = in.ProxyID
		}
		if _, err := s.admin.UpdateAccount(ctx, in.AccountID, input); err != nil {
			return nil, err
		}
		return s.accountView(ctx, in.AccountID)
	}

	account, err := s.admin.CreateAccount(ctx, &service.CreateAccountInput{
		Name:                 strings.TrimSpace(in.Name),
		Notes:                stringPtr(fmt.Sprintf("supplier #%d %s", sup.ID, sup.Name)),
		Platform:             domain.PlatformAnthropic,
		Type:                 in.Mode,
		Credentials:          credentials,
		Extra:                extra,
		ProxyID:              nonZero(in.ProxyID),
		Concurrency:          sup.DefaultConcurrency,
		Priority:             defaultAccountPriority,
		SkipDefaultGroupBind: true,
	})
	if err != nil {
		return nil, err
	}
	// Ungrouped accounts are never selected, but stay explicit: nothing a supplier adds is
	// schedulable until an admin approves it.
	if _, err := s.admin.SetAccountSchedulable(ctx, account.ID, false); err != nil {
		_ = s.admin.DeleteAccount(ctx, account.ID)
		return nil, err
	}
	link := &AccountLink{AccountID: account.ID, SupplierID: sup.ID, ReviewStatus: ReviewPending}
	if err := s.repo.LinkAccount(ctx, link); err != nil {
		_ = s.admin.DeleteAccount(ctx, account.ID)
		return nil, err
	}
	account.Schedulable = false
	view := toAccountView(account, link)
	return &view, nil
}

// AccountUpdate is the supplier-editable part of an account.
type AccountUpdate struct {
	Name       string
	ProxyID    *int64
	ClearProxy bool
}

func (s *Service) UpdateAccount(ctx context.Context, sup *Supplier, accountID int64, in AccountUpdate) (*AccountView, error) {
	if err := s.requireAccount(ctx, sup, accountID); err != nil {
		return nil, err
	}
	input := &service.UpdateAccountInput{Name: strings.TrimSpace(in.Name)}
	switch {
	case in.ClearProxy:
		zero := int64(0)
		input.ProxyID = &zero
	case in.ProxyID != nil:
		if err := s.requireProxy(ctx, sup, *in.ProxyID); err != nil {
			return nil, err
		}
		input.ProxyID = in.ProxyID
	}
	if _, err := s.admin.UpdateAccount(ctx, accountID, input); err != nil {
		return nil, err
	}
	return s.accountView(ctx, accountID)
}

func (s *Service) DeleteAccount(ctx context.Context, sup *Supplier, accountID int64) error {
	if err := s.requireAccount(ctx, sup, accountID); err != nil {
		return err
	}
	return s.admin.DeleteAccount(ctx, accountID)
}

func (s *Service) requireAccount(ctx context.Context, sup *Supplier, accountID int64) error {
	link, err := s.repo.GetAccountLink(ctx, accountID)
	if err != nil {
		return err
	}
	if link.SupplierID != sup.ID {
		return ErrAccountNotFound
	}
	return nil
}

func (s *Service) accountView(ctx context.Context, accountID int64) (*AccountView, error) {
	link, err := s.repo.GetAccountLink(ctx, accountID)
	if err != nil {
		return nil, err
	}
	views, err := s.accountViews(ctx, []AccountLink{*link})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, ErrAccountNotFound
	}
	return &views[0], nil
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

// UsageFilter narrows a usage query within the supplier's own accounts.
type UsageFilter struct {
	Start     time.Time
	End       time.Time
	AccountID int64
	Model     string
}

func (s *Service) usageQuery(ctx context.Context, supplierID int64, f UsageFilter) (UsageQuery, error) {
	if err := validateRange(f.Start, f.End); err != nil {
		return UsageQuery{}, err
	}
	ids, err := s.repo.AllAccountIDs(ctx, supplierID)
	if err != nil {
		return UsageQuery{}, err
	}
	if f.AccountID > 0 && !containsID(ids, f.AccountID) {
		return UsageQuery{}, ErrAccountNotFound
	}
	return UsageQuery{AccountIDs: ids, Start: f.Start, End: f.End, AccountID: f.AccountID, Model: f.Model}, nil
}

// UsageSummary returns totals plus per-family and per-model breakdowns.
func (s *Service) UsageSummary(ctx context.Context, supplierID int64, f UsageFilter) (*UsageSummary, error) {
	q, err := s.usageQuery(ctx, supplierID, f)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.AggregateUsage(ctx, q, GroupByModel)
	if err != nil {
		return nil, err
	}
	summary := &UsageSummary{Start: f.Start, End: f.End, ByModel: rows, ByFamily: groupByFamily(rows)}
	for _, row := range rows {
		summary.Totals.Add(row.UsageTotals)
	}
	return summary, nil
}

// UsageDaily returns one row per day and model.
func (s *Service) UsageDaily(ctx context.Context, supplierID int64, f UsageFilter) ([]UsageRow, error) {
	q, err := s.usageQuery(ctx, supplierID, f)
	if err != nil {
		return nil, err
	}
	return s.repo.AggregateUsage(ctx, q, GroupByDateModel)
}

// UsageByAccount returns one row per account.
func (s *Service) UsageByAccount(ctx context.Context, supplierID int64, f UsageFilter) ([]UsageRow, error) {
	q, err := s.usageQuery(ctx, supplierID, f)
	if err != nil {
		return nil, err
	}
	return s.repo.AggregateUsage(ctx, q, GroupByAccount)
}

// ---------------------------------------------------------------------------
// Settlements
// ---------------------------------------------------------------------------

func (s *Service) ListSettlements(ctx context.Context, supplierID int64, status string, params pagination.PaginationParams) ([]Settlement, int64, error) {
	return s.repo.ListSettlements(ctx, supplierID, status, params)
}

// GetSettlement returns a settlement; supplierID > 0 restricts it to that supplier.
func (s *Service) GetSettlement(ctx context.Context, supplierID, id int64) (*Settlement, error) {
	st, err := s.repo.GetSettlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if supplierID > 0 && st.SupplierID != supplierID {
		return nil, ErrSettlementNotFound
	}
	return st, nil
}

// PreviewSettlement computes a statement without saving it.
func (s *Service) PreviewSettlement(ctx context.Context, supplierID int64, start, end time.Time) (*Settlement, error) {
	if _, err := s.repo.GetSupplier(ctx, supplierID); err != nil {
		return nil, err
	}
	if err := s.validatePeriod(start, end); err != nil {
		return nil, err
	}
	ids, err := s.repo.AllAccountIDs(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.AggregateUsage(ctx, UsageQuery{AccountIDs: ids, Start: start, End: end}, GroupByAccountModel)
	if err != nil {
		return nil, err
	}
	st := &Settlement{SupplierID: supplierID, PeriodStart: start, PeriodEnd: end, Status: SettlementDraft, Items: make([]SettlementItem, 0, len(rows))}
	for _, row := range rows {
		st.Items = append(st.Items, SettlementItem{
			AccountID:   row.AccountID,
			AccountName: row.AccountName,
			Model:       row.Model,
			Family:      row.Family,
			UsageTotals: row.UsageTotals,
		})
		st.UsageTotals.Add(row.UsageTotals)
	}
	return st, nil
}

// CreateSettlement saves a draft statement for the period.
func (s *Service) CreateSettlement(ctx context.Context, adminID, supplierID int64, start, end time.Time, notes string) (*Settlement, error) {
	st, err := s.PreviewSettlement(ctx, supplierID, start, end)
	if err != nil {
		return nil, err
	}
	st.Notes = strings.TrimSpace(notes)
	st.CreatedBy = &adminID
	if err := s.repo.CreateSettlement(ctx, st); err != nil {
		return nil, err
	}
	return s.repo.GetSettlement(ctx, st.ID)
}

func (s *Service) ConfirmSettlement(ctx context.Context, id int64) (*Settlement, error) {
	return s.transition(ctx, id, []string{SettlementDraft}, SettlementConfirmed)
}

func (s *Service) MarkSettlementPaid(ctx context.Context, id int64) (*Settlement, error) {
	return s.transition(ctx, id, []string{SettlementConfirmed}, SettlementPaid)
}

// VoidSettlement frees the period for a new statement. Paid statements cannot be voided.
func (s *Service) VoidSettlement(ctx context.Context, id int64) (*Settlement, error) {
	return s.transition(ctx, id, []string{SettlementDraft, SettlementConfirmed}, SettlementVoid)
}

func (s *Service) transition(ctx context.Context, id int64, from []string, to string) (*Settlement, error) {
	if err := s.repo.TransitionSettlement(ctx, id, from, to); err != nil {
		return nil, err
	}
	return s.repo.GetSettlement(ctx, id)
}

// validatePeriod requires a closed period: usage is recorded asynchronously, so a
// statement must not cover time that has not happened yet.
func (s *Service) validatePeriod(start, end time.Time) error {
	if err := validateRange(start, end); err != nil {
		return err
	}
	if end.After(s.now()) {
		return infraerrors.BadRequest("SUPPLIER_PERIOD_NOT_CLOSED", "settlement period must end in the past")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

// SupplierInput is the admin-editable part of a supplier. Updates replace every field;
// UserID / Email identify the user and are only used on create.
type SupplierInput struct {
	UserID             int64
	Email              string
	Name               string
	Status             string
	DefaultConcurrency int
	MaxAccounts        int
	Notes              string
}

func (s *Service) ListSuppliers(ctx context.Context, params pagination.PaginationParams, search string) ([]Supplier, int64, error) {
	return s.repo.ListSuppliers(ctx, params, search)
}

func (s *Service) GetSupplier(ctx context.Context, id int64) (*Supplier, error) {
	return s.repo.GetSupplier(ctx, id)
}

func (s *Service) CreateSupplier(ctx context.Context, in SupplierInput) (*Supplier, error) {
	in.Status = StatusActive
	sup, err := normalizeSupplierInput(in)
	if err != nil {
		return nil, err
	}
	sup.UserID = in.UserID
	if sup.UserID == 0 {
		if strings.TrimSpace(in.Email) == "" {
			return nil, infraerrors.BadRequest("SUPPLIER_USER_REQUIRED", "user_id or email is required")
		}
		if sup.UserID, err = s.repo.FindUserIDByEmail(ctx, in.Email); err != nil {
			return nil, err
		}
	}
	if err := s.repo.CreateSupplier(ctx, sup); err != nil {
		return nil, err
	}
	return s.repo.GetSupplier(ctx, sup.ID)
}

func (s *Service) UpdateSupplier(ctx context.Context, id int64, in SupplierInput) (*Supplier, error) {
	sup, err := normalizeSupplierInput(in)
	if err != nil {
		return nil, err
	}
	sup.ID = id
	if err := s.repo.UpdateSupplier(ctx, sup); err != nil {
		return nil, err
	}
	return s.repo.GetSupplier(ctx, id)
}

func normalizeSupplierInput(in SupplierInput) (*Supplier, error) {
	sup := &Supplier{
		Name:               strings.TrimSpace(in.Name),
		Status:             in.Status,
		DefaultConcurrency: in.DefaultConcurrency,
		MaxAccounts:        in.MaxAccounts,
		Notes:              strings.TrimSpace(in.Notes),
	}
	switch {
	case sup.Name == "":
		return nil, infraerrors.BadRequest("SUPPLIER_NAME_REQUIRED", "supplier name is required")
	case sup.Status != StatusActive && sup.Status != StatusDisabled:
		return nil, infraerrors.BadRequest("SUPPLIER_INVALID_STATUS", "status must be active or disabled")
	case sup.DefaultConcurrency < 1:
		return nil, infraerrors.BadRequest("SUPPLIER_INVALID_CONCURRENCY", "default_concurrency must be at least 1")
	case sup.MaxAccounts < 0:
		return nil, infraerrors.BadRequest("SUPPLIER_INVALID_MAX_ACCOUNTS", "max_accounts must not be negative")
	}
	return sup, nil
}

// AdminAccountView adds the owning supplier to an account view.
type AdminAccountView struct {
	AccountView
	SupplierID     int64    `json:"supplier_id"`
	Priority       int      `json:"priority"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	GroupIDs       []int64  `json:"group_ids"`
}

// ListSupplierAccounts lists supplier accounts for review; supplierID 0 means all.
func (s *Service) ListSupplierAccounts(ctx context.Context, supplierID int64, reviewStatus string) ([]AdminAccountView, error) {
	links, err := s.repo.ListAccountLinks(ctx, supplierID, reviewStatus)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return []AdminAccountView{}, nil
	}
	ids := make([]int64, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.AccountID)
	}
	accounts, err := s.admin.GetAccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.Account, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			byID[acc.ID] = acc
		}
	}
	out := make([]AdminAccountView, 0, len(links))
	for i := range links {
		acc, ok := byID[links[i].AccountID]
		if !ok {
			continue
		}
		groupIDs := acc.GroupIDs
		if groupIDs == nil {
			groupIDs = []int64{}
		}
		out = append(out, AdminAccountView{
			AccountView:    toAccountView(acc, &links[i]),
			SupplierID:     links[i].SupplierID,
			Priority:       acc.Priority,
			RateMultiplier: acc.RateMultiplier,
			GroupIDs:       groupIDs,
		})
	}
	return out, nil
}

// ReviewInput approves or rejects a supplier account. On approval the optional fields are
// applied before the account becomes schedulable.
type ReviewInput struct {
	Action         string
	Note           string
	GroupIDs       *[]int64
	Concurrency    *int
	Priority       *int
	RateMultiplier *float64
}

func (s *Service) ReviewAccount(ctx context.Context, adminID, accountID int64, in ReviewInput) error {
	if _, err := s.repo.GetAccountLink(ctx, accountID); err != nil {
		return err
	}
	switch in.Action {
	case "approve":
		update := &service.UpdateAccountInput{
			GroupIDs:       in.GroupIDs,
			Concurrency:    in.Concurrency,
			Priority:       in.Priority,
			RateMultiplier: in.RateMultiplier,
		}
		if update.GroupIDs != nil || update.Concurrency != nil || update.Priority != nil || update.RateMultiplier != nil {
			if _, err := s.admin.UpdateAccount(ctx, accountID, update); err != nil {
				return err
			}
		}
		if _, err := s.admin.SetAccountSchedulable(ctx, accountID, true); err != nil {
			return err
		}
		return s.repo.UpdateAccountReview(ctx, accountID, ReviewApproved, strings.TrimSpace(in.Note), adminID)
	case "reject":
		if _, err := s.admin.SetAccountSchedulable(ctx, accountID, false); err != nil {
			return err
		}
		return s.repo.UpdateAccountReview(ctx, accountID, ReviewRejected, strings.TrimSpace(in.Note), adminID)
	default:
		return ErrInvalidReview
	}
}

// AttachAccounts assigns existing (admin-created) accounts to a supplier as approved, so
// their usage counts towards the supplier's settlements from now on.
func (s *Service) AttachAccounts(ctx context.Context, adminID, supplierID int64, accountIDs []int64) error {
	if _, err := s.repo.GetSupplier(ctx, supplierID); err != nil {
		return err
	}
	accounts, err := s.admin.GetAccountsByIDs(ctx, accountIDs)
	if err != nil {
		return err
	}
	found := make(map[int64]bool, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			found[acc.ID] = true
		}
	}
	for _, id := range accountIDs {
		if !found[id] {
			return ErrAccountNotFound
		}
	}
	for _, id := range accountIDs {
		if err := s.repo.LinkAccount(ctx, &AccountLink{AccountID: id, SupplierID: supplierID, ReviewStatus: ReviewApproved}); err != nil {
			return err
		}
		if err := s.repo.UpdateAccountReview(ctx, id, ReviewApproved, "attached by admin", adminID); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// claudeCredentials mirrors what the admin UI stores for Claude OAuth / setup-token
// accounts: the whole token response as credentials, identity fields copied to extra.
func claudeCredentials(token *service.TokenInfo) (map[string]any, map[string]any, error) {
	raw, err := json.Marshal(token)
	if err != nil {
		return nil, nil, err
	}
	credentials := map[string]any{}
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return nil, nil, err
	}
	extra := map[string]any{}
	for key, value := range map[string]string{
		"org_uuid":      token.OrgUUID,
		"account_uuid":  token.AccountUUID,
		"email_address": token.EmailAddress,
	} {
		if value != "" {
			extra[key] = value
		}
	}
	return credentials, extra, nil
}

func toAccountView(acc *service.Account, link *AccountLink) AccountView {
	view := AccountView{
		ID:                      acc.ID,
		Name:                    acc.Name,
		Platform:                acc.Platform,
		Type:                    acc.Type,
		Status:                  acc.Status,
		Schedulable:             acc.Schedulable,
		ErrorMessage:            acc.ErrorMessage,
		ProxyID:                 acc.ProxyID,
		Concurrency:             acc.Concurrency,
		RateLimitResetAt:        acc.RateLimitResetAt,
		OverloadUntil:           acc.OverloadUntil,
		TempUnschedulableUntil:  acc.TempUnschedulableUntil,
		TempUnschedulableReason: acc.TempUnschedulableReason,
		SessionWindowEnd:        acc.SessionWindowEnd,
		SessionWindowStatus:     acc.SessionWindowStatus,
		ExpiresAt:               acc.ExpiresAt,
		LastUsedAt:              acc.LastUsedAt,
		CreatedAt:               acc.CreatedAt,
	}
	if email, ok := acc.Extra["email_address"].(string); ok {
		view.Email = email
	}
	if link != nil {
		view.ReviewStatus = link.ReviewStatus
		view.ReviewNote = link.ReviewNote
	}
	return view
}

func toProxyView(p *service.Proxy, accountCount int64) ProxyView {
	return ProxyView{
		ID:           p.ID,
		Name:         p.Name,
		Protocol:     p.Protocol,
		Host:         p.Host,
		Port:         p.Port,
		Username:     p.Username,
		HasPassword:  p.Password != "",
		Status:       p.Status,
		ExpiresAt:    p.ExpiresAt,
		AccountCount: accountCount,
		CreatedAt:    p.CreatedAt,
	}
}

func validateRange(start, end time.Time) error {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ErrInvalidPeriod
	}
	if end.Sub(start) > maxRangeDays*24*time.Hour {
		return infraerrors.BadRequest("SUPPLIER_RANGE_TOO_LONG", fmt.Sprintf("time range must not exceed %d days", maxRangeDays))
	}
	return nil
}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func stringPtr(s string) *string { return &s }

func nonZero(id *int64) *int64 {
	if id == nil || *id == 0 {
		return nil
	}
	return id
}
