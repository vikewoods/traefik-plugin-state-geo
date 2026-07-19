package traefik_plugin_state_geo

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testDatabasePath = "testdata/GeoIP2-City-Test.mmdb"

type mockGeoResult struct {
	country   string
	state     string
	lookupErr error
}

func newTestMiddleware(t *testing.T, cfg *Config, geo mockGeoResult) *stateBlock {
	t.Helper()

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	cfg.DBPath = ""
	cfg.FailOpen = true
	cfg.LogLevel = "off"

	handler, err := New(context.Background(), next, cfg, "test-plugin")
	if err != nil {
		t.Fatalf("expected no error creating middleware, got %v", err)
	}

	sb, ok := handler.(*stateBlock)
	if !ok {
		t.Fatalf("expected handler to be *stateBlock")
	}

	sb.mockGeoLookup = func(_ net.IP) (string, string, error) {
		if geo.lookupErr != nil {
			return "", "", geo.lookupErr
		}
		return geo.country, geo.state, nil
	}

	return sb
}

func TestStateBlock(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedIPs = []string{"1.2.3.4", "81.2.69.160"}
	cfg.DBPath = testDatabasePath
	cfg.TemplateHTML = "<html><body>Access Denied for {{STATE}}</body></html>"

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "state-block-test")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		remoteAddr    string
		expectedCode  int
		expectContent string
	}{
		{
			name:         "Allowed US State (WA)",
			remoteAddr:   "216.160.83.56:1234",
			expectedCode: http.StatusOK,
		},
		{
			name:          "Blocked US State (CA)",
			remoteAddr:    "214.78.120.1:1234",
			expectedCode:  http.StatusForbidden,
			expectContent: "CA",
		},
		{
			name:         "Whitelisted IP (GB)",
			remoteAddr:   "81.2.69.160:1234",
			expectedCode: http.StatusOK,
		},
		{
			name:          "Blocked IP (SE)",
			remoteAddr:    "89.160.20.128:1234",
			expectedCode:  http.StatusForbidden,
			expectContent: "SE",
		},
		{
			name:          "Blocked IPv6 (JP)",
			remoteAddr:    "[2001:218::1]:1234",
			expectedCode:  http.StatusForbidden,
			expectContent: "JP",
		},
		{
			name:          "Blocked US Unknown Subdivision",
			remoteAddr:    "149.101.100.1:1234",
			expectedCode:  http.StatusForbidden,
			expectContent: "Unknown",
		},
		{
			name:         "Whitelisted IP (Regardless of Location)",
			remoteAddr:   "1.2.3.4:1234",
			expectedCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
			req.RemoteAddr = tt.remoteAddr

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.expectedCode {
				t.Errorf("%s: expected status %d, got %d", tt.name, tt.expectedCode, recorder.Code)
			}

			if tt.expectedCode == http.StatusForbidden {
				contentType := recorder.Header().Get("Content-Type")
				if !strings.Contains(contentType, "text/html") {
					t.Errorf("%s: expected Content-Type text/html, got %s", tt.name, contentType)
				}

				body := recorder.Body.String()
				if tt.expectContent != "" && !strings.Contains(body, tt.expectContent) {
					t.Errorf("%s: expected body to contain %s, but it didn't. Body: %s", tt.name, tt.expectContent, body)
				}
			}
		})
	}
}

