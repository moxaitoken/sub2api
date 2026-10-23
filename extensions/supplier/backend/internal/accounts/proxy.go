package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var deniedProxyPrefixes = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "fc00::/7", "fe80::/10", "ff00::/8"}

func publicProxyIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return false
	}
	for _, raw := range deniedProxyPrefixes {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return ip.Zone() == ""
}

// ParseProxy has no network side effects. The caller validates all DNS answers before use.
func ParseProxy(raw string) (ports.ProxyConfig, error) {
	var p ports.ProxyConfig
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\t") {
		return p, failure(400, "INVALID_PROXY")
	}
	if !strings.Contains(raw, "://") {
		parts := strings.SplitN(raw, ":", 4)
		if len(parts) == 4 {
			raw = "http://" + url.UserPassword(parts[2], parts[3]).String() + "@" + parts[0] + ":" + parts[1]
		} else {
			raw = "http://" + raw
		}
	}
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.Opaque != "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return p, failure(400, "INVALID_PROXY")
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return p, failure(400, "INVALID_PROXY")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 || u.Hostname() == "" || strings.Contains(u.Hostname(), "%") {
		return p, failure(400, "INVALID_PROXY")
	}
	if strings.Contains(u.Hostname(), ":") && !strings.HasPrefix(u.Host, "[") {
		return p, failure(400, "INVALID_PROXY")
	}
	p = ports.ProxyConfig{Protocol: u.Scheme, Host: strings.ToLower(u.Hostname()), Port: port}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
		if p.Username == "" || p.Password == "" || strings.TrimSpace(p.Username) != p.Username || strings.TrimSpace(p.Password) != p.Password || strings.ContainsAny(p.Username+p.Password, "\r\n\x00") {
			return p, failure(400, "INVALID_PROXY")
		}
	}
	return p, nil
}
func validateProxy(ctx context.Context, p ports.ProxyConfig) (ports.ProxyConfig, error) {
	return validateProxyResolved(ctx, p, net.DefaultResolver.LookupNetIP)
}
func validateProxyResolved(ctx context.Context, p ports.ProxyConfig, lookup func(context.Context, string, string) ([]netip.Addr, error)) (ports.ProxyConfig, error) {
	var ips []netip.Addr
	if ip, e := netip.ParseAddr(p.Host); e == nil {
		ips = []netip.Addr{ip}
	} else {
		var e error
		ips, e = lookup(ctx, "ip", p.Host)
		if e != nil || len(ips) == 0 {
			return p, failure(400, "PROXY_DNS_REJECTED")
		}
	}
	for _, ip := range ips {
		if !publicProxyIP(ip) {
			return p, failure(400, "PROXY_ADDRESS_REJECTED")
		}
	}
	return p, nil
}

// proxyDisplayAddress keeps the submitted endpoint without authentication material.
// It is for display only; outbound configuration also includes authentication.
func proxyDisplayAddress(p ports.ProxyConfig) string {
	return p.Protocol + "://" + net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}
