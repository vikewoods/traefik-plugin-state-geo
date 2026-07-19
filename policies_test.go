package traefik_plugin_state_geo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateConfigDecisionPolicyDefaults(t *testing.T) {
	config := CreateConfig()
	tests := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "database failure", actual: config.DatabaseFailurePolicy, expected: "legacy"},
		{name: "lookup failure", actual: config.LookupFailurePolicy, expected: "allow"},
		{name: "invalid client ip", actual: config.InvalidClientIPPolicy, expected: "deny"},
		{name: "unknown country", actual: config.UnknownCountryPolicy, expected: "allow"},
		{name: "unknown subdivision", actual: config.UnknownSubdivisionPolicy, expected: "deny"},
		{name: "private ip", actual: config.PrivateIPPolicy, expected: "deny"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.actual != test.expected {
				t.Errorf("default = %q, want %q", test.actual, test.expected)
			}
		})
	}

	if config.DatabaseReloadInterval != defaultDatabaseReloadInterval.String() {
		t.Errorf(
			"database reload interval default = %q, want %q",
			config.DatabaseReloadInterval,
			defaultDatabaseReloadInterval.String(),
		)
	}
	if config.CacheSize != defaultDecisionCacheSize {
		t.Errorf("cache size default = %d, want %d", config.CacheSize, defaultDecisionCacheSize)
	}
	if config.CacheTTL != defaultDecisionCacheTTL.String() {
		t.Errorf("cache ttl default = %q, want %q", config.CacheTTL, defaultDecisionCacheTTL)
	}
	if len(config.WhitelistedPathPrefixes) != 0 {
		t.Errorf("whitelisted path prefix default = %v, want empty", config.WhitelistedPathPrefixes)
	}
	if config.LogLevel != "info" {
		t.Errorf("log level default = %q, want info", config.LogLevel)
	}
	if config.LogClientIP {
		t.Error("log client IP default = true, want false")
	}
}

func TestNewRejectsInvalidPolicyConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
	}{
		{
			name: "invalid database failure policy",
			configure: func(config *Config) {
				config.DatabaseFailurePolicy = "sometimes"
			},
		},
		{
			name: "invalid lookup failure policy",
			configure: func(config *Config) {
				config.LookupFailurePolicy = "sometimes"
			},
		},
		{
			name: "invalid client ip policy",
			configure: func(config *Config) {
				config.InvalidClientIPPolicy = "sometimes"
			},
		},
		{
			name: "invalid unknown country policy",
			configure: func(config *Config) {
				config.UnknownCountryPolicy = "sometimes"
			},
		},
		{
			name: "invalid unknown subdivision policy",
			configure: func(config *Config) {
				config.UnknownSubdivisionPolicy = "sometimes"
			},
		},
		{
			name: "invalid private ip policy",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "sometimes"
			},
		},
	}

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CreateConfig()
			test.configure(config)

			if _, err := New(context.Background(), next, config, "invalid-policy-test"); err == nil {
				t.Fatal("New() error = nil, want policy configuration error")
			}
		})
	}
}

func TestDatabaseFailurePolicy(t *testing.T) {
	tests := []struct {
		name            string
		dbPath          string
		policy          string
		failOpen        bool
		expectsNewError bool
		expectedStatus  int
	}{
		{
			name:           "allow with empty database path",
			policy:         "allow",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "deny with empty database path",
			policy:         "deny",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:            "error with empty database path",
			policy:          "error",
			expectsNewError: true,
		},
		{
			name:           "allow with missing database file",
			dbPath:         "data/does-not-exist.mmdb",
			policy:         "allow",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "deny with missing database file",
			dbPath:         "data/does-not-exist.mmdb",
			policy:         "deny",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:            "error with missing database file",
			dbPath:          "data/does-not-exist.mmdb",
			policy:          "error",
			expectsNewError: true,
		},
		{
			name:           "legacy fail open allows",
			policy:         "legacy",
			failOpen:       true,
			expectedStatus: http.StatusOK,
		},
		{
			name:            "legacy fail closed returns construction error",
			policy:          "legacy",
			failOpen:        false,
			expectsNewError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CreateConfig()
			config.DBPath = test.dbPath
			config.DatabaseFailurePolicy = test.policy
			config.FailOpen = test.failOpen

			next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				rw.WriteHeader(http.StatusOK)
			})
			handler, err := New(context.Background(), next, config, "database-policy-test")
			if test.expectsNewError {
				if err == nil {
					t.Fatal("New() error = nil, want database failure error")
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.RemoteAddr = "203.0.113.25:443"
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != test.expectedStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.expectedStatus)
			}
		})
	}
}