func TestStateBlockClientIPSourcesWithFixture(t *testing.T) {
	tests := []struct {
		name                       string
		remoteAddr                 string
		headers                    map[string]string
		allowInvalidHeaderFallback bool
		expectedCode               int
	}{
		{
			name:         "direct IPv4",
			remoteAddr:   "214.78.120.1:443",
			expectedCode: http.StatusForbidden,
		},
		{
			name:       "Cloudflare header",
			remoteAddr: "10.17.1.10:443",
			headers: map[string]string{
				"CF-Connecting-IP": "214.78.120.1",
			},
			expectedCode: http.StatusForbidden,
		},
		{
			name:       "True-Client-IP fallback",
			remoteAddr: "10.17.1.10:443",
			headers: map[string]string{
				"True-Client-IP": "216.160.83.56",
			},
			expectedCode: http.StatusOK,
		},
		{
			name:       "X-Real-IP fallback",
			remoteAddr: "10.17.1.10:443",
			headers: map[string]string{
				"X-Real-IP": "214.78.120.1",
			},
			expectedCode: http.StatusForbidden,
		},
		{
			name:       "X-Forwarded-For chain",
			remoteAddr: "10.17.1.10:443",
			headers: map[string]string{
				"X-Forwarded-For": "216.160.83.56, 10.17.1.20",
			},
			expectedCode: http.StatusOK,
		},
		{
			name:       "RFC Forwarded IPv6",
			remoteAddr: "10.17.1.10:443",
			headers: map[string]string{
				"Forwarded": `for="[2001:218::1]:443";proto=https`,
			},
			expectedCode: http.StatusForbidden,
		},
		{
			name:                       "permissive malformed preferred header falls back",
			remoteAddr:                 "10.17.1.10:443",
			allowInvalidHeaderFallback: true,
			headers: map[string]string{
				"CF-Connecting-IP": "not-an-ip",
				"X-Real-IP":        "216.160.83.56",
			},
			expectedCode: http.StatusOK,
		},
		{
			name:       "untrusted peer cannot spoof headers",
			remoteAddr: "216.160.83.56:443",
			headers: map[string]string{
				"CF-Connecting-IP": "214.78.120.1",
			},
			expectedCode: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := CreateConfig()
			cfg.BlockedStates = []string{"CA"}
			cfg.TrustedProxyCIDRs = []string{"10.17.1.0/24"}
			cfg.ClientIPHeaders = []string{
				"CF-Connecting-IP",
				"True-Client-IP",
				"X-Forwarded-For",
				"Forwarded",
				"X-Real-IP",
			}
			cfg.RejectInvalidClientIPHeaders = !test.allowInvalidHeaderFallback
			cfg.DBPath = testDatabasePath
			cfg.LogLevel = "off"

			next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				rw.WriteHeader(http.StatusOK)
			})
			handler, err := New(context.Background(), next, cfg, "client-ip-fixture-test")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.RemoteAddr = test.remoteAddr
			for name, value := range test.headers {
				req.Header.Set(name, value)
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.expectedCode {
				t.Fatalf("status = %d, want %d", response.Code, test.expectedCode)
			}
		})
	}
}

func TestPathWhitelist(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedPaths = []string{"/health"}
	cfg.WhitelistedPathPrefixes = []string{"/.well-known", "/api/public"}
	cfg.DBPath = testDatabasePath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("Success"))
	})

	handler, err := New(ctx, next, cfg, "path-whitelist-test")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		path         string
		remoteAddr   string
		expectedCode int
		description  string
	}{
		{
			name:         "Well-Known ACME Challenge",
			path:         "/.well-known/acme-challenge/token123",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusOK,
			description:  "Should allow .well-known even from blocked state",
		},
		{
			name:         "Well-Known Root",
			path:         "/.well-known/",
			remoteAddr:   "89.160.20.128:1234",
			expectedCode: http.StatusOK,
			description:  "Should allow .well-known root from blocked country",
		},
		{
			name:         "Health Check Endpoint",
			path:         "/health",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusOK,
			description:  "Should allow health check from blocked state",
		},
		{
			name:         "Public API Endpoint",
			path:         "/api/public/status",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusOK,
			description:  "Should allow public API from blocked state",
		},
		{
			name:         "Non-Whitelisted Path Blocked",
			path:         "/admin",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should block non-whitelisted path from blocked state",
		},
		{
			name:         "Root Path Blocked",
			path:         "/",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should block root path from blocked state",
		},
		{
			name:         "Similar But Not Matching Path",
			path:         "/api/private/data",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should block similar but non-matching path",
		},
		{
			name:         "Exact Health Path Does Not Match Suffix",
			path:         "/healthz",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should not treat an exact path rule as a raw prefix",
		},
		{
			name:         "Public Prefix Is Segment Safe",
			path:         "/api/publicity",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should not match a similar path segment",
		},
		{
			name:         "Traversal Cannot Retain Whitelisted Prefix",
			path:         "/.well-known/../admin",
			remoteAddr:   "214.78.120.1:1234",
			expectedCode: http.StatusForbidden,
			description:  "Should normalize the request path before matching",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://localhost"+tt.path, nil)
			req.RemoteAddr = tt.remoteAddr

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.expectedCode {
				t.Errorf("%s: %s\nExpected status %d, got %d",
					tt.name, tt.description, tt.expectedCode, recorder.Code)
			}

			if tt.expectedCode == http.StatusOK {
				body := recorder.Body.String()
				if body != "Success" {
					t.Errorf("%s: Expected body 'Success', got '%s'", tt.name, body)
				}
			}
		})
	}
}

