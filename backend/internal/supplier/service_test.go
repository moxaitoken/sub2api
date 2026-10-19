package supplier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeRepo struct {
	suppliers    map[int64]*Supplier
	links        map[int64]*AccountLink
	proxyOwners  map[int64]int64
	usageRows    []UsageRow
	lastQuery    UsageQuery
	created      *Settlement
	reviews      map[int64]string
	linkProxyErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		suppliers:   map[int64]*Supplier{},
		links:       map[int64]*AccountLink{},
		proxyOwners: map[int64]int64{},
		reviews:     map[int64]string{},
	}
}

func (r *fakeRepo) GetSupplier(_ context.Context, id int64) (*Supplier, error) {
	if s, ok := r.suppliers[id]; ok {
		return s, nil
	}
	return nil, ErrSupplierNotFound
}

func (r *fakeRepo) GetSupplierByUserID(_ context.Context, userID int64) (*Supplier, error) {
	for _, s := range r.suppliers {
		if s.UserID == userID {
			return s, nil
		}
	}
	return nil, ErrSupplierNotFound
}

func (r *fakeRepo) FindUserIDByEmail(context.Context, string) (int64, error) {
	return 0, ErrUserNotFound
}

func (r *fakeRepo) ListSuppliers(context.Context, pagination.PaginationParams, string) ([]Supplier, int64, error) {
	return nil, 0, nil
}

func (r *fakeRepo) CreateSupplier(_ context.Context, s *Supplier) error {
	s.ID = int64(len(r.suppliers) + 1)
	r.suppliers[s.ID] = s
	return nil
}

func (r *fakeRepo) UpdateSupplier(_ context.Context, s *Supplier) error {
	r.suppliers[s.ID] = s
	return nil
}

func (r *fakeRepo) LinkAccount(_ context.Context, link *AccountLink) error {
	if _, ok := r.links[link.AccountID]; ok {
		return ErrAccountAlreadyLinked
	}
	r.links[link.AccountID] = link
	return nil
}

func (r *fakeRepo) GetAccountLink(_ context.Context, accountID int64) (*AccountLink, error) {
	if link, ok := r.links[accountID]; ok {
		return link, nil
	}
	return nil, ErrAccountNotFound
}

func (r *fakeRepo) ListAccountLinks(_ context.Context, supplierID int64, _ string) ([]AccountLink, error) {
	out := []AccountLink{}
	for _, link := range r.links {
		if supplierID == 0 || link.SupplierID == supplierID {
			out = append(out, *link)
		}
	}
	return out, nil
}