func TestRuntimeDecisionPolicies(t *testing.T) {
	tests := []struct {
		name            string
		configure       func(*Config)
		remoteAddr      string
		geo             mockGeoResult
		expectedStatus  int
		expectedLookups int
	}{
		{
			name: "invalid client ip allows",
			configure: func(config *Config) {
				config.InvalidClientIPPolicy = "allow"
			},
			remoteAddr:      "not-an-address",
			expectedStatus:  http.StatusOK,
			expectedLookups: 0,
		},
		{
			name: "invalid client ip denies",
			configure: func(config *Config) {
				config.InvalidClientIPPolicy = "deny"
			},
			remoteAddr:      "not-an-address",
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 0,
		},
		{
			name: "lookup failure allows",
			configure: func(config *Config) {
				config.LookupFailurePolicy = "allow"
			},
			remoteAddr: "8.8.8.8:443",
			geo: mockGeoResult{
				lookupErr: errors.New("lookup failed"),
			},
			expectedStatus:  http.StatusOK,
			expectedLookups: 1,
		},
		{
			name: "lookup failure denies",
			configure: func(config *Config) {
				config.LookupFailurePolicy = "deny"
			},
			remoteAddr: "8.8.8.8:443",
			geo: mockGeoResult{
				lookupErr: errors.New("lookup failed"),
			},
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 1,
		},
		{
			name: "unknown country allows",
			configure: func(config *Config) {
				config.UnknownCountryPolicy = "allow"
			},
			remoteAddr:      "8.8.8.8:443",
			expectedStatus:  http.StatusOK,
			expectedLookups: 1,
		},
		{
			name: "unknown country denies",
			configure: func(config *Config) {
				config.UnknownCountryPolicy = "deny"
			},
			remoteAddr:      "8.8.8.8:443",
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 1,
		},
		{
			name: "unknown subdivision allows",
			configure: func(config *Config) {
				config.UnknownSubdivisionPolicy = "allow"
				config.BlockUSStates = true
			},
			remoteAddr: "8.8.8.8:443",
			geo: mockGeoResult{
				country: "US",
			},
			expectedStatus:  http.StatusOK,
			expectedLookups: 1,
		},
		{
			name: "unknown subdivision denies",
			configure: func(config *Config) {
				config.UnknownSubdivisionPolicy = "deny"
				config.BlockUSStates = true
			},
			remoteAddr: "8.8.8.8:443",
			geo: mockGeoResult{
				country: "US",
			},
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 1,
		},
		{
			name: "private ip allows without lookup",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "allow"
			},
			remoteAddr: "10.17.1.25:443",
			geo: mockGeoResult{
				country: "GB",
			},
			expectedStatus:  http.StatusOK,
			expectedLookups: 0,
		},
		{
			name: "private ip denies without lookup",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "deny"
			},
			remoteAddr: "10.17.1.25:443",
			geo: mockGeoResult{
				country: "GB",
			},
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 0,
		},
		{
			name: "private ip uses lookup policy",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "lookup"
				config.BlockNonUS = true
			},
			remoteAddr: "10.17.1.25:443",
			geo: mockGeoResult{
				country: "GB",
			},
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 1,
		},
		{
			name: "IPv4 link-local address uses private deny policy",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "deny"
			},
			remoteAddr:      "169.254.10.20:443",
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 0,
		},
		{
			name: "IPv6 link-local address uses private allow policy",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "allow"
			},
			remoteAddr:      "[fe80::1234]:443",
			expectedStatus:  http.StatusOK,
			expectedLookups: 0,
		},
		{
			name: "IPv6 link-local address uses private lookup policy",
			configure: func(config *Config) {
				config.PrivateIPPolicy = "lookup"
				config.BlockNonUS = true
			},
			remoteAddr: "[fe80::1234]:443",
			geo: mockGeoResult{
				country: "GB",
			},
			expectedStatus:  http.StatusForbidden,
			expectedLookups: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CreateConfig()
			test.configure(config)

			handler := newTestMiddleware(t, config, test.geo)
			lookupCount := 0
			handler.mockGeoLookup = func(_ net.IP) (string, string, error) {
				lookupCount++
				return test.geo.country, test.geo.state, test.geo.lookupErr
			}

			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.RemoteAddr = test.remoteAddr
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != test.expectedStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.expectedStatus)
			}
			if lookupCount != test.expectedLookups {
				t.Errorf("GeoIP lookup count = %d, want %d", lookupCount, test.expectedLookups)
			}
		})
	}
}

func TestWhitelistsBypassDatabaseDenyPolicy(t *testing.T) {
	config := CreateConfig()
	config.DatabaseFailurePolicy = "deny"
	config.WhitelistedIPs = []string{"203.0.113.25"}
	config.WhitelistedPaths = []string{"/health"}

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
	handler, err := New(context.Background(), next, config, "database-deny-whitelist-test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name           string
		path           string
		remoteAddr     string
		expectedStatus int
	}{
		{
			name:           "path whitelist bypasses unavailable database",
			path:           "/health",
			remoteAddr:     "198.51.100.25:443",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "ip whitelist bypasses unavailable database",
			path:           "/",
			remoteAddr:     "203.0.113.25:443",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "other request is denied",
			path:           "/",
			remoteAddr:     "198.51.100.25:443",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com"+test.path, nil)
			req.RemoteAddr = test.remoteAddr
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != test.expectedStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.expectedStatus)
			}
		})
	}
}
