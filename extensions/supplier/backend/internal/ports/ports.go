// Package ports defines the supplier service's own contracts, not upstream Go types.
package ports

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("access denied")
	ErrConflict        = errors.New("version or idempotency conflict")
	ErrUnavailable     = errors.New("service unavailable")
)

type ClientInfo struct{ IP, UserAgent, Language string }

type LoginInput struct {
	Email                 string `json:"email"`
	Password              string `json:"password"`
	TurnstileToken        string `json:"turnstile_token,omitempty"`
	TencentCaptchaTicket  string `json:"tencent_captcha_ticket,omitempty"`
	TencentCaptchaRandstr string `json:"tencent_captcha_randstr,omitempty"`
}

type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type LoginResult struct {
	Tokens    Tokens
	Challenge string
}

type Profile struct {
	ID     int64  `json:"id,string"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type Upstream interface {
	Login(context.Context, LoginInput, ClientInfo) (LoginResult, error)
	Verify2FA(context.Context, string, string, ClientInfo) (Tokens, error)
	Profile(context.Context, string, ClientInfo) (Profile, error)
	Refresh(context.Context, string, ClientInfo) (Tokens, error)
	Logout(context.Context, string, ClientInfo) error
}

type Supplier struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	PortalStatus string `json:"portal_status"`
	Version      int64  `json:"version"`
}

type Member struct {
	ID             string `json:"id"`
	SupplierID     string `json:"supplier_id"`
	UpstreamUserID int64  `json:"upstream_user_id,string"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	Version        int64  `json:"version"`
}

type Membership struct {
	Member   Member
	Supplier Supplier
}

// Session stores only ciphertext and hashes. Cookie values never enter this type.
// WithSession must serialize callbacks across processes using a row lock, and
// persist callback updates only on success. Callers never log session values.
type Session struct {
	ID, Hash, InstanceID            string
	UserID                          int64
	SupplierID, MemberID, Kind      string
	Cipher                          []byte
	KeyID, CSRFHash, ClientHash     string
	ExpiresAt, LastSeenAt           time.Time
	Revoked                         bool
	ExternalPending                 bool
	Checkpoint                      func() error
	Version                         int64
	CredentialVersion, SessionEpoch int64
}

type Store interface {
	CreateSession(context.Context, Session) error
	WithSession(context.Context, string, func(*Session) error) error
	Membership(context.Context, string, int64) (Membership, error)
}