func (r *fakeRepo) CountActiveAccounts(_ context.Context, supplierID int64) (int64, error) {
	var n int64
	for _, link := range r.links {
		if link.SupplierID == supplierID {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) AllAccountIDs(_ context.Context, supplierID int64) ([]int64, error) {
	ids := []int64{}
	for id, link := range r.links {
		if link.SupplierID == supplierID {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *fakeRepo) UpdateAccountReview(_ context.Context, accountID int64, status, _ string, _ int64) error {
	r.reviews[accountID] = status
	if link, ok := r.links[accountID]; ok {
		link.ReviewStatus = status
	}
	return nil
}

func (r *fakeRepo) LinkProxy(_ context.Context, proxyID, supplierID int64) error {
	if r.linkProxyErr != nil {
		return r.linkProxyErr
	}
	r.proxyOwners[proxyID] = supplierID
	return nil
}

func (r *fakeRepo) GetProxyOwner(_ context.Context, proxyID int64) (int64, error) {
	if owner, ok := r.proxyOwners[proxyID]; ok {
		return owner, nil
	}
	return 0, ErrProxyNotFound
}

func (r *fakeRepo) ListProxyIDs(context.Context, int64) ([]int64, error) { return nil, nil }

func (r *fakeRepo) CountAccountsByProxy(context.Context, []int64) (map[int64]int64, error) {
	return map[int64]int64{}, nil
}

func (r *fakeRepo) AggregateUsage(_ context.Context, q UsageQuery, _ UsageGrouping) ([]UsageRow, error) {
	r.lastQuery = q
	return r.usageRows, nil
}

func (r *fakeRepo) CreateSettlement(_ context.Context, s *Settlement) error {
	s.ID = 99
	r.created = s
	return nil
}

func (r *fakeRepo) GetSettlement(_ context.Context, id int64) (*Settlement, error) {
	if r.created != nil && r.created.ID == id {
		return r.created, nil
	}
	return nil, ErrSettlementNotFound
}

func (r *fakeRepo) ListSettlements(context.Context, int64, string, pagination.PaginationParams) ([]Settlement, int64, error) {
	return nil, 0, nil
}

func (r *fakeRepo) TransitionSettlement(context.Context, int64, []string, string) error { return nil }

// fakeAdmin implements only the AdminService methods the supplier module calls; any
// other call panics on the nil embedded interface.
type fakeAdmin struct {
	service.AdminService
	accounts     map[int64]*service.Account
	nextID       int64
	createInput  *service.CreateAccountInput
	updateInputs map[int64]*service.UpdateAccountInput
	schedulable  map[int64]bool
	deleted      []int64
	deletedProxy []int64
	createdProxy *service.CreateProxyInput
}

func newFakeAdmin() *fakeAdmin {
	return &fakeAdmin{
		accounts:     map[int64]*service.Account{},
		nextID:       100,
		updateInputs: map[int64]*service.UpdateAccountInput{},
		schedulable:  map[int64]bool{},
	}
}

func (a *fakeAdmin) CreateAccount(_ context.Context, in *service.CreateAccountInput) (*service.Account, error) {
	a.createInput = in
	a.nextID++
	acc := &service.Account{ID: a.nextID, Name: in.Name, Platform: in.Platform, Type: in.Type,
		Credentials: in.Credentials, Extra: in.Extra, Concurrency: in.Concurrency, Schedulable: true}
	a.accounts[acc.ID] = acc
	return acc, nil
}

func (a *fakeAdmin) UpdateAccount(_ context.Context, id int64, in *service.UpdateAccountInput) (*service.Account, error) {
	a.updateInputs[id] = in
	return a.accounts[id], nil
}

func (a *fakeAdmin) SetAccountSchedulable(_ context.Context, id int64, schedulable bool) (*service.Account, error) {
	a.schedulable[id] = schedulable
	return a.accounts[id], nil
}

func (a *fakeAdmin) DeleteAccount(_ context.Context, id int64) error {
	a.deleted = append(a.deleted, id)
	return nil
}

func (a *fakeAdmin) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	out := []*service.Account{}
	for _, id := range ids {
		if acc, ok := a.accounts[id]; ok {
			out = append(out, acc)
		}
	}
	return out, nil
}

func (a *fakeAdmin) CreateProxy(_ context.Context, in *service.CreateProxyInput) (*service.Proxy, error) {
	a.createdProxy = in
	return &service.Proxy{ID: 7, Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port, Password: in.Password}, nil
}

func (a *fakeAdmin) DeleteProxy(_ context.Context, id int64) error {
	a.deletedProxy = append(a.deletedProxy, id)
	return nil
}

type fakeOAuth struct {
	exchanged int
	cookie    *service.CookieAuthInput
	token     *service.TokenInfo
	err       error
}

func (o *fakeOAuth) GenerateAuthURL(context.Context, *int64) (*service.GenerateAuthURLResult, error) {
	return &service.GenerateAuthURLResult{AuthURL: "https://claude.ai/oauth", SessionID: "sess-oauth"}, nil
}

func (o *fakeOAuth) GenerateSetupTokenURL(context.Context, *int64) (*service.GenerateAuthURLResult, error) {
	return &service.GenerateAuthURLResult{AuthURL: "https://claude.ai/oauth", SessionID: "sess-setup"}, nil
}

func (o *fakeOAuth) ExchangeCode(context.Context, *service.ExchangeCodeInput) (*service.TokenInfo, error) {
	o.exchanged++
	if o.err != nil {
		return nil, o.err
	}
	return o.token, nil
}

func (o *fakeOAuth) CookieAuth(_ context.Context, in *service.CookieAuthInput) (*service.TokenInfo, error) {
	o.cookie = in
	return o.token, nil
}

type fixture struct {
	svc   *Service
	repo  *fakeRepo
	admin *fakeAdmin
	oauth *fakeOAuth
	sup   *Supplier
	other *Supplier
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := newFakeRepo()
	admin := newFakeAdmin()
	oauth := &fakeOAuth{token: &service.TokenInfo{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: 1893456000,
		OrgUUID: "org-1", EmailAddress: "owner@example.com",
	}}
	f := &fixture{
		repo:  repo,
		admin: admin,
		oauth: oauth,
		sup:   &Supplier{ID: 1, UserID: 10, Name: "acme", Status: StatusActive, DefaultConcurrency: 3},
		other: &Supplier{ID: 2, UserID: 20, Name: "other", Status: StatusActive, DefaultConcurrency: 1},
		now:   time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	repo.suppliers[1] = f.sup
	repo.suppliers[2] = f.other
	f.svc = newService(repo, admin, oauth)
	f.svc.now = func() time.Time { return f.now }
	f.svc.validateProxy = func(context.Context, string) error { return nil }
	return f
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestResolveActive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	sup, err := f.svc.ResolveActive(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), sup.ID)

	_, err = f.svc.ResolveActive(ctx, 999)
	require.ErrorIs(t, err, ErrNotSupplier)

	f.sup.Status = StatusDisabled
	_, err = f.svc.ResolveActive(ctx, 10)
	require.ErrorIs(t, err, ErrSupplierDisabled)
}

func TestOAuthCreatesPendingUnschedulableAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.repo.proxyOwners[5] = f.sup.ID
	proxyID := int64(5)

	start, err := f.svc.StartAuth(ctx, f.sup, AuthModeSetupToken, &proxyID)
	require.NoError(t, err)

	view, err := f.svc.CompleteAuth(ctx, f.sup, AuthInput{SessionID: start.SessionID, Code: "code", Name: " claude-1 ", ProxyID: &proxyID})
	require.NoError(t, err)

	in := f.admin.createInput
	require.Equal(t, domain.PlatformAnthropic, in.Platform)
	require.Equal(t, AuthModeSetupToken, in.Type)
	require.Equal(t, "claude-1", in.Name)
	require.Equal(t, 3, in.Concurrency, "uses the supplier default concurrency")
	require.True(t, in.SkipDefaultGroupBind, "supplier accounts must not join the default group")
	require.Empty(t, in.GroupIDs)
	require.Equal(t, "at", in.Credentials["access_token"])
	require.Equal(t, "rt", in.Credentials["refresh_token"])
	require.Equal(t, "owner@example.com", in.Extra["email_address"])
	require.Equal(t, int64(5), *in.ProxyID)

	require.False(t, f.admin.schedulable[view.ID], "new supplier accounts start unschedulable")
	require.Equal(t, ReviewPending, f.repo.links[view.ID].ReviewStatus)
	require.Equal(t, f.sup.ID, f.repo.links[view.ID].SupplierID)
	require.Equal(t, "owner@example.com", view.Email)

	_, err = f.svc.CompleteAuth(ctx, f.sup, AuthInput{SessionID: start.SessionID, Code: "code", Name: "again"})
	require.ErrorIs(t, err, ErrOAuthSessionNotFound, "a completed session cannot be reused")
}

func TestOAuthSessionIsBoundToSupplier(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	start, err := f.svc.StartAuth(ctx, f.sup, AuthModeOAuth, nil)
	require.NoError(t, err)

	_, err = f.svc.CompleteAuth(ctx, f.other, AuthInput{SessionID: start.SessionID, Code: "code", Name: "x"})
	require.ErrorIs(t, err, ErrOAuthSessionNotFound)
	require.Zero(t, f.oauth.exchanged, "another supplier's session must not reach the token exchange")

	f.now = f.now.Add(31 * time.Minute)
	_, err = f.svc.CompleteAuth(ctx, f.sup, AuthInput{SessionID: start.SessionID, Code: "code", Name: "x"})
	require.ErrorIs(t, err, ErrOAuthSessionNotFound, "expired sessions are rejected")
}

func TestOAuthFailedExchangeKeepsSessionForRetry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start, err := f.svc.StartAuth(ctx, f.sup, AuthModeOAuth, nil)
	require.NoError(t, err)

	f.oauth.err = errors.New("invalid code")
	_, err = f.svc.CompleteAuth(ctx, f.sup, AuthInput{SessionID: start.SessionID, Code: "bad", Name: "x"})
	require.Error(t, err)

	f.oauth.err = nil
	_, err = f.svc.CompleteAuth(ctx, f.sup, AuthInput{SessionID: start.SessionID, Code: "good", Name: "x"})
	require.NoError(t, err)
}

func TestAuthRejectsForeignProxyAndAccountBeforeExchange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.repo.proxyOwners[9] = f.other.ID
	foreignProxy := int64(9)

	_, err := f.svc.StartAuth(ctx, f.sup, AuthModeOAuth, &foreignProxy)
	require.ErrorIs(t, err, ErrProxyNotFound)

	f.repo.links[500] = &AccountLink{AccountID: 500, SupplierID: f.other.ID}
	_, err = f.svc.CookieAuth(ctx, f.sup, AuthInput{Mode: AuthModeOAuth, SessionKey: "sk", AccountID: 500})
	require.ErrorIs(t, err, ErrAccountNotFound)
	require.Nil(t, f.oauth.cookie, "ownership is checked before calling upstream")
}

