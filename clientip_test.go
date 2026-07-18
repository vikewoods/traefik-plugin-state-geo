package traefik_plugin_state_geo

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type testHeaderValue struct {
	name  string
	value string
}

func TestClientIPResolverResolve(t *testing.T) {
	defaultHeaders := CreateConfig().ClientIPHeaders

	tests := []struct {
		name           string
		headerNames    []string
		trusted        []string
		remoteAddr     string
		headers        []testHeaderValue
		expectedAddr   string
		expectedSource string
	}{
		{
			name:        "untrusted peer cannot spoof Cloudflare header",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "198.51.100.10:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "203.0.113.25"},
			},
			expectedAddr:   "198.51.100.10",
			expectedSource: remoteAddrSource,
		},
		{
			name:        "trusted peer supplies Cloudflare IPv4",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "203.0.113.25"},
			},
			expectedAddr:   "203.0.113.25",
			expectedSource: "Cf-Connecting-Ip",
		},
		{
			name:        "trusted peer supplies Cloudflare IPv6",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "2a06:98c0:3600::103"},
			},
			expectedAddr:   "2a06:98c0:3600::103",
			expectedSource: "Cf-Connecting-Ip",
		},
		{
			name:        "configured order gives Cloudflare priority",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "203.0.113.25"},
				{name: "X-Real-IP", value: "198.51.100.25"},
				{name: "X-Forwarded-For", value: "192.0.2.25"},
			},
			expectedAddr:   "203.0.113.25",
			expectedSource: "Cf-Connecting-Ip",
		},
		{
			name:        "default order prefers forwarding chain over Traefik rewritten X-Real-IP",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "X-Forwarded-For", value: "198.51.100.25"},
				{name: "X-Real-IP", value: "10.17.1.12"},
			},
			expectedAddr:   "198.51.100.25",
			expectedSource: "X-Forwarded-For",
		},
		{
			name:        "malformed higher priority header falls back to X-Real-IP",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "not-an-address"},
				{name: "X-Real-IP", value: "198.51.100.25"},
			},
			expectedAddr:   "198.51.100.25",
			expectedSource: "X-Real-Ip",
		},
		{
			name:        "ambiguous single-IP header falls back to X-Forwarded-For",
			headerNames: defaultHeaders,
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "CF-Connecting-IP", value: "203.0.113.25"},
				{name: "CF-Connecting-IP", value: "203.0.113.26"},
				{name: "X-Forwarded-For", value: "198.51.100.25"},
			},
			expectedAddr:   "198.51.100.25",
			expectedSource: "X-Forwarded-For",
		},
		{
			name:        "X-Forwarded-For walks trusted chain from right",
			headerNames: []string{"X-Forwarded-For"},
			trusted:     []string{"10.17.1.0/24", "173.245.48.0/20"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "X-Forwarded-For", value: "203.0.113.25, 173.245.48.12, 10.17.1.20"},
			},
			expectedAddr:   "203.0.113.25",
			expectedSource: "X-Forwarded-For",
		},
		{
			name:        "X-Forwarded-For does not trust spoofed leftmost entry",
			headerNames: []string{"X-Forwarded-For"},
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "X-Forwarded-For", value: "192.0.2.66, 198.51.100.25"},
			},
			expectedAddr:   "198.51.100.25",
			expectedSource: "X-Forwarded-For",
		},
		{
			name:        "X-Forwarded-For returns leftmost when all entries are trusted",
			headerNames: []string{"X-Forwarded-For"},
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "X-Forwarded-For", value: "10.17.1.30, 10.17.1.20"},
			},
			expectedAddr:   "10.17.1.30",
			expectedSource: "X-Forwarded-For",
		},
		{
			name:        "RFC Forwarded parses quoted IPv6 and trusted chain",
			headerNames: []string{"Forwarded"},
			trusted:     []string{"10.17.1.0/24", "2001:db8:ffff::/48"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{
					name:  "Forwarded",
					value: `for="[2001:db8:1234::50]:4711";proto=https, for="[2001:db8:ffff::20]"`,
				},
			},
			expectedAddr:   "2001:db8:1234::50",
			expectedSource: "Forwarded",
		},
		{
			name:        "RFC Forwarded parses IPv4 with port",
			headerNames: []string{"Forwarded"},
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "Forwarded", value: "for=203.0.113.25:4711;proto=https"},
			},
			expectedAddr:   "203.0.113.25",
			expectedSource: "Forwarded",
		},
		{
			name:        "provider-specific custom single-IP header",
			headerNames: []string{"Fly-Client-IP"},
			trusted:     []string{"10.17.1.12"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "Fly-Client-IP", value: "2001:db8:1234::50"},
			},
			expectedAddr:   "2001:db8:1234::50",
			expectedSource: "Fly-Client-Ip",
		},
		{
			name:        "IPv4-mapped IPv6 is normalized",
			headerNames: []string{"X-Real-IP"},
			trusted:     []string{"::ffff:10.17.1.0/120"},
			remoteAddr:  "[::ffff:10.17.1.12]:443",
			headers: []testHeaderValue{
				{name: "X-Real-IP", value: "::ffff:203.0.113.25"},
			},
			expectedAddr:   "203.0.113.25",
			expectedSource: "X-Real-Ip",
		},
		{
			name:        "malformed forwarding chain falls back to socket peer",
			headerNames: []string{"X-Forwarded-For", "Forwarded"},
			trusted:     []string{"10.17.1.0/24"},
			remoteAddr:  "10.17.1.12:443",
			headers: []testHeaderValue{
				{name: "X-Forwarded-For", value: "203.0.113.25, unknown"},
				{name: "Forwarded", value: `for="[2001:db8::1]`},
			},
			expectedAddr:   "10.17.1.12",
			expectedSource: remoteAddrSource,
		},
		{
			name:           "raw IPv6 RemoteAddr fallback",
			headerNames:    defaultHeaders,
			trusted:        []string{},
			remoteAddr:     "2001:db8:1234::50",
			expectedAddr:   "2001:db8:1234::50",
			expectedSource: remoteAddrSource,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := newClientIPResolver(test.headerNames, test.trusted)
			if err != nil {
				t.Fatalf("newClientIPResolver() error = %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.RemoteAddr = test.remoteAddr
			for _, header := range test.headers {
				req.Header.Add(header.name, header.value)
			}

			resolved, err := resolver.resolve(req)
			if err != nil {
				t.Fatalf("resolve() error = %v", err)
			}
			if resolved.addr.String() != test.expectedAddr {
				t.Errorf("resolve() address = %q, want %q", resolved.addr, test.expectedAddr)
			}
			if resolved.source != test.expectedSource {
				t.Errorf("resolve() source = %q, want %q", resolved.source, test.expectedSource)
			}
		})
	}
}

func TestClientIPResolverRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		headerNames []string
		trusted     []string
	}{
		{
			name:        "empty header name",
			headerNames: []string{""},
		},
		{
			name:        "header name with colon",
			headerNames: []string{"X-Real-IP: bad"},
		},
		{
			name:        "header name with newline",
			headerNames: []string{"X-Real-IP\nInjected"},
		},
		{
			name:    "empty trusted proxy entry",
			trusted: []string{""},
		},
		{
			name:    "invalid trusted proxy CIDR",
			trusted: []string{"10.17.1.0/99"},
		},
		{
			name:    "invalid trusted proxy address",
			trusted: []string{"not-an-address"},
		},
		{
			name:    "IPv4-mapped prefix broader than mapped range",
			trusted: []string{"::ffff:10.17.1.0/80"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newClientIPResolver(test.headerNames, test.trusted)
			if err == nil {
				t.Fatal("newClientIPResolver() error = nil, want configuration error")
			}
		})
	}
}

func TestClientIPResolverRejectsInvalidRemoteAddr(t *testing.T) {
	resolver, err := newClientIPResolver(CreateConfig().ClientIPHeaders, []string{"10.17.1.0/24"})
	if err != nil {
		t.Fatalf("newClientIPResolver() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "not-an-address"
	if _, err := resolver.resolve(req); err == nil {
		t.Fatal("resolve() error = nil, want invalid RemoteAddr error")
	}
}
