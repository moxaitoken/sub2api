package supplier

import (
	"context"
	"net"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var errProxyHostNotAllowed = infraerrors.BadRequest("SUPPLIER_PROXY_HOST_NOT_ALLOWED",
	"proxy host must resolve to a public address")

// validatePublicProxyHost rejects proxy hosts that point into the server's own network.
// The gateway dials supplier proxies when testing and relaying, so a loopback or private
// address would let a supplier reach internal services (SSRF). Admin-created proxies are
// not subject to this check.
func validatePublicProxyHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return errProxyHostNotAllowed
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return errProxyHostNotAllowed
		}
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		return infraerrors.BadRequest("SUPPLIER_PROXY_HOST_UNRESOLVED", "proxy host could not be resolved")
	}
	for _, addr := range addrs {
		if !isPublicIP(addr.IP) {
			return errProxyHostNotAllowed
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		isSharedAddressSpace(ip))
}

// isSharedAddressSpace reports carrier-grade NAT addresses (100.64.0.0/10), which
// net.IP.IsPrivate does not cover but are commonly internal.
func isSharedAddressSpace(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64
}
