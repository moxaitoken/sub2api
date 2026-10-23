package accounts

import (
	"context"
	"net/netip"
	"testing"
)

func TestProxyParserAndOutboundBoundary(t *testing.T) {
	for _, input := range []string{"8.8.8.8:8080", "8.8.8.8:8080:user:pass", "http://user:p%40ss@8.8.8.8:8080", "https://proxy.example.invalid:443", "socks5://8.8.8.8:1080", "http://[2606:4700:4700::1111]:8080"} {
		p, e := ParseProxy(input)
		if e != nil || p.Port < 1 {
			t.Error("valid format rejected", input, e)
		}
	}
	for _, input := range []string{"", "http://8.8.8.8", "http://8.8.8.8:0", "http://8.8.8.8:70000", "http://8.8.8.8:80/", "http://8.8.8.8:80?q=x", "http://8.8.8.8:80#x", "file://8.8.8.8:80", "http://u:%0a@8.8.8.8:80"} {
		if _, e := ParseProxy(input); e == nil {
			t.Error("invalid accepted", input)
		}
	}
	for _, input := range []string{"127.0.0.1", "10.0.0.1", "172.29.80.224", "169.254.169.254", "100.100.100.200", "192.168.1.1", "0.0.0.0", "224.0.0.1", "198.18.0.1", "::1", "fd00::1", "::ffff:127.0.0.1", "2002:7f00:1::", "64:ff9b::7f00:1"} {
		if publicProxyIP(netip.MustParseAddr(input)) {
			t.Error("private/reserved accepted", input)
		}
		p, _ := ParseProxy("http://" + netip.AddrPortFrom(netip.MustParseAddr(input), 80).String())
		if _, e := validateProxy(context.Background(), p); e == nil {
			t.Error("not rejected before upstream call", input)
		}
	}
	if !publicProxyIP(netip.MustParseAddr("8.8.8.8")) || !publicProxyIP(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public address rejected")
	}
}

func TestProxyDisplayPreservesEndpointWithoutCredentials(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"http://fixture-user:fixture-secret@proxy.example.invalid:1111", "http://proxy.example.invalid:1111"},
		{"proxy.example.invalid:1111:fixture-user:fixture-secret", "http://proxy.example.invalid:1111"},
		{"http://[2606:4700:4700::1111]:8080", "http://[2606:4700:4700::1111]:8080"},
	} {
		cfg, err := ParseProxy(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		display := proxyDisplayAddress(cfg)
		if display != tc.want {
			t.Fatalf("display=%q, want=%q", display, tc.want)
		}
	}
}

func TestDomainProxyUsesHostnameAndRejectsUnsafeDNSAnswers(t *testing.T) {
	p, err := ParseProxy("http://fixture-user:fixture-secret@proxy.example.invalid:1111")
	if err != nil {
		t.Fatal(err)
	}
	for _, addresses := range [][]string{{"8.8.8.8"}, {"8.8.4.4", "8.8.8.8"}, {"8.8.8.8", "127.0.0.1"}, {"169.254.169.254"}, {}} {
		lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if host != p.Host {
				t.Fatal("wrong lookup host")
			}
			ips := []netip.Addr{}
			for _, address := range addresses {
				ips = append(ips, netip.MustParseAddr(address))
			}
			return ips, nil
		}
		got, err := validateProxyResolved(context.Background(), p, lookup)
		valid := len(addresses) > 0
		for _, address := range addresses {
			valid = valid && publicProxyIP(netip.MustParseAddr(address))
		}
		if (err == nil) != valid {
			t.Fatalf("addresses=%v err=%v", addresses, err)
		}
		if valid && got != p {
			t.Fatal("DNS validation rewrote domain or credentials")
		}
	}
}
