package traefik_plugin_state_geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStateBlock(t *testing.T) {
	dbPath := "data/GeoLite2-City.mmdb"
	templatePath := "data/blocked.html"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoLite2-City.mmdb not found in data/ folder, skipping test")
	}

	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		_ = os.MkdirAll("data", 0755)
		dummyHTML := "<html><body>Access Denied for {{STATE}}</body></html>"
		_ = os.WriteFile(templatePath, []byte(dummyHTML), 0644)
	}

	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedIPs = []string{"1.2.3.4", "140.228.62.31"}
	cfg.DBPath = dbPath
	cfg.TemplatePath = templatePath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
			name:         "Allowed US State (NY)",
			remoteAddr:   "161.185.160.93:1234", // NYC IP
			expectedCode: http.StatusOK,
		},
		{
			name:          "Blocked US State (CA)",
			remoteAddr:    "76.79.129.110:1234", // San Francisco, CA
			expectedCode:  http.StatusForbidden,
			expectContent: "CA",
		},
		{
			name:         "Whitelisted IP (GB)",
			remoteAddr:   "140.228.62.31:1234", // GB IP
			expectedCode: http.StatusOK,
		},
		{
			name:          "Blocked IP (GB)",
			remoteAddr:    "140.228.62.32:1234", // GB IP
			expectedCode:  http.StatusForbidden,
			expectContent: "GB",
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

// NEW: Test specifically for whitelisted paths
func TestPathWhitelist(t *testing.T) {
	dbPath := "data/GeoLite2-City.mmdb"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoLite2-City.mmdb not found in data/ folder, skipping test")
	}

	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedPaths = []string{"/.well-known/", "/health", "/api/public/"}
	cfg.DBPath = dbPath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
			remoteAddr:   "76.79.129.110:1234", // CA IP that would normally be blocked
			expectedCode: http.StatusOK,
			description:  "Should allow .well-known even from blocked state",
		},
		{
			name:         "Well-Known Root",
			path:         "/.well-known/",
			remoteAddr:   "140.228.62.32:1234", // GB IP that would normally be blocked
			expectedCode: http.StatusOK,
			description:  "Should allow .well-known root from blocked country",
		},
		{
			name:         "Health Check Endpoint",
			path:         "/health",
			remoteAddr:   "76.79.129.110:1234", // CA IP
			expectedCode: http.StatusOK,
			description:  "Should allow health check from blocked state",
		},
		{
			name:         "Public API Endpoint",
			path:         "/api/public/status",
			remoteAddr:   "76.79.129.110:1234", // CA IP
			expectedCode: http.StatusOK,
			description:  "Should allow public API from blocked state",
		},
		{
			name:         "Non-Whitelisted Path Blocked",
			path:         "/admin",
			remoteAddr:   "76.79.129.110:1234", // CA IP
			expectedCode: http.StatusForbidden,
			description:  "Should block non-whitelisted path from blocked state",
		},
		{
			name:         "Root Path Blocked",
			path:         "/",
			remoteAddr:   "76.79.129.110:1234", // CA IP
			expectedCode: http.StatusForbidden,
			description:  "Should block root path from blocked state",
		},
		{
			name:         "Similar But Not Matching Path",
			path:         "/api/private/data",
			remoteAddr:   "76.79.129.110:1234", // CA IP
			expectedCode: http.StatusForbidden,
			description:  "Should block similar but non-matching path",
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

// NEW: Test path whitelist priority (should bypass geo-lookup entirely)
func TestPathWhitelistBypassesGeoLookup(t *testing.T) {
	dbPath := "data/GeoLite2-City.mmdb"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoLite2-City.mmdb not found in data/ folder, skipping test")
	}

	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA", "NY", "TX"} // Block multiple states
	cfg.WhitelistedPaths = []string{"/.well-known/"}
	cfg.DBPath = dbPath

	ctx := context.Background()
	callCount := 0
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		callCount++
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "bypass-test")
	if err != nil {
		t.Fatal(err)
	}

	// Make request to whitelisted path with blocked CA IP
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/.well-known/acme-challenge/test", nil)
	req.RemoteAddr = "76.79.129.110:1234" // CA IP

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected path whitelist to bypass geo-blocking, got status %d", recorder.Code)
	}

	if callCount != 1 {
		t.Errorf("Expected next handler to be called once, was called %d times", callCount)
	}
}

// NEW: Test X-Forwarded-For header with whitelisted paths
func TestPathWhitelistWithProxyHeaders(t *testing.T) {
	dbPath := "data/GeoLite2-City.mmdb"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoLite2-City.mmdb not found in data/ folder, skipping test")
	}

	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedPaths = []string{"/.well-known/"}
	cfg.DBPath = dbPath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "proxy-test")
	if err != nil {
		t.Fatal(err)
	}

	// Test with X-Forwarded-For header
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/.well-known/acme-challenge/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "76.79.129.110") // CA IP in header

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
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	_, err := New(ctx, next, cfg, "missing-db-failclosed-test")
	if err == nil {
		t.Fatal("expected initialization to fail when dbPath is invalid and failOpen=false, got nil error")
	}
}

func TestTemplateHTMLUsedForBlockedResponse(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := tmpDir + "/template.html"
	fileTemplate := "<html><body>FILE TEMPLATE {{STATE}}</body></html>"
	if err := os.WriteFile(templatePath, []byte(fileTemplate), 0644); err != nil {
		t.Fatalf("failed to write temp template file: %v", err)
	}

	cfg := CreateConfig()
	cfg.DBPath = ""
	cfg.FailOpen = true
	cfg.TemplatePath = templatePath
	cfg.TemplateHTML = "<html><body>INLINE TEMPLATE {{STATE}}</body></html>"

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "template-html-priority-test")
	if err != nil {
		t.Fatalf("expected middleware initialization to succeed, got error: %v", err)
	}

	stateBlock, ok := handler.(*StateBlock)
	if !ok {
		t.Fatalf("expected handler type *StateBlock, got %T", handler)
	}

	recorder := httptest.NewRecorder()
	stateBlock.serveBlocked(recorder, "CA")

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
	if strings.Contains(body, "FILE TEMPLATE") {
		t.Fatalf("did not expect templatePath content when templateHTML is set, body: %s", body)
	}
	if strings.Contains(body, "Access Denied") {
		t.Fatalf("did not expect built-in fallback when templateHTML is set, body: %s", body)
	}
}
