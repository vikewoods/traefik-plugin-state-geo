package traefik_plugin_state_geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStateBlock(t *testing.T) {
	dbPath := "data/GeoLite2-City.mmdb"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoLite2-City.mmdb not found in data/ folder, skipping test")
	}

	cfg := CreateConfig()
	cfg.BlockedStates = []string{"CA"}
	cfg.WhitelistedIPs = []string{"1.2.3.4"}
	cfg.DBPath = dbPath

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "state-block-test")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		remoteAddr string
		expected   int
	}{
		{
			name:       "Allowed US State (NY)",
			remoteAddr: "161.185.160.93:1234", // NYC IP
			expected:   http.StatusOK,
		},
		{
			name:       "Blocked US State (CA)",
			remoteAddr: "76.79.129.110:1234", // San Francisco, CA
			expected:   http.StatusForbidden,
		},
		{
			name:       "Blocked Foreign Country (UK)",
			remoteAddr: "140.228.62.31:1234", // UK IP
			expected:   http.StatusForbidden,
		},
		{
			name:       "Whitelisted IP (Regardless of Location)",
			remoteAddr: "1.2.3.4:1234",
			expected:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
			req.RemoteAddr = tt.remoteAddr

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.expected {
				t.Errorf("expected status %d, got %d", tt.expected, recorder.Code)
			}
		})
	}
}