func TestPathWhitelistBypassesGeoLookup(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA", "NY", "TX"} // Block multiple states
	cfg.WhitelistedPathPrefixes = []string{"/.well-known"}
	cfg.DBPath = testDatabasePath

	ctx := context.Background()
	callCount := 0
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		callCount++
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "bypass-test")
	if err != nil {
		t.Fatal(err)
	}

	// Make request to whitelisted path with blocked CA IP
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/.well-known/acme-challenge/test", nil)
	req.RemoteAddr = "214.78.120.1:1234"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected path whitelist to bypass geo-blocking, got status %d", recorder.Code)
	}

	if callCount != 1 {
		t.Errorf("Expected next handler to be called once, was called %d times", callCount)
	}
}

func TestPathWhitelistWithProxyHeaders(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedPathPrefixes = []string{"/.well-known"}
	cfg.DBPath = testDatabasePath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "proxy-test")
	if err != nil {
		t.Fatal(err)
	}

	// Test with X-Forwarded-For header
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/.well-known/acme-challenge/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "214.78.120.1")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected whitelisted path to work with X-Forwarded-For, got status %d", recorder.Code)
	}
}

func TestNoDBPathFailOpenPassThrough(t *testing.T) {
	cfg := CreateConfig()
	cfg.DBPath = ""
	cfg.FailOpen = true

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("pass"))
	})

	handler, err := New(ctx, next, cfg, "no-dbpath-failopen-test")
	if err != nil {
		t.Fatalf("expected middleware initialization to succeed, got error: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	req.RemoteAddr = "76.79.129.110:1234"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if recorder.Body.String() != "pass" {
		t.Fatalf("expected body %q, got %q", "pass", recorder.Body.String())
	}
}

func TestMissingDBFileFailOpenPassThrough(t *testing.T) {
	cfg := CreateConfig()
	cfg.DBPath = "data/does-not-exist.mmdb"
	cfg.FailOpen = true

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("pass"))
	})

	handler, err := New(ctx, next, cfg, "missing-db-failopen-test")
	if err != nil {
		t.Fatalf("expected middleware initialization to succeed, got error: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	req.RemoteAddr = "76.79.129.110:1234"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if recorder.Body.String() != "pass" {
		t.Fatalf("expected body %q, got %q", "pass", recorder.Body.String())
	}
}

func TestMissingDBFileFailClosedReturnsError(t *testing.T) {
	cfg := CreateConfig()
	cfg.DBPath = "data/does-not-exist.mmdb"
	cfg.FailOpen = false

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	_, err := New(ctx, next, cfg, "missing-db-failclosed-test")
	if err == nil {
		t.Fatal("expected initialization to fail when dbPath is invalid and failOpen=false, got nil error")
	}
}

