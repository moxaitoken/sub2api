// Package accounts owns the supplier account workflow, not gateway scheduling.
package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type Service struct {
	Store          ports.AccountStore
	Upstream       ports.AccountUpstream
	Vault          *auth.Vault
	KeyID          string
	IdentityKey    []byte
	IdentityKeyID  string
	QuarantineID   int64
	ApprovedGroups []int64
	ProxyCapacity  int64
}

var claudeIdentityPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type fault struct {
	status int
	reason string
}

func (e *fault) Error() string                { return e.reason }
func (e *fault) StatusCode() int              { return e.status }
func (e *fault) ReasonCode() string           { return e.reason }
func failure(status int, reason string) error { return &fault{status, reason} }

type Authorization struct {
	ID        string    `json:"id"`
	URL       string    `json:"authorization_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) ready() error {
	if s == nil || s.Store == nil || s.Upstream == nil || s.Vault == nil || s.KeyID == "" || len(s.IdentityKey) != 32 || s.IdentityKeyID == "" || s.QuarantineID <= 0 {
		return failure(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
	}
	return nil
}

func (s *Service) Begin(ctx context.Context, supplier string, actor int64, name string) (Authorization, error) {
	var result Authorization
	if err := s.ready(); err != nil {
		return result, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return result, failure(400, "INVALID_ACCOUNT_NAME")
	}
	if err := s.Store.CheckAccountScope(ctx, supplier, actor, false); err != nil {
		return result, err
	}
	if err := s.Upstream.CheckQuarantine(ctx, s.QuarantineID); err != nil {
		return result, err
	}
	remote, err := s.Upstream.GenerateClaudeOAuth(ctx)
	if err != nil {
		return result, err
	}
	u, err := url.Parse(remote.URL)
	if err != nil || u.Scheme != "https" || u.Host != "claude.com" || u.Path != "/cai/oauth/authorize" || u.User != nil || u.Fragment != "" || u.Query().Get("state") == "" || remote.SessionID == "" {
		return result, failure(502, "INVALID_AUTHORIZATION_RESPONSE")
	}
	id, err := auth.NewID()
	if err != nil {
		return result, err
	}
	plain, _ := json.Marshal(remote)
	cipher, err := s.Vault.SealBlob("oauth:"+supplier+":"+id, plain)
	if err != nil {
		return result, err
	}
	expires := time.Now().Add(25 * time.Minute)
	o := ports.OAuthSession{ID: id, SupplierID: supplier, ActorID: actor, Name: name, Cipher: cipher, StateHash: auth.Hash(u.Query().Get("state")), KeyID: s.KeyID, ExpiresAt: expires}
	if err = s.Store.SaveOAuth(ctx, o); err != nil {
		return result, err
	}
	return Authorization{ID: id, URL: remote.URL, ExpiresAt: expires}, nil
}

func (s *Service) markUnknown(o ports.AccountOperation, a ports.Account, step string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if a.UpstreamID > 0 {
		_ = s.Upstream.SetAccountSchedulable(ctx, a.UpstreamID, false)
	}
	if a.ID != "" {
		a.Admission = "needs_attention"
		a.Desired = "paused"
		_ = s.Store.FinishAccount(ctx, o, a, "needs_attention", step)
	} else {
		_ = s.Store.CheckpointAccountOperation(ctx, o, "needs_attention", step)
	}
}

func (s *Service) Complete(ctx context.Context, supplier string, actor int64, id, code, key string) (out ports.AccountOperation, err error) {
	if err = s.ready(); err != nil {
		return out, err
	}
	if len(code) > 4096 {
		return out, failure(400, "INVALID_AUTHORIZATION_CODE")
	}
	o, err := s.Store.OAuth(ctx, supplier, actor, id)
	if err != nil {
		return out, err
	}
	parts := strings.Split(code, "#")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || subtle.ConstantTimeCompare([]byte(auth.Hash(parts[1])), []byte(o.StateHash)) != 1 {
		return out, failure(400, "OAUTH_STATE_MISMATCH")
	}
	opid, err := auth.NewID()
	if err != nil {
		return out, err
	}
	out, replay, err := s.Store.BeginAccountOperation(ctx, ports.AccountOperation{ID: opid, SupplierID: supplier, ActorID: actor, ResourceID: id, Action: "oauth.complete", Key: key, Hash: auth.Hash(id + ":" + code)})
	if err != nil {
		return ports.AccountOperation{}, err
	}
	if replay {
		return out, nil
	}
	var account ports.Account
	done := false
	defer func() {
		if !done {
			s.markUnknown(out, account, "authorization_result_unknown")
			out.State = "needs_attention"
			out.Step = "authorization_result_unknown"
		}
	}()
	if o.KeyID != s.KeyID || !time.Now().Before(o.ExpiresAt) {
		return out, failure(409, "OAUTH_EXPIRED")
	}
	plain, err := s.Vault.OpenBlob("oauth:"+supplier+":"+id, o.Cipher)
	if err != nil {
		return out, err
	}
	var remote ports.RemoteOAuth
	if err = json.Unmarshal(plain, &remote); err != nil {
		return out, failure(502, "INVALID_AUTHORIZATION_RESPONSE")
	}
	if err = s.Store.CheckAccountScope(ctx, supplier, actor, false); err != nil {
		return out, err
	}
	if err = s.Store.CheckpointAccountOperation(ctx, out, "running", "exchange_started"); err != nil {
		return out, err
	}
	tokens, err := s.Upstream.ExchangeClaudeOAuth(ctx, remote.SessionID, code)
	if err != nil {
		return out, err
	}
	defer func() { tokens = ports.ClaudeTokens{} }()
	if tokens.Access == "" || tokens.Refresh == "" || !claudeIdentityPattern.MatchString(tokens.AccountUUID) || tokens.ExpiresAt <= time.Now().Unix() {
		return out, failure(409, "ACCOUNT_IDENTITY_UNVERIFIED")
	}
	if err = s.Upstream.CheckClaudeIdentity(ctx, tokens.AccountUUID); err != nil {
		return out, err
	}
	mac := hmac.New(sha256.New, s.IdentityKey)
	mac.Write([]byte("claude.account.uuid:" + strings.ToLower(tokens.AccountUUID)))
	accountID, err := auth.NewID()
	if err != nil {
		return out, err
	}
	account, err = s.Store.ReserveAccount(ctx, accountID, o, hex.EncodeToString(mac.Sum(nil)), s.IdentityKeyID)
	if err != nil {
		return out, err
	}
	out.AccountID = account.ID
	if err = s.Store.CheckpointAccountOperation(ctx, out, "running", "identity_reserved"); err != nil {
		return out, err
	}
	if err = s.Store.CheckAccountScope(ctx, supplier, actor, false); err != nil {
		return out, err
	}
	if err = s.Upstream.CheckQuarantine(ctx, s.QuarantineID); err != nil {
		return out, err
	}
	if err = s.Store.CheckpointAccountOperation(ctx, out, "running", "account_create_started"); err != nil {
		return out, err
	}
	created, err := s.Upstream.CreateClaudeAccount(ctx, o.Name, out.ID, tokens, s.QuarantineID)
	if err != nil {
		return out, err
	}
	account.UpstreamID = created.ID
	if !matches(created, []int64{s.QuarantineID}) {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	if err = s.Upstream.SetAccountSchedulable(ctx, created.ID, false); err != nil {
		return out, err
	}
	read, err := s.Upstream.ReadAccount(ctx, created.ID)
	if err != nil {
		return out, err
	}
	if read.ID != created.ID || read.Schedulable || !matches(read, []int64{s.QuarantineID}) {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	account.Admission = "pending"
	account.Desired = "paused"
	if err = s.Store.FinishAccount(ctx, out, account, "succeeded", "awaiting_review"); err != nil {
		return out, err
	}
	done = true
	out.State = "succeeded"
	out.Step = "awaiting_review"
	return out, nil
}

func matches(a ports.RemoteAccount, groups []int64) bool {
	if a.ID <= 0 || a.Platform != "anthropic" || a.Type != "oauth" || a.Status != "active" || len(a.Groups) != len(groups) {
		return false
	}
	got := make([]int64, len(a.Groups))
	want := append([]int64(nil), groups...)
	for i, g := range a.Groups {
		got[i] = g.ID
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (s *Service) Change(ctx context.Context, supplier string, actor ports.Profile, id, key string, version int64, approve bool, reason string, verifyAdmin func(context.Context) error) (out ports.AccountOperation, err error) {
	if err = s.ready(); err != nil {
		return out, err
	}
	action := "account.pause"
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 || approve && reason == "" {
		return out, failure(400, "INVALID_REVIEW_REASON")
	}
	var account ports.Account
	if approve {
		if actor.Role != "admin" || verifyAdmin == nil {
			return out, ports.ErrForbidden
		}
		if err = verifyAdmin(ctx); err != nil {
			return out, err
		}
		if len(s.ApprovedGroups) == 0 {
			return out, failure(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
		}
		for _, g := range s.ApprovedGroups {
			if g <= 0 || g == s.QuarantineID {
				return out, ports.ErrForbidden
			}
		}
		account, err = s.Store.ReviewAccount(ctx, id)
		action = "account.approve"
		supplier = account.SupplierID
	} else {
		account, err = s.Store.Account(ctx, supplier, id)
	}
	if err != nil {
		return out, err
	}
	opid, err := auth.NewID()
	if err != nil {
		return out, err
	}
	payload, _ := json.Marshal(struct {
		ID      string
		Version int64
		Approve bool
		Reason  string
	}{id, version, approve, reason})
	out, replay, err := s.Store.BeginAccountOperation(ctx, ports.AccountOperation{ID: opid, SupplierID: supplier, ActorID: actor.ID, ResourceID: id, Action: action, Key: key, Hash: auth.Hash(string(payload)), ExpectedVersion: version, Reason: reason})
	if err != nil {
		return ports.AccountOperation{}, err
	}
	if replay {
		return out, nil
	}
	done, touched := false, false
	defer func() {
		if !done {
			if touched {
				s.markUnknown(out, account, "account_change_unknown")
				out.State = "needs_attention"
				out.Step = "account_change_unknown"
			} else {
				cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = s.Store.CheckpointAccountOperation(cleanup, out, "failed", "change_rejected")
				out.State = "failed"
				out.Step = "change_rejected"
			}
		}
	}()
	if account.UpstreamID <= 0 || approve && account.Admission != "pending" || !approve && account.Admission != "approved" {
		return out, failure(409, "ACCOUNT_STATE_CONFLICT")
	}
	if err = s.Store.CheckAccountScope(ctx, supplier, actor.ID, approve); err != nil {
		return out, err
	}
	current, err := s.Upstream.ReadAccount(ctx, account.UpstreamID)
	if err != nil {
		return out, err
	}
	groups := s.ApprovedGroups
	if approve {
		groups = []int64{s.QuarantineID}
	}
	if current.ID != account.UpstreamID || !matches(current, groups) || approve && current.Schedulable {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	touched = true
	if err = s.Upstream.SetAccountSchedulable(ctx, account.UpstreamID, false); err != nil {
		return out, err
	}
	if approve {
		if err = s.Upstream.SetAccountGroups(ctx, account.UpstreamID, s.ApprovedGroups); err != nil {
			return out, err
		}
		read, e := s.Upstream.ReadAccount(ctx, account.UpstreamID)
		if e != nil {
			return out, e
		}
		if read.ID != account.UpstreamID || read.Schedulable || !matches(read, s.ApprovedGroups) {
			return out, failure(409, "ACCOUNT_STATE_DRIFT")
		}
		if err = verifyAdmin(ctx); err != nil {
			return out, err
		}
		if err = s.Store.CheckAccountScope(ctx, supplier, actor.ID, true); err != nil {
			return out, err
		}
		if err = s.Upstream.SetAccountSchedulable(ctx, account.UpstreamID, true); err != nil {
			return out, err
		}
	}
	read, err := s.Upstream.ReadAccount(ctx, account.UpstreamID)
	if err != nil {
		return out, err
	}
	if read.ID != account.UpstreamID || read.Schedulable != approve || !matches(read, s.ApprovedGroups) {
		return out, failure(409, "ACCOUNT_STATE_DRIFT")
	}
	account.Desired = "paused"
	if approve {
		account.Admission = "approved"
		account.Desired = "running"
	}
	if err = s.Store.FinishAccount(ctx, out, account, "succeeded", "confirmed"); err != nil {
		return out, err
	}
	done = true
	out.State = "succeeded"
	out.Step = "confirmed"
	out.AccountID = account.ID
	return out, nil
}
