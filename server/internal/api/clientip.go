package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// DefaultTrustedProxies are private and loopback ranges: the compose network
// Caddy talks to us over, and localhost for development.
var DefaultTrustedProxies = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

// clientIP returns the address to rate-limit a request by. X-Forwarded-For is
// only believed when the direct peer is a trusted proxy. Walking the list
// from the right, the first untrusted hop is the client, so a client can't
// dodge limits by sending its own X-Forwarded-For. If every hop is trusted
// (e.g. Docker Desktop's NAT on a laptop) the leftmost entry is used.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	remote := remoteAddr(r)
	if !remote.IsValid() || !isTrusted(remote, trusted) {
		return remote.String()
	}

	var hops []netip.Addr
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(header, ",") {
			if addr, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
				hops = append(hops, addr.Unmap())
			}
		}
	}
	if len(hops) == 0 {
		return remote.String()
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !isTrusted(hops[i], trusted) {
			return hops[i].String()
		}
	}
	return hops[0].String()
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
