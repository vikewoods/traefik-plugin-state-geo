package traefik_plugin_state_geo

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPluginLoggerRejectsInvalidLevel(t *testing.T) {
	if _, err := newPluginLogger("test", "verbose", false); err == nil {
		t.Fatal("newPluginLogger() error = nil, want invalid level error")
	}
}

func TestPluginLoggerOmitsClientIPByDefault(t *testing.T) {
	var output bytes.Buffer
	logger := &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	logger.debug(
		context.Background(),
		"request evaluated",
		"203.0.113.25",
		slog.String("country", "US"),
	)
	if strings.Contains(output.String(), "203.0.113.25") || strings.Contains(output.String(), "client_ip") {
		t.Fatalf("log unexpectedly contains client IP: %s", output.String())
	}
}

func TestPluginLoggerIncludesClientIPWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	logger := &pluginLogger{
		logger:          slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
		includeClientIP: true,
	}

	logger.debug(context.Background(), "request evaluated", "2001:db8::25")
	if !strings.Contains(output.String(), "2001:db8::25") || !strings.Contains(output.String(), "client_ip") {
		t.Fatalf("log does not contain opted-in client IP: %s", output.String())
	}
}

func TestPluginLoggerRespectsLevelAndOffMode(t *testing.T) {
	var output bytes.Buffer
	logger := &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	logger.debug(context.Background(), "debug message", "")
	if output.Len() != 0 {
		t.Fatalf("warn logger emitted debug output: %s", output.String())
	}

	offLogger, err := newPluginLogger("test", "off", true)
	if err != nil {
		t.Fatalf("newPluginLogger() error = %v", err)
	}
	offLogger.error(context.Background(), "error message", "203.0.113.25")
}

func TestMiddlewareErrorLogDoesNotLeakClientAddressByDefault(t *testing.T) {
	config := CreateConfig()
	config.InvalidClientIPPolicy = "allow"
	handler := newTestMiddleware(t, config, mockGeoResult{})

	var output bytes.Buffer
	handler.logger = &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.25:not-a-port"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if !strings.Contains(output.String(), "client ip resolution failed") {
		t.Fatalf("expected resolution error log, got: %s", output.String())
	}
	if strings.Contains(output.String(), "203.0.113.25") {
		t.Fatalf("default error log leaked client address: %s", output.String())
	}
}
