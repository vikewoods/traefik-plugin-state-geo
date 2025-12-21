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
			name:         "Blocked Foreign Country (UK)",
			remoteAddr:   "140.228.62.31:1234", // UK IP
			expectedCode: http.StatusOK,
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
