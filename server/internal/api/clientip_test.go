package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"direct client, header ignored", "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"via trusted proxy", "172.18.0.5:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"spoofed left entry ignored", "172.18.0.5:5000", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"multiple headers", "10.0.0.2:1", []string{"6.6.6.6", "198.51.100.7, 10.0.0.9"}, "198.51.100.7"},
		{"all hops private (docker desktop)", "172.18.0.5:5000", []string{"192.168.65.1"}, "192.168.65.1"},
		{"trusted proxy, no header", "127.0.0.1:9", nil, "127.0.0.1"},
		{"garbage entries skipped", "10.0.0.2:1", []string{"nonsense, 198.51.100.7"}, "198.51.100.7"},
		{"ipv6 client", "[::1]:9", []string{"2001:db8::1"}, "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/rooms", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r, DefaultTrustedProxies); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