func TestAccountLimitIsCheckedBeforeExchange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.sup.MaxAccounts = 1
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID}

	_, err := f.svc.CookieAuth(ctx, f.sup, AuthInput{Mode: AuthModeOAuth, SessionKey: "sk", Name: "n"})
	require.ErrorIs(t, err, ErrAccountLimitReached)
	require.Nil(t, f.oauth.cookie)
}

func TestCookieAuthReauthorizesExistingAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.admin.accounts[300] = &service.Account{ID: 300, Name: "old"}
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID, ReviewStatus: ReviewApproved}

	_, err := f.svc.CookieAuth(ctx, f.sup, AuthInput{Mode: AuthModeSetupToken, SessionKey: "sk", AccountID: 300})
	require.NoError(t, err)
	require.Equal(t, "inference", f.oauth.cookie.Scope)
	update := f.admin.updateInputs[300]
	require.Equal(t, AuthModeSetupToken, update.Type)
	require.Equal(t, "at", update.Credentials["access_token"])
	require.Nil(t, f.admin.createInput, "re-authorization must not create a new account")
}

func TestUpdateAccountRequiresOwnProxy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.admin.accounts[300] = &service.Account{ID: 300}
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID}
	f.repo.proxyOwners[9] = f.other.ID
	foreignProxy := int64(9)

	_, err := f.svc.UpdateAccount(ctx, f.sup, 300, AccountUpdate{ProxyID: &foreignProxy})
	require.ErrorIs(t, err, ErrProxyNotFound)

	_, err = f.svc.UpdateAccount(ctx, f.sup, 300, AccountUpdate{ClearProxy: true})
	require.NoError(t, err)
	require.Equal(t, int64(0), *f.admin.updateInputs[300].ProxyID, "0 clears the proxy upstream")

	_, err = f.svc.UpdateAccount(ctx, f.other, 300, AccountUpdate{Name: "steal"})
	require.ErrorIs(t, err, ErrAccountNotFound)
}

