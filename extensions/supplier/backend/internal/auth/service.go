package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type Service struct {
	Store             ports.Store
	Upstream          ports.Upstream
	Vault             *Vault
	InstanceID, KeyID string
	Now               func() time.Time
	Local             ports.IdentityStore
	Administrator     bool
}
type Me struct {
	User      ports.Profile   `json:"user"`
	Supplier  *ports.Supplier `json:"supplier"`
	CSRFToken string          `json:"csrf_token"`
	bindingID string
}
type Login struct {
	Cookie      string
	Requires2FA bool
	ChallengeID string
	ExpiresAt   time.Time
}
type Principal struct {
	Me     Me
	Tokens ports.Tokens
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func binding(info ports.ClientInfo) string { return Hash(info.IP + "\x00" + info.UserAgent) }

func (s *Service) Login(ctx context.Context, input ports.LoginInput, info ports.ClientInfo) (Login, error) {
	result, err := s.Upstream.Login(ctx, input, info)
	if err != nil {
		return Login{}, err
	}
	if result.Challenge != "" {
		return s.createChallenge(ctx, result.Challenge, info)
	}
	return s.establish(ctx, result.Tokens, info)
}

func (s *Service) createChallenge(ctx context.Context, challenge string, info ports.ClientInfo) (Login, error) {
	id, err := NewID()
	if err != nil {
		return Login{}, err
	}
	cookie, err := RandomToken()
	if err != nil {
		return Login{}, err
	}
	session := ports.Session{ID: id, Hash: Hash(cookie), InstanceID: s.InstanceID, Kind: s.challengeKind(), KeyID: s.KeyID, ClientHash: binding(info), CSRFHash: Hash(""), ExpiresAt: s.now().Add(5 * time.Minute), LastSeenAt: s.now(), Version: 1}
	session.Cipher, err = s.Vault.seal(session, secrets{Challenge: challenge})
	if err != nil {
		return Login{}, err
	}
	if err = s.Store.CreateSession(ctx, session); err != nil {
		return Login{}, err
	}
	return Login{Cookie: cookie, Requires2FA: true, ChallengeID: id, ExpiresAt: session.ExpiresAt}, nil
}

func (s *Service) Complete2FA(ctx context.Context, cookie, id, code string, info ports.ClientInfo) (Login, error) {
	var tokens ports.Tokens
	var outcome error
	err := s.Store.WithSession(ctx, Hash(cookie), func(session *ports.Session) error {
		if session.Kind != s.challengeKind() || session.ID != id || session.Revoked || session.KeyID != s.KeyID || !s.now().Before(session.ExpiresAt) || session.ClientHash != binding(info) {
			return ports.ErrForbidden
		}
		value, err := s.Vault.open(*session)
		if err != nil {
			return err
		}
		// Consume before returning, even when the upstream verification fails.
		session.Revoked = true
		if session.Checkpoint == nil {
			return ports.ErrUnavailable
		}
		if err := session.Checkpoint(); err != nil {
			return err
		}
		tokens, outcome = s.Upstream.Verify2FA(ctx, value.Challenge, code, info)
		return nil
	})
	if err != nil {
		return Login{}, err
	}
	if outcome != nil {
		return Login{}, outcome
	}
	return s.establish(ctx, tokens, info)
}

func (s *Service) identity(ctx context.Context, tokens ports.Tokens, info ports.ClientInfo) (Me, error) {
	profile, err := s.Upstream.Profile(ctx, tokens.AccessToken, info)
	if err != nil {
		return Me{}, err
	}
	return s.membershipForProfile(ctx, profile)
}

func (s *Service) membershipForProfile(ctx context.Context, profile ports.Profile) (Me, error) {
	if profile.ID <= 0 || profile.Status != "active" || (profile.Role != "admin" && profile.Role != "user") {
		return Me{}, ports.ErrForbidden
	}
	me := Me{User: profile}
	if s.Administrator {
		if profile.Role != "admin" {
			return Me{}, ports.ErrForbidden
		}
		return me, nil
	}
	membership, err := s.Store.Membership(ctx, s.InstanceID, profile.ID)
	if errors.Is(err, ports.ErrNotFound) {
		return Me{}, ports.ErrForbidden
	}
	if err != nil {
		return Me{}, err
	}
	if membership.Member.ID == "" || membership.Supplier.ID == "" || membership.Member.SupplierID != membership.Supplier.ID || membership.Member.Role != "owner" || membership.Member.UpstreamUserID != profile.ID || membership.Member.Status != "active" || membership.Supplier.PortalStatus != "active" {
		return Me{}, ports.ErrForbidden
	}
	me.Supplier = &membership.Supplier
	me.bindingID = membership.Member.ID
	return me, nil
}

func (s *Service) establish(ctx context.Context, tokens ports.Tokens, info ports.ClientInfo) (Login, error) {
	if tokens.AccessToken == "" || !s.now().Before(tokens.ExpiresAt) {
		return Login{}, ports.ErrForbidden
	}
	me, err := s.identity(ctx, tokens, info)
	if err != nil {
		return Login{}, err
	}
	id, err := NewID()
	if err != nil {
		return Login{}, err
	}
	cookie, err := RandomToken()
	if err != nil {
		return Login{}, err
	}
	csrf, err := RandomToken()
	if err != nil {
		return Login{}, err
	}
	session := ports.Session{ID: id, Hash: Hash(cookie), InstanceID: s.InstanceID, UserID: me.User.ID, Kind: s.sessionKind(), KeyID: s.KeyID, ClientHash: binding(info), CSRFHash: Hash(csrf), ExpiresAt: s.now().Add(8 * time.Hour), LastSeenAt: s.now(), Version: 1}
	if me.Supplier != nil {
		session.SupplierID = me.Supplier.ID
		session.MemberID = me.bindingID
	}
	// Without a refresh token the local session cannot outlive the access token.
	if tokens.RefreshToken == "" && tokens.ExpiresAt.Before(session.ExpiresAt) {
		session.ExpiresAt = tokens.ExpiresAt
	}
	session.Cipher, err = s.Vault.seal(session, secrets{Tokens: tokens, CSRF: csrf})
	if err != nil {
		return Login{}, err
	}
	if err = s.Store.CreateSession(ctx, session); err != nil {
		return Login{}, err
	}
	return Login{Cookie: cookie, ExpiresAt: session.ExpiresAt}, nil
}

// Do serializes refresh and validation using a session row lock across replicas.
// Identity binding is reloaded after that transaction commits, avoiding nested
// database acquisitions while preserving a fresh supplier-isolation check.
func (s *Service) Do(ctx context.Context, cookie, csrf string, write bool, info ports.ClientInfo, fn func(Principal) error) error {
	if cookie == "" {
		return ports.ErrUnauthenticated
	}
	var outcome error
	var principal *Principal
	var expectedSupplier, expectedMember string
	err := s.Store.WithSession(ctx, Hash(cookie), func(session *ports.Session) error {
		if session.InstanceID != s.InstanceID || session.Revoked || session.ExternalPending || session.KeyID != s.KeyID || session.Kind != s.sessionKind() || !s.now().Before(session.ExpiresAt) || s.now().Sub(session.LastSeenAt) >= 30*time.Minute || session.ClientHash != binding(info) {
			session.Revoked = true
			outcome = ports.ErrForbidden
			return nil
		}
		value, err := s.Vault.open(*session)
		if err != nil {
			session.Revoked = true
			outcome = ports.ErrForbidden
			return nil
		}
		if write && subtle.ConstantTimeCompare([]byte(session.CSRFHash), []byte(Hash(csrf))) != 1 {
			return ports.ErrForbidden
		}
		if !s.now().Add(30 * time.Second).Before(value.Tokens.ExpiresAt) {
			if value.Tokens.RefreshToken == "" {
				session.Revoked = true
				outcome = ports.ErrForbidden
				return nil
			}
			if session.Checkpoint == nil {
				return ports.ErrUnavailable
			}
			session.ExternalPending = true
			if err := session.Checkpoint(); err != nil {
				return err
			}
			value.Tokens, err = s.Upstream.Refresh(ctx, value.Tokens.RefreshToken, info)
			if err != nil || value.Tokens.AccessToken == "" || !s.now().Before(value.Tokens.ExpiresAt) {
				session.Revoked = true
				outcome = ports.ErrForbidden
				return nil
			}
			session.ExternalPending = false
			// The refresh token has already rotated upstream. Preserve the known
			// result even when the subsequent profile read temporarily fails.
			session.Cipher, err = s.Vault.seal(*session, value)
			if err != nil {
				session.Revoked = true
				outcome = ports.ErrForbidden
				return nil
			}
		}
		profile, err := s.Upstream.Profile(ctx, value.Tokens.AccessToken, info)
		if err != nil {
			if errors.Is(err, ports.ErrForbidden) || errors.Is(err, ports.ErrNotFound) {
				session.Revoked = true
			}
			outcome = err
			return nil
		}
		if profile.ID != session.UserID || profile.Status != "active" || (profile.Role != "admin" && profile.Role != "user") {
			session.Revoked = true
			outcome = ports.ErrForbidden
			return nil
		}
		me := Me{User: profile, CSRFToken: value.CSRF}
		expectedSupplier, expectedMember = session.SupplierID, session.MemberID
		session.Cipher, err = s.Vault.seal(*session, value)
		if err != nil {
			return err
		}
		session.LastSeenAt = s.now()
		principal = &Principal{Me: me, Tokens: value.Tokens}
		return nil
	})
	if errors.Is(err, ports.ErrNotFound) {
		return ports.ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if outcome != nil {
		return outcome
	}
	if principal == nil {
		return ports.ErrForbidden
	}
	// No session DB connection is held while looking up membership. This also
	// works with a one-connection pool and avoids cross-session pool deadlocks.
	me, identityErr := s.membershipForProfile(ctx, principal.Me.User)
	if identityErr == nil && !s.Administrator && (me.Supplier.ID != expectedSupplier || me.bindingID != expectedMember) {
		identityErr = ports.ErrForbidden
	}
	if identityErr != nil {
		if errors.Is(identityErr, ports.ErrForbidden) || errors.Is(identityErr, ports.ErrNotFound) {
			if err := s.Store.WithSession(ctx, Hash(cookie), func(v *ports.Session) error { v.Revoked = true; return nil }); err != nil {
				return err
			}
		}
		return identityErr
	}
	me.CSRFToken = principal.Me.CSRFToken
	principal.Me = me
	return fn(*principal)
}

func (s *Service) Logout(ctx context.Context, cookie, csrf string, info ports.ClientInfo) error {
	var outcome error
	err := s.Store.WithSession(ctx, Hash(cookie), func(session *ports.Session) error {
		if session.InstanceID != s.InstanceID || session.Revoked || session.Kind != s.sessionKind() || session.ClientHash != binding(info) {
			return ports.ErrForbidden
		}
		if subtle.ConstantTimeCompare([]byte(session.CSRFHash), []byte(Hash(csrf))) != 1 {
			return ports.ErrForbidden
		}
		value, err := s.Vault.open(*session)
		if err != nil {
			return err
		}
		session.Revoked = true
		if session.Checkpoint == nil {
			return ports.ErrUnavailable
		}
		if err := session.Checkpoint(); err != nil {
			return err
		}
		if value.Tokens.RefreshToken != "" {
			outcome = s.Upstream.Logout(ctx, value.Tokens.RefreshToken, info)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Local revocation is the authoritative logout result. No upstream token is returned.
	if outcome != nil {
		return ports.ErrUnavailable
	}
	return nil
}

func (s *Service) sessionKind() string {
	if s.Administrator {
		return "admin"
	}
	return "supplier"
}
func (s *Service) challengeKind() string {
	if s.Administrator {
		return "admin_challenge"
	}
	return "challenge"
}