func (s *Service) configHash(p ports.ProxyConfig) string {
	b, _ := json.Marshal(p)
	h := hmac.New(sha256.New, s.IdentityKey)
	h.Write([]byte("supplier-proxy-config-v1\x00"))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Service) onboardingPorts() (ports.OnboardingStore, ports.OnboardingUpstream, error) {
	if e := s.ready(); e != nil {
		return nil, nil, e
	}
	store, ok := s.Store.(ports.OnboardingStore)
	if !ok {
		return nil, nil, failure(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
	}
	up, ok := s.Upstream.(ports.OnboardingUpstream)
	if !ok {
		return nil, nil, failure(503, "ACCOUNT_SERVICE_NOT_CONFIGURED")
	}
	return store, up, nil
}
func (s *Service) OwnProxies(ctx context.Context, supplier string) ([]ports.OwnProxy, error) {
	store, _, e := s.onboardingPorts()
	if e != nil {
		return nil, e
	}
	return store.Proxies(ctx, supplier)
}
func (s *Service) DeleteOwnProxy(ctx context.Context, r ports.SupplierRecord, id string) error {
	if e := s.ready(); e != nil {
		return e
	}
	store, ok := s.Store.(ports.ProxyDeletionStore)
	if !ok {
		return ports.ErrUnavailable
	}
	up, ok := s.Upstream.(ports.ProxyDeletionUpstream)
	if !ok {
		return ports.ErrUnavailable
	}
	p, e := store.BeginProxyDelete(ctx, r, id)
	if e != nil {
		return e
	}
	// The persisted deleting state blocks new reservations while a remote outcome is unknown.
	if e = up.DeleteProxy(ctx, p.UpstreamID); e != nil {
		return e
	}
	return store.FinishProxyDelete(ctx, p)
}
func (s *Service) Groups(ctx context.Context) ([]ports.AccountGroup, error) {
	_, up, e := s.onboardingPorts()
	if e != nil {
		return nil, e
	}
	out := []ports.AccountGroup{}
	ids := s.ApprovedGroups
	if s.GroupPolicies != nil {
		ids, e = s.GroupPolicies.EnabledGroupIDs(ctx)
		if e != nil {
			return nil, e
		}
	}
	for _, id := range ids {
		g, e := up.AccountGroup(ctx, id)
		if e != nil {
			return nil, e
		}
		if g.ID != id || g.Platform != "anthropic" || g.Status != "active" {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}
func (s *Service) checkGroup(ctx context.Context, id int64) error {
	groups, e := s.Groups(ctx)
	if e != nil {
		return e
	}
	for _, g := range groups {
		if g.ID == id {
			return nil
		}
	}
	return ports.ErrForbidden
}
func (s *Service) checkProxy(ctx context.Context, p ports.OwnProxy) error {
	_, up, e := s.onboardingPorts()
	if e != nil {
		return e
	}
	if p.Status != "active" || p.UpstreamID <= 0 {
		return failure(409, "PROXY_NOT_AVAILABLE")
	}
	remote, e := up.ReadProxy(ctx, p.UpstreamID)
	if e != nil {
		return e
	}
	if _, e := validateProxy(ctx, remote.ProxyConfig); e != nil {
		return e
	}
	if remote.ID != p.UpstreamID || remote.Status != "active" || remote.Fallback != "none" || remote.Backup != nil || remote.ExpiresAt != nil && *remote.ExpiresAt <= time.Now().Unix() || s.configHash(remote.ProxyConfig) != p.ConfigHash {
		return failure(409, "PROXY_STATE_DRIFT")
	}
	return nil
}
func (s *Service) AddProxy(ctx context.Context, r ports.SupplierRecord, in ports.ProxyInput, key string) (ports.OwnProxy, error) {
	store, up, e := s.onboardingPorts()
	if e != nil {
		return ports.OwnProxy{}, e
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Region = strings.ToUpper(strings.TrimSpace(in.Region))
	if in.Region == "" || in.Region == "UNKNOWN" {
		in.Region = "unknown"
	}
	if in.Name == "" || len([]rune(in.Name)) > 100 || !(in.Region == "unknown" || strings.Contains("|US|CA|IT|GB|DE|FR|JP|KR|SG|AE|AU|VN|IN|CH|MY|TH|TW|BR|", "|"+in.Region+"|")) {
		return ports.OwnProxy{}, failure(400, "INVALID_PROXY")
	}
	cfg, e := ParseProxy(in.Address)
	if e != nil {
		return ports.OwnProxy{}, e
	}
	original, _ := json.Marshal(in)
	hash := s.Vault.Fingerprint("proxy.create", original)
	displayAddress := proxyDisplayAddress(cfg)
	cfg, e = validateProxy(ctx, cfg)
	if e != nil {
		return ports.OwnProxy{}, e
	}
	id, e := auth.NewID()
	if e != nil {
		return ports.OwnProxy{}, e
	}
	capacity := s.ProxyCapacity
	if capacity <= 0 {
		capacity = 1
	}
	p := ports.OwnProxy{ID: id, Name: in.Name, Address: displayAddress, Region: in.Region, Capacity: capacity, ConfigHash: s.configHash(cfg)}
	p, replay, e := store.ReserveProxy(ctx, r, p, key, hash)
	if e != nil {
		return p, e
	}
	if replay {
		if p.Status == "active" {
			return p, nil
		}
		if p.Status == "disabled" || p.Status == "deleting" {
			return ports.OwnProxy{}, s.rejectFailedProxy(ctx, r, p)
		}
		if p.Status == "deleted" {
			return ports.OwnProxy{}, failure(422, "PROXY_CHECK_FAILED")
		}
		return ports.OwnProxy{}, failure(503, "PROXY_CLEANUP_PENDING")
	}
	remote, e := up.CreateProxy(ctx, p.Name, cfg, p.OperationID)
	cfg.Password = ""
	cfg.Username = ""
	if e != nil {
		s.finishUnknownProxy(ctx, p, 0)
		return p, e
	}
	p.UpstreamID = remote.ID
	p.Status = "active"
	if e = s.checkProxy(ctx, p); e != nil {
		s.finishUnknownProxy(ctx, p, p.UpstreamID)
		return p, e
	}
	valid, e := up.TestProxy(ctx, p.UpstreamID)
	status := "active"
	if e != nil {
		status = "needs_attention"
	} else if !valid {
		status = "disabled"
	}
	if finish := store.FinishProxy(ctx, p, p.UpstreamID, status); finish != nil {
		return p, finish
	}
	if status != "active" {
		return ports.OwnProxy{}, s.rejectFailedProxy(ctx, r, p)
	}
	return store.Proxy(ctx, r.Supplier.ID, p.ID)
}

func (s *Service) rejectFailedProxy(ctx context.Context, r ports.SupplierRecord, p ports.OwnProxy) error {
	if e := s.DeleteOwnProxy(ctx, r, p.ID); e != nil {
		return failure(503, "PROXY_CLEANUP_PENDING")
	}
	return failure(422, "PROXY_CHECK_FAILED")
}

func (s *Service) RetestOwnProxy(ctx context.Context, supplier, id string) (ports.OwnProxy, error) {
	store, up, e := s.onboardingPorts()
	if e != nil {
		return ports.OwnProxy{}, e
	}
	p, e := store.Proxy(ctx, supplier, id)
	if e != nil {
		return p, e
	}
	if p.Status != "active" && p.Status != "disabled" {
		return p, ports.ErrConflict
	}
	p.Status = "active"
	if e = s.checkProxy(ctx, p); e != nil {
		return p, e
	}
	valid, e := up.TestProxy(ctx, p.UpstreamID)
	if e != nil {
		return p, e
	}
	if e = store.RetestProxy(ctx, p, valid); e != nil {
		return p, e
	}
	return store.Proxy(ctx, supplier, id)
}

func (s *Service) finishUnknownProxy(parent context.Context, p ports.OwnProxy, id int64) {
	store, _, e := s.onboardingPorts()
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer cancel()
	_ = store.FinishProxy(ctx, p, id, "needs_attention")
}