func TestCreateProxyValidatesHostAndCleansUpOnLinkFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.svc.validateProxy = validatePublicProxyHost

	_, err := f.svc.CreateProxy(ctx, f.sup, ProxyInput{Name: "p", Protocol: "socks5", Host: "127.0.0.1", Port: 1080})
	require.ErrorIs(t, err, errProxyHostNotAllowed)
	require.Nil(t, f.admin.createdProxy)

	f.svc.validateProxy = func(context.Context, string) error { return nil }
	f.repo.linkProxyErr = errors.New("db down")
	_, err = f.svc.CreateProxy(ctx, f.sup, ProxyInput{Name: "p", Protocol: "socks5", Host: "203.0.113.10", Port: 1080})
	require.Error(t, err)
	require.Equal(t, []int64{7}, f.admin.deletedProxy, "orphan proxy is removed")
}

func TestReviewAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.admin.accounts[300] = &service.Account{ID: 300}
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID, ReviewStatus: ReviewPending}
	groups := []int64{4, 5}
	concurrency := 2

	require.NoError(t, f.svc.ReviewAccount(ctx, 1, 300, ReviewInput{Action: "approve", GroupIDs: &groups, Concurrency: &concurrency}))
	require.Equal(t, &groups, f.admin.updateInputs[300].GroupIDs)
	require.True(t, f.admin.schedulable[300])
	require.Equal(t, ReviewApproved, f.repo.reviews[300])

	require.NoError(t, f.svc.ReviewAccount(ctx, 1, 300, ReviewInput{Action: "reject"}))
	require.False(t, f.admin.schedulable[300])
	require.Equal(t, ReviewRejected, f.repo.reviews[300])

	require.ErrorIs(t, f.svc.ReviewAccount(ctx, 1, 300, ReviewInput{Action: "maybe"}), ErrInvalidReview)
	require.ErrorIs(t, f.svc.ReviewAccount(ctx, 1, 404, ReviewInput{Action: "approve"}), ErrAccountNotFound)
}

