package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustTrust(t *testing.T, raw string) ProxyTrust {
	t.Helper()
	trust, err := ParseProxyTrust(raw)
	if err != nil {
		t.Fatalf("ParseProxyTrust(%q): %v", raw, err)
	}
	return trust
}

func request(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/strict/resource", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestPresentedKey(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"api key wins over bearer", map[string]string{"X-API-Key": "key-1", "Authorization": "Bearer tok"}, "key-1"},
		{"bearer token stripped", map[string]string{"Authorization": "Bearer tok-2"}, "tok-2"},
		{"bearer is case-insensitive", map[string]string{"Authorization": "bearer tok-3"}, "tok-3"},
		{"other auth schemes ignored", map[string]string{"Authorization": "Basic abc"}, ""},
		{"no credentials", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PresentedKey(request("10.0.0.1:5555", tt.headers)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		trust      string
		remoteAddr string
		xff        string
		want       string
	}{
		// The spoofing bug: a client-supplied header must not choose the identity.
		{"untrusted by default ignores spoofed XFF", "", "203.0.113.9:4000", "1.2.3.4", "203.0.113.9"},
		{"one hop picks the rightmost entry", "1", "10.0.0.2:4000", "6.6.6.6, 198.51.100.7", "198.51.100.7"},
		{"two hops step one further left", "2", "10.0.0.2:4000", "6.6.6.6, 198.51.100.7, 10.0.0.3", "198.51.100.7"},
		{"hop count clamps to leftmost entry", "5", "10.0.0.2:4000", "198.51.100.7", "198.51.100.7"},
		{"hop count without XFF uses socket", "1", "10.0.0.2:4000", "", "10.0.0.2"},
		{"subnet list skips trusted hops", "10.0.0.0/8", "10.0.0.2:4000", "6.6.6.6, 198.51.100.7, 10.0.0.3", "198.51.100.7"},
		{"subnet list rejects untrusted socket", "10.0.0.0/8", "203.0.113.9:4000", "1.2.3.4", "203.0.113.9"},
		{"loopback alias", "loopback", "127.0.0.1:4000", "198.51.100.7", "198.51.100.7"},
		{"bare IP entry", "10.0.0.2", "10.0.0.2:4000", "198.51.100.7", "198.51.100.7"},
		{"IPv4-mapped forwarded address normalized", "1", "10.0.0.2:4000", "::ffff:198.51.100.7", "198.51.100.7"},
		{"IPv4-mapped socket address normalized", "", "[::ffff:203.0.113.9]:4000", "", "203.0.113.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.xff != "" {
				headers["X-Forwarded-For"] = tt.xff
			}
			got := mustTrust(t, tt.trust).ClientIP(request(tt.remoteAddr, headers))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseProxyTrustRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"-1", "not-an-ip", "10.0.0.0/99"} {
		if _, err := ParseProxyTrust(raw); err == nil {
			t.Errorf("ParseProxyTrust(%q) accepted invalid input", raw)
		}
	}
}
