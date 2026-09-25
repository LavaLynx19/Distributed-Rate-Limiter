// Package middleware holds the HTTP interception layer. Every limiter is a
// func(http.Handler) http.Handler so they compose with the standard library.
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// ProxyTrust decides which X-Forwarded-For hops may be believed. It mirrors
// Express's 'trust proxy' setting used by the Node gateway. The zero value
// trusts nothing: X-Forwarded-For is client-controlled, and honouring it
// blindly lets any client mint a fresh identifier per request.
type ProxyTrust struct {
	hops int          // trust this many hops nearest the server
	nets []*net.IPNet // or trust any hop inside these networks
}

var namedNets = map[string][]string{
	"loopback": {"127.0.0.0/8", "::1/128"},
}

// ParseProxyTrust accepts the TRUST_PROXY forms Express does, minus its
// rarely used aliases: empty, a hop count, or a comma-separated list of IPs,
// CIDRs, and the alias "loopback".
func ParseProxyTrust(raw string) (ProxyTrust, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ProxyTrust{}, nil
	}

	if hops, err := strconv.Atoi(raw); err == nil {
		if hops < 0 {
			return ProxyTrust{}, fmt.Errorf("TRUST_PROXY hop count must be >= 0, got %d", hops)
		}
		return ProxyTrust{hops: hops}, nil
	}

	var trust ProxyTrust
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		cidrs, ok := namedNets[item]
		if !ok {
			cidrs = []string{item}
		}
		for _, c := range cidrs {
			if !strings.Contains(c, "/") {
				if ip := net.ParseIP(c); ip != nil && ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
			_, network, err := net.ParseCIDR(c)
			if err != nil {
				return ProxyTrust{}, fmt.Errorf("TRUST_PROXY: invalid address %q", item)
			}
			trust.nets = append(trust.nets, network)
		}
	}
	return trust, nil
}

func (t ProxyTrust) trusts(addr string, hop int) bool {
	if t.nets == nil {
		return hop < t.hops
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP walks from the socket address outward through X-Forwarded-For
// (rightmost first) and returns the first hop not trusted to have forwarded
// the request honestly — the same answer Express gives for req.ip.
func (t ProxyTrust) ClientIP(r *http.Request) string {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}

	addrs := []string{remote}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		hops := strings.Split(forwarded, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addrs = append(addrs, strings.TrimSpace(hops[i]))
		}
	}

	for i := 0; i < len(addrs)-1; i++ {
		if !t.trusts(addrs[i], i) {
			return normalizeIP(addrs[i])
		}
	}
	return normalizeIP(addrs[len(addrs)-1])
}

// normalizeIP collapses IPv4-mapped IPv6 ("::ffff:1.2.3.4") to plain IPv4 so
// one client maps to one identity whichever path its request took.
func normalizeIP(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return addr
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

// PresentedKey returns the API key a request carries: X-API-Key, else an
// Authorization bearer token. Other auth schemes are ignored. The key is only
// a claim until the registry confirms it (ARCHITECTURE.md section 10).
func PresentedKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}