func TestUsageSummaryScopesToOwnAccounts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID}
	f.repo.links[400] = &AccountLink{AccountID: 400, SupplierID: f.other.ID}
	f.repo.usageRows = []UsageRow{
		{Model: "claude-opus-5-5", UsageTotals: UsageTotals{RequestCount: 2, InputTokens: 100, OutputTokens: 50, Amount: 1.5}},
		{Model: "claude-fable-5-1", UsageTotals: UsageTotals{RequestCount: 1, OutputTokens: 10, Amount: 0.5}},
		{Model: "claude-opus-4-1", UsageTotals: UsageTotals{RequestCount: 1, CacheReadTokens: 7, Amount: 0.25}},
	}
	start := f.now.Add(-24 * time.Hour)

	summary, err := f.svc.UsageSummary(ctx, f.sup.ID, UsageFilter{Start: start, End: f.now})
	require.NoError(t, err)
	require.Equal(t, []int64{300}, f.repo.lastQuery.AccountIDs)
	require.Equal(t, int64(4), summary.Totals.RequestCount)
	require.InDelta(t, 2.25, summary.Totals.Amount, 1e-9)
	require.Len(t, summary.ByFamily, 2)
	require.Equal(t, FamilyOpus, summary.ByFamily[0].Family)
	require.Equal(t, int64(3), summary.ByFamily[0].RequestCount)
	require.Equal(t, int64(7), summary.ByFamily[0].CacheReadTokens)
	require.Equal(t, FamilyFable, summary.ByFamily[1].Family)

	_, err = f.svc.UsageSummary(ctx, f.sup.ID, UsageFilter{Start: start, End: f.now, AccountID: 400})
	require.ErrorIs(t, err, ErrAccountNotFound, "cannot filter on another supplier's account")

	_, err = f.svc.UsageSummary(ctx, f.sup.ID, UsageFilter{Start: f.now, End: start})
	require.ErrorIs(t, err, ErrInvalidPeriod)
}

func TestSettlementRequiresClosedPeriodAndSumsItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.repo.links[300] = &AccountLink{AccountID: 300, SupplierID: f.sup.ID}
	f.repo.usageRows = []UsageRow{
		{AccountID: 300, AccountName: "a", Model: "claude-opus-5-5", Family: FamilyOpus, UsageTotals: UsageTotals{RequestCount: 3, InputTokens: 10, Amount: 2}},
		{AccountID: 300, AccountName: "a", Model: "claude-fable-5-1", Family: FamilyFable, UsageTotals: UsageTotals{RequestCount: 1, OutputTokens: 5, Amount: 0.5}},
	}

	_, err := f.svc.CreateSettlement(ctx, 1, f.sup.ID, f.now.Add(-time.Hour), f.now.Add(time.Hour), "")
	require.Error(t, err, "open periods cannot be settled")

	st, err := f.svc.CreateSettlement(ctx, 1, f.sup.ID, f.now.Add(-48*time.Hour), f.now.Add(-24*time.Hour), " oct ")
	require.NoError(t, err)
	require.Equal(t, SettlementDraft, st.Status)
	require.Equal(t, "oct", st.Notes)
	require.Len(t, st.Items, 2)
	require.Equal(t, int64(4), st.RequestCount)
	require.InDelta(t, 2.5, st.Amount, 1e-9)
	require.Equal(t, int64(1), *st.CreatedBy)

	_, err = f.svc.GetSettlement(ctx, f.other.ID, st.ID)
	require.ErrorIs(t, err, ErrSettlementNotFound, "suppliers only see their own statements")
}

func TestSupplierInputValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.CreateSupplier(ctx, SupplierInput{Name: "x", DefaultConcurrency: 1})
	require.Error(t, err, "user_id or email is required")

	_, err = f.svc.CreateSupplier(ctx, SupplierInput{Email: "nobody@example.com", Name: "x", DefaultConcurrency: 1})
	require.ErrorIs(t, err, ErrUserNotFound)

	sup, err := f.svc.CreateSupplier(ctx, SupplierInput{UserID: 30, Name: " new ", DefaultConcurrency: 2})
	require.NoError(t, err)
	require.Equal(t, "new", sup.Name)
	require.Equal(t, StatusActive, sup.Status)

	_, err = f.svc.UpdateSupplier(ctx, sup.ID, SupplierInput{Name: "new", Status: "paused", DefaultConcurrency: 1})
	require.Error(t, err)
	_, err = f.svc.UpdateSupplier(ctx, sup.ID, SupplierInput{Name: "new", Status: StatusActive, DefaultConcurrency: 0})
	require.Error(t, err)
}
