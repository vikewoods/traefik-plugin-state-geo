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
			name:       "Blocked State (CA)",
			remoteAddr: "8.8.8.8:1234",
			expected:   http.StatusForbidden,
		},
		{
			name:       "Allowed IP",
			remoteAddr: "1.1.1.1:1234",
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