func TestTemplateHTMLUsedForBlockedResponse(t *testing.T) {
	cfg := CreateConfig()
	cfg.DBPath = ""
	cfg.FailOpen = true
	cfg.TemplateHTML = "<html><body>INLINE TEMPLATE {{STATE}}</body></html>"

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "template-html-test")
	if err != nil {
		t.Fatalf("expected middleware initialization to succeed, got error: %v", err)
	}

	stateBlock, ok := handler.(*stateBlock)
	if !ok {
		t.Fatalf("expected handler type *stateBlock, got %T", handler)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	stateBlock.serveBlocked(recorder, req, "CA", "")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}

	contentType := recorder.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected Content-Type to contain text/html, got %s", contentType)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "INLINE TEMPLATE CA") {
		t.Fatalf("expected inline template to be used, body: %s", body)
	}
	if strings.Contains(body, "Access Denied") {
		t.Fatalf("did not expect built-in fallback when templateHTML is set, body: %s", body)
	}
}

func TestCreateConfigDefaults(t *testing.T) {
	cfg := CreateConfig()

	if !cfg.BlockNonUS {
		t.Fatalf("expected BlockNonUS default to be true")
	}

	if !cfg.BlockUSStates {
		t.Fatalf("expected BlockUSStates default to be true")
	}

	expectedHeaders := []string{"X-Forwarded-For"}
	if len(cfg.ClientIPHeaders) != len(expectedHeaders) {
		t.Fatalf("expected %d default client IP headers, got %d", len(expectedHeaders), len(cfg.ClientIPHeaders))
	}
	for index, expected := range expectedHeaders {
		if cfg.ClientIPHeaders[index] != expected {
			t.Fatalf("client IP header %d = %q, want %q", index, cfg.ClientIPHeaders[index], expected)
		}
	}
	if len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("expected no trusted proxy CIDRs by default, got %v", cfg.TrustedProxyCIDRs)
	}
	if !cfg.RejectInvalidClientIPHeaders {
		t.Fatal("expected invalid trusted client IP headers to be rejected by default")
	}
}

func TestNonUSBlockedWhenEnabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockNonUS = true
	cfg.BlockUSStates = true

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "GB",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-US when blockNonUS=true, got %d", rr.Code)
	}
}

func TestNonUSAllowedWhenDisabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockNonUS = false
	cfg.BlockUSStates = true

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "GB",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-US when blockNonUS=false, got %d", rr.Code)
	}
}

func TestBlockedUSStateWhenEnabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockUSStates = true
	cfg.BlockedStates = []string{"CA"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "US",
		state:   "CA",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked US state when blockUSStates=true, got %d", rr.Code)
	}
}

func TestBlockedUSStateIgnoredWhenDisabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockUSStates = false
	cfg.BlockedStates = []string{"CA"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "US",
		state:   "CA",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for blocked US state when blockUSStates=false, got %d", rr.Code)
	}
}

func TestUSWithoutSubdivisionBlockedWhenStateBlockingEnabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockUSStates = true

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "US",
		state:   "",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for US without subdivision when blockUSStates=true, got %d", rr.Code)
	}
}

func TestUSWithoutSubdivisionAllowedWhenStateBlockingDisabled(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockUSStates = false

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "US",
		state:   "",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for US without subdivision when blockUSStates=false, got %d", rr.Code)
	}
}

func TestWhitelistedIPBypassesAllBlocking(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockNonUS = true
	cfg.BlockUSStates = true
	cfg.WhitelistedIPs = []string{"8.8.8.8"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "GB",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for whitelisted IP, got %d", rr.Code)
	}
}

