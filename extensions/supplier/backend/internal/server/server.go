package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/moxaitoken/sub2api-supplier/internal/accounts"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/finance"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"github.com/moxaitoken/sub2api-supplier/internal/usage"
)

type Config struct {
	Finance           *finance.Service
	Origin            string
	AdminOrigin       string
	InsecureLocalhost bool
	TrustedProxies    []netip.Prefix
	StaticDir         string
	Accounts          *accounts.Service
	Usage             *usage.Service
	Suppliers         *suppliers.Service
	Admin             *auth.Service
	LegacyTestMode    bool
}
type Server struct {
	service                   *auth.Service
	cfg                       Config
	cookieName, challengeName string
	mu                        sync.Mutex
	attempts                  map[string]bucket
}
type bucket struct {
	count int
	until time.Time
}

func New(service *auth.Service, cfg Config) (http.Handler, error) {
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, fmt.Errorf("origin must contain only scheme and host")
	}
	if cfg.InsecureLocalhost && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("insecure cookies are restricted to localhost")
	}
	if u.Scheme != "https" && !(cfg.InsecureLocalhost && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, fmt.Errorf("HTTPS origin required outside explicit localhost mode")
	}
	if cfg.AdminOrigin != "" {
		a, err := url.Parse(cfg.AdminOrigin)
		if err != nil || a.Host == "" || a.User != nil || cfg.AdminOrigin != a.Scheme+"://"+a.Host ||
			(a.Scheme != "https" && !(cfg.InsecureLocalhost && a.Scheme == "http" && (a.Hostname() == "127.0.0.1" || a.Hostname() == "localhost" || a.Hostname() == "::1"))) {
			return nil, fmt.Errorf("valid main admin origin required")
		}
	}
	s := &Server{service: service, cfg: cfg, cookieName: "__Host-supplier_session", challengeName: "__Host-supplier_challenge", attempts: make(map[string]bucket)}
	if cfg.InsecureLocalhost {
		s.cookieName = "supplier_session"
		s.challengeName = "supplier_challenge"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { s.reply(w, 200, map[string]string{"status": "ok"}) })
	if cfg.Suppliers != nil {
		if cfg.Admin == nil || !cfg.Admin.Administrator || service.Local == nil {
			return nil, fmt.Errorf("explicit local supplier store and independent admin identity required")
		}
		s.registerIdentityRoutes(mux)
	} else if cfg.LegacyTestMode {
		mux.HandleFunc("POST /api/supplier/v1/auth/login", s.login)
		mux.HandleFunc("POST /api/supplier/v1/auth/login/2fa", s.twoFA)
		mux.HandleFunc("POST /api/supplier/v1/auth/logout", s.logout)
		mux.HandleFunc("GET /api/supplier/v1/me", s.authorize(false, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			s.reply(w, 200, p.Me)
			return nil
		}))
		mux.HandleFunc("GET /api/supplier/v1/accounts", s.authorize(false, s.listAccounts))
		mux.HandleFunc("POST /api/supplier/v1/oauth/sessions", s.authorize(true, s.beginOAuth))
		mux.HandleFunc("POST /api/supplier/v1/oauth/sessions/{id}/complete", s.authorize(true, s.completeOAuth))
		mux.HandleFunc("POST /api/supplier/v1/accounts/{id}/pause", s.authorize(true, s.changeAccount(false)))
		mux.HandleFunc("GET /api/supplier/v1/review/accounts", s.authorize(false, s.reviewAccounts))
		mux.HandleFunc("POST /api/supplier/v1/review/accounts/{id}/approve", s.authorize(true, s.changeAccount(true)))
		mux.HandleFunc("GET /api/supplier/v1/operations/{id}", s.authorize(false, s.getAccountOperation))
	} else {
		return nil, fmt.Errorf("supplier identity configuration required")
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, ports.ErrNotFound) })
	if cfg.StaticDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(cfg.StaticDir)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		mainAdmin := cfg.AdminOrigin != "" && strings.HasPrefix(r.URL.Path, "/api/supplier/admin/v1/") &&
			!strings.Contains(r.URL.Path, "/auth/") && r.Header.Get("Authorization") != "" && r.Header.Get("Origin") == cfg.AdminOrigin
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != cfg.Origin && !mainAdmin {
			s.fail(w, r, problem(403, "ORIGIN_REJECTED"))
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

type fault struct {
	status int
	reason string
}

func (e *fault) Error() string                { return e.reason }
func problem(status int, reason string) error { return &fault{status, reason} }
func (s *Server) reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, reason := 503, "SERVICE_UNAVAILABLE"
	var f *fault
	if errors.As(err, &f) {
		status, reason = f.status, f.reason
	} else if errors.Is(err, ports.ErrUnauthenticated) {
		status, reason = 401, "AUTHENTICATION_REQUIRED"
	} else if errors.Is(err, ports.ErrConflict) {
		status, reason = 409, "RESOURCE_VERSION_CONFLICT"
	} else if errors.Is(err, ports.ErrNotFound) {
		status, reason = 404, "RESOURCE_NOT_FOUND"
	} else if errors.Is(err, ports.ErrForbidden) {
		status, reason = 403, "ACCESS_DENIED"
	}
	// Adapter errors expose only stable status/reason, never upstream messages.
	var remote interface {
		StatusCode() int
		ReasonCode() string
	}
	if errors.As(err, &remote) {
		status, reason = remote.StatusCode(), remote.ReasonCode()
	}
	id, _ := auth.NewID()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": http.StatusText(status), "reason": reason, "request_id": id})
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return problem(415, "JSON_REQUIRED")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return problem(400, "INVALID_REQUEST")
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return problem(400, "INVALID_REQUEST")
	}
	return nil
}
func (s *Server) client(r *http.Request) (ports.ClientInfo, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ports.ClientInfo{}, problem(400, "INVALID_CLIENT")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return ports.ClientInfo{}, problem(400, "INVALID_CLIENT")
	}
	addr = addr.Unmap()
	for _, prefix := range s.cfg.TrustedProxies {
		if prefix.Contains(addr) {
			if raw := r.Header.Get("X-Real-IP"); raw != "" {
				addr, err = netip.ParseAddr(raw)
				if err != nil {
					return ports.ClientInfo{}, problem(400, "INVALID_CLIENT")
				}
			}
			break
		}
	}
	return ports.ClientInfo{IP: addr.Unmap().String(), UserAgent: r.UserAgent(), Language: r.Header.Get("Accept-Language")}, nil
}
func (s *Server) cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	if len(c.Value) > 128 {
		return ""
	}
	return c.Value
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: !s.cfg.InsecureLocalhost, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expiry})
}
func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Secure: !s.cfg.InsecureLocalhost, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Server) allowLogin(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, b := range s.attempts {
		if now.After(b.until) {
			delete(s.attempts, k)
		}
	}
	b, ok := s.attempts[key]
	if !ok {
		if len(s.attempts) >= 10000 {
			return false
		}
		b = bucket{until: now.Add(time.Minute)}
	}
	b.count++
	s.attempts[key] = b
	return b.count <= 20
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	info, err := s.client(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.allowLogin(info.IP) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, problem(429, "LOGIN_RATE_LIMIT"))
		return
	}
	var input ports.LoginInput
	if err = decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	addr, e := mail.ParseAddress(input.Email)
	if e != nil || addr.Address != input.Email || input.Password == "" || len(input.Password) > 4096 {
		s.fail(w, r, problem(400, "INVALID_REQUEST"))
		return
	}
	result, err := s.service.Login(r.Context(), input, info)
	input.Password = ""
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loginReply(w, result)
}
func (s *Server) loginReply(w http.ResponseWriter, result auth.Login) {
	if result.Requires2FA {
		s.setCookie(w, s.challengeName, result.Cookie, result.ExpiresAt)
		s.reply(w, 200, map[string]any{"requires_2fa": true, "challenge_id": result.ChallengeID, "expires_at": result.ExpiresAt})
		return
	}
	s.clearCookie(w, s.challengeName)
	s.setCookie(w, s.cookieName, result.Cookie, result.ExpiresAt)
	s.reply(w, 200, map[string]bool{"requires_2fa": false})
}
func (s *Server) twoFA(w http.ResponseWriter, r *http.Request) {
	info, err := s.client(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.allowLogin(info.IP) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, problem(429, "LOGIN_RATE_LIMIT"))
		return
	}
	var input struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"totp_code"`
	}
	if err = decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	if !uuid.MatchString(input.ChallengeID) || !totp.MatchString(input.Code) {
		s.fail(w, r, problem(400, "INVALID_REQUEST"))
		return
	}
	result, err := s.service.Complete2FA(r.Context(), s.cookie(r, s.challengeName), input.ChallengeID, input.Code, info)
	s.clearCookie(w, s.challengeName)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loginReply(w, result)
}

func (s *Server) authorize(write bool, fn func(http.ResponseWriter, *http.Request, auth.Principal) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := s.client(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		cookie := s.cookie(r, s.cookieName)
		if cookie == "" {
			s.fail(w, r, problem(401, "AUTHENTICATION_REQUIRED"))
			return
		}
		err = s.service.Do(r.Context(), cookie, r.Header.Get("X-CSRF-Token"), write, info, func(p auth.Principal) error { return fn(w, r, p) })
		if err != nil {
			s.fail(w, r, err)
		}
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	info, err := s.client(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cookie := s.cookie(r, s.cookieName)
	if cookie == "" {
		s.clearCookie(w, s.cookieName)
		s.reply(w, 200, map[string]bool{"logged_out": true})
		return
	}
	err = s.service.Logout(r.Context(), cookie, r.Header.Get("X-CSRF-Token"), info)
	if err == nil || errors.Is(err, ports.ErrUnavailable) {
		s.clearCookie(w, s.cookieName)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.reply(w, 200, map[string]bool{"logged_out": true})
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var totp = regexp.MustCompile(`^[0-9]{6}$`)
