package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net/url"
	"strings"
	"time"
)

func oauthURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "claude.com" && u.Path == "/cai/oauth/authorize" && u.User == nil && u.Fragment == "" && u.Query().Get("state") != ""
}
func (s *Service) BeginLocal(ctx context.Context, r ports.SupplierRecord, in ports.BeginOnboarding, key string) (Authorization, error) {
	var zero Authorization
	store, up, e := s.onboardingPorts()
	if e != nil {
		return zero, e
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 100 {
		return zero, failure(400, "INVALID_ACCOUNT_NAME")
	}
	if e = s.checkGroup(ctx, in.GroupID); e != nil {
		return zero, e
	}
	if e = s.Upstream.CheckQuarantine(ctx, s.QuarantineID); e != nil {
		return zero, e
	}
	id, e := auth.NewID()
	if e != nil {
		return zero, e
	}
	b, _ := json.Marshal(in)
	o := ports.BoundOAuth{OAuthSession: ports.OAuthSession{ID: id, SupplierID: r.Supplier.ID, Name: in.Name, KeyID: s.KeyID, StateHash: auth.Hash(id), ExpiresAt: time.Now().Add(25 * time.Minute)}, Proxy: ports.OwnProxy{ID: in.ProxyID}, GroupID: in.GroupID, BeginKey: key, RequestHash: s.Vault.Fingerprint("oauth.begin", b)}
	o, replay, e := store.ReserveOnboarding(ctx, r, o)
	if e != nil {
		return zero, e
	}
	if e = store.CheckOnboarding(ctx, o); e != nil {
		return zero, e
	}
	if replay {
		if o.Status != "created" || o.Expired(time.Now()) || o.KeyID != s.KeyID || len(o.Cipher) == 0 {
			return zero, ports.ErrConflict
		}
		plain, e := s.Vault.OpenBlob("oauth:"+o.SupplierID+":"+o.ID, o.Cipher)
		if e != nil {
			return zero, e
		}
		var remote ports.RemoteOAuth
		if json.Unmarshal(plain, &remote) != nil || !oauthURL(remote.URL) {
			return zero, ports.ErrUnavailable
		}
		return Authorization{ID: o.ID, URL: remote.URL, ExpiresAt: o.ExpiresAt}, nil
	}
	published := false
	defer func() {
		if !published {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_ = store.CancelOnboarding(cleanup, o.SupplierID, o.ID)
		}
	}()
	if e = s.checkProxy(ctx, o.Proxy); e != nil {
		return zero, e
	}
	remote, e := up.GenerateBoundOAuth(ctx, o.Proxy.UpstreamID)
	if e != nil {
		return zero, e
	}
	if !oauthURL(remote.URL) || remote.SessionID == "" {
		return zero, failure(502, "INVALID_AUTHORIZATION_RESPONSE")
	}
	parsed, _ := url.Parse(remote.URL)
	o.StateHash = auth.Hash(parsed.Query().Get("state"))
	plain, _ := json.Marshal(remote)
	o.Cipher, e = s.Vault.SealBlob("oauth:"+o.SupplierID+":"+o.ID, plain)
	if e != nil {
		return zero, e
	}
	if e = store.PublishOnboarding(ctx, o); e != nil {
		return zero, e
	}
	published = true
	return Authorization{ID: o.ID, URL: remote.URL, ExpiresAt: o.ExpiresAt}, nil
}
func normalizeAuthorizationCode(code string) string {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "https://") || strings.HasPrefix(code, "http://") {
		u, e := url.Parse(code)
		if e == nil && u.Query().Get("code") != "" && u.Query().Get("state") != "" {
			return u.Query().Get("code") + "#" + u.Query().Get("state")
		}
	}
	return code
}
func (s *Service) CompleteLocal(ctx context.Context, supplier, id, code, key string) (out ports.AccountOperation, err error) {
	store, up, err := s.onboardingPorts()
	if err != nil {
		return out, err
	}
	if len(code) > 8192 {
		return out, failure(400, "INVALID_AUTHORIZATION_CODE")
	}
	code = normalizeAuthorizationCode(code)
	o, err := store.Onboarding(ctx, supplier, id)
	if err != nil {
		return out, err
	}
	parts := strings.Split(code, "#")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || subtle.ConstantTimeCompare([]byte(auth.Hash(parts[1])), []byte(o.StateHash)) != 1 {
		return out, failure(400, "OAUTH_STATE_MISMATCH")
	}
	if err = store.CheckOnboarding(ctx, o); err != nil {
		return out, err
	}
	out, replay, err := store.ClaimOnboarding(ctx, o, key, s.Vault.Fingerprint("oauth.complete", []byte(id+":"+code)))
	if err != nil {
		return ports.AccountOperation{}, err
	}
	if replay {
		return out, nil
	}
	var account ports.Account
	done := false
	step := "exchange_result_unknown"
	knownFailure := false
	defer func() {
		if !done {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			state := "needs_attention"
			if knownFailure {
				state = "failed"
			}
			if account.UpstreamID > 0 {
				_ = s.Upstream.SetAccountSchedulable(cleanup, account.UpstreamID, false)
			}
			if account.ID != "" {
				account.Admission = "needs_attention"
				account.Desired = "paused"
			}
			_ = store.FinishOnboarding(cleanup, o, out, account, state, step)
			out.State = state
			out.Step = step
			out.AccountID = account.ID
		}
	}()
	if o.Expired(time.Now()) || o.KeyID != s.KeyID {
		knownFailure = true
		step = "authorization_expired"
		return out, failure(409, "OAUTH_EXPIRED")
	}
	plain, err := s.Vault.OpenBlob("oauth:"+o.SupplierID+":"+o.ID, o.Cipher)
	if err != nil {
		knownFailure = true
		step = "authorization_invalid"
		return out, err
	}
	var remote ports.RemoteOAuth
	if json.Unmarshal(plain, &remote) != nil {
		knownFailure = true
		return out, ports.ErrUnavailable
	}
	if err = s.checkProxy(ctx, o.Proxy); err != nil {
		knownFailure = true
		step = "proxy_rejected"
		return out, err
	}
	if err = s.checkGroup(ctx, o.GroupID); err != nil {
		knownFailure = true
		step = "group_rejected"
		return out, err
	}
	if err = store.CheckOnboarding(ctx, o); err != nil {
		knownFailure = true
		step = "identity_changed"
		return out, err
	}
	tokens, err := up.ExchangeBoundOAuth(ctx, remote.SessionID, code, o.Proxy.UpstreamID)
	if err != nil {
		return out, err
	}
	defer func() { tokens = ports.ClaudeTokens{} }()
	knownFailure = true
	step = "identity_unverified"
	if tokens.Access == "" || tokens.Refresh == "" || !claudeIdentityPattern.MatchString(tokens.AccountUUID) || tokens.ExpiresAt <= time.Now().Unix() {
		return out, failure(409, "ACCOUNT_IDENTITY_UNVERIFIED")
	}
	if o.Name == "" {
		o.Name = strings.TrimSpace(tokens.Email)
	}
	if o.Name == "" || len([]rune(o.Name)) > 100 {
		step = "account_name_required"
		return out, failure(400, "INVALID_ACCOUNT_NAME")
	}
	if err = s.Upstream.CheckClaudeIdentity(ctx, tokens.AccountUUID); err != nil {
		step = "identity_conflict"
		return out, err
	}
	mac := hmac.New(sha256.New, s.IdentityKey)
	mac.Write([]byte("claude.account.uuid:" + strings.ToLower(tokens.AccountUUID)))
	accountID, err := auth.NewID()
	if err != nil {
		return out, err
	}
	account, err = store.ReserveLocalAccount(ctx, o, accountID, hex.EncodeToString(mac.Sum(nil)), s.IdentityKeyID)
	if err != nil {
		step = "identity_conflict"
		return out, err
	}
	out.AccountID = account.ID
	if err = store.CheckOnboarding(ctx, o); err != nil {
		step = "identity_changed"
		return out, err
	}
	if err = s.checkProxy(ctx, o.Proxy); err != nil {
		step = "proxy_rejected"
		return out, err
	}
	if err = s.Upstream.CheckQuarantine(ctx, s.QuarantineID); err != nil {
		step = "quarantine_rejected"
		return out, err
	}
	knownFailure = false
	step = "account_create_unknown"
	if err = s.Store.CheckpointAccountOperation(ctx, out, "running", "account_create_started"); err != nil {
		return out, err
	}
	created, err := up.CreateBoundAccount(ctx, o.Name, out.ID, tokens, s.QuarantineID, o.Proxy.UpstreamID)
	if err != nil {
		return out, err
	}
	account.UpstreamID = created.ID
	if !matches(created, []int64{s.QuarantineID}) || created.ProxyID == nil || *created.ProxyID != o.Proxy.UpstreamID {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	if err = s.Upstream.SetAccountSchedulable(ctx, created.ID, false); err != nil {
		return out, err
	}
	read, err := s.Upstream.ReadAccount(ctx, created.ID)
	if err != nil {
		return out, err
	}
	if read.ID != created.ID || read.Schedulable || !matches(read, []int64{s.QuarantineID}) || read.ProxyID == nil || *read.ProxyID != o.Proxy.UpstreamID {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	if err = store.CheckOnboarding(ctx, o); err != nil {
		return out, err
	}
	if err = s.checkProxy(ctx, o.Proxy); err != nil {
		return out, err
	}
	if err = s.checkGroup(ctx, o.GroupID); err != nil {
		return out, err
	}
	step = "account_group_unknown"
	if err = s.Store.CheckpointAccountOperation(ctx, out, "running", "account_group_started"); err != nil {
		return out, err
	}
	if err = s.Upstream.SetAccountGroups(ctx, created.ID, []int64{o.GroupID}); err != nil {
		return out, err
	}
	read, err = s.Upstream.ReadAccount(ctx, created.ID)
	if err != nil {
		return out, err
	}
	if read.ID != created.ID || read.Schedulable || !matches(read, []int64{o.GroupID}) || read.ProxyID == nil || *read.ProxyID != o.Proxy.UpstreamID {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	// Manual admission is not reintroduced. Identity and binding are verified,
	// but scheduling stays paused until the separate eligibility/enable capability exists.
	account.Admission = "approved"
	account.Desired = "paused"
	step = "created_paused"
	if err = store.FinishOnboarding(ctx, o, out, account, "succeeded", step); err != nil {
		return out, err
	}
	done = true
	out.State = "succeeded"
	out.Step = step
	return out, nil
}