func TestWhitelistedIPRulesBypassBlocking(t *testing.T) {
	tests := []struct {
		name           string
		whitelistedIPs []string
		remoteAddr     string
		headers        map[string]string
		expectedCode   int
	}{
		{
			name:           "exact IP",
			whitelistedIPs: []string{"8.8.8.8"},
			remoteAddr:     "8.8.8.8:12345",
			expectedCode:   http.StatusOK,
		},
		{
			name:           "IPv4 CIDR contains remote address",
			whitelistedIPs: []string{"203.0.113.0/24"},
			remoteAddr:     "203.0.113.42:12345",
			expectedCode:   http.StatusOK,
		},
		{
			name:           "IPv4 CIDR does not contain remote address",
			whitelistedIPs: []string{"203.0.113.0/24"},
			remoteAddr:     "198.51.100.42:12345",
			expectedCode:   http.StatusForbidden,
		},
		{
			name:           "IPv6 CIDR contains remote address",
			whitelistedIPs: []string{"2001:db8:abcd::/48"},
			remoteAddr:     "[2001:db8:abcd::1234]:12345",
			expectedCode:   http.StatusOK,
		},
		{
			name:           "Cloudflare connecting IP matches CIDR",
			whitelistedIPs: []string{"198.51.100.0/24"},
			remoteAddr:     "127.0.0.1:12345",
			headers: map[string]string{
				"Cf-Connecting-Ip": "198.51.100.24",
				"X-Forwarded-For":  "8.8.8.8",
			},
			expectedCode: http.StatusOK,
		},
		{
			name:           "X-Forwarded-For client before trusted proxy matches CIDR",
			whitelistedIPs: []string{"198.51.100.0/24"},
			remoteAddr:     "127.0.0.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "198.51.100.24, 127.0.0.2",
			},
			expectedCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateConfig()
			cfg.BlockNonUS = true
			cfg.BlockUSStates = true
			cfg.WhitelistedIPs = tt.whitelistedIPs
			if len(tt.headers) > 0 {
				cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
				cfg.ClientIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For"}
			}

			handler := newTestMiddleware(t, cfg, mockGeoResult{
				country: "GB",
			})

			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.RemoteAddr = tt.remoteAddr
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Fatalf("expected status %d, got %d", tt.expectedCode, rr.Code)
			}
		})
	}
}

func TestInvalidWhitelistedIPRulesAreRejected(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{name: "invalid ip", entry: "not-an-ip"},
		{name: "invalid cidr", entry: "203.0.113.0/99"},
		{name: "empty entry", entry: ""},
		{name: "whitespace entry", entry: "   "},
		{name: "scoped ipv6", entry: "fe80::1%eth0"},
	}

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CreateConfig()
			config.WhitelistedIPs = []string{test.entry}
			if _, err := New(context.Background(), next, config, "invalid-ip-whitelist-test"); err == nil {
				t.Fatal("New() error = nil, want invalid whitelist error")
			}
		})
	}
}

func TestWhitelistedPathBypassesAllBlocking(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockNonUS = true
	cfg.BlockUSStates = true
	cfg.WhitelistedPaths = []string{"/healthz"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "GB",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/healthz", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for whitelisted path, got %d", rr.Code)
	}
}

func TestCfConnectingIPTakesPriorityOverXForwardedFor(t *testing.T) {
	cfg := CreateConfig()
	cfg.BlockNonUS = true
	cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	cfg.ClientIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "GB",
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Cf-Connecting-Ip", "8.8.8.8")
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 because Cf-Connecting-Ip should be used first, got %d", rr.Code)
	}
}

func TestMiddlewareUsesResolvedIPv6ClientIPForGeoLookup(t *testing.T) {
	cfg := CreateConfig()
	cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	cfg.ClientIPHeaders = []string{"CF-Connecting-IP"}

	handler := newTestMiddleware(t, cfg, mockGeoResult{
		country: "US",
		state:   "NY",
	})

	var lookedUpIP string
	handler.mockGeoLookup = func(ip net.IP) (string, string, error) {
		lookedUpIP = ip.String()
		return "US", "NY", nil
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "2606:4700:4700::1111")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if lookedUpIP != "2606:4700:4700::1111" {
		t.Fatalf("GeoIP lookup address = %q, want %q", lookedUpIP, "2606:4700:4700::1111")
	}
}
