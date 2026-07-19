package traefik_plugin_state_geo

import (
	"bytes"
	"context"
	"errors"
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

func TestInvalidTrustedHeaderWarnsAndTriggersInvalidClientIPPolicy(t *testing.T) {
	config := CreateConfig()
	config.ClientIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For"}
	config.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	config.RejectInvalidClientIPHeaders = true
	config.InvalidClientIPPolicy = "deny"
	handler := newTestMiddleware(t, config, mockGeoResult{country: "US", state: "WA"})

	var output bytes.Buffer
	logger := &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	handler.logger = logger
	handler.clientIPResolver.logger = logger

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set("CF-Connecting-IP", "not-an-address")
	req.Header.Set("X-Forwarded-For", "216.160.83.56")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	logOutput := output.String()
	if !strings.Contains(logOutput, "trusted client ip header is invalid") ||
		!strings.Contains(logOutput, "Cf-Connecting-Ip") {
		t.Fatalf("missing invalid-header warning: %s", logOutput)
	}
	if strings.Contains(logOutput, "not-an-address") || strings.Contains(logOutput, "216.160.83.56") {
		t.Fatalf("invalid-header warning leaked header values: %s", logOutput)
	}
}

func TestPermissiveInvalidTrustedHeaderWarnsAndFallsBack(t *testing.T) {
	config := CreateConfig()
	config.ClientIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For"}
	config.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	config.RejectInvalidClientIPHeaders = false
	handler := newTestMiddleware(t, config, mockGeoResult{country: "US", state: "WA"})

	var output bytes.Buffer
	logger := &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	handler.logger = logger
	handler.clientIPResolver.logger = logger

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set("CF-Connecting-IP", "not-an-address")
	req.Header.Set("X-Forwarded-For", "216.160.83.56")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(output.String(), `"request_rejected":false`) {
		t.Fatalf("missing fallback warning attributes: %s", output.String())
	}
}

func TestDeniedRequestIsLoggedAtInfoWithoutClientIP(t *testing.T) {
	config := CreateConfig()
	handler := newTestMiddleware(t, config, mockGeoResult{country: "GB"})

	var output bytes.Buffer
	handler.logger = &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:443"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if !strings.Contains(output.String(), "request denied by geo policy") {
		t.Fatalf("missing info denial log: %s", output.String())
	}
	if strings.Contains(output.String(), "8.8.8.8") {
		t.Fatalf("denial log leaked client address: %s", output.String())
	}
}

func TestLookupFailureAlwaysLogsDiagnosticError(t *testing.T) {
	config := CreateConfig()
	config.LookupFailurePolicy = "allow"
	handler := newTestMiddleware(t, config, mockGeoResult{lookupErr: errors.New("fixture decode failed")})

	var output bytes.Buffer
	handler.logger = &pluginLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "8.8.8.8:443"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if !strings.Contains(output.String(), "fixture decode failed") {
		t.Fatalf("lookup error diagnostic was suppressed: %s", output.String())
	}
	if strings.Contains(output.String(), "8.8.8.8") {
		t.Fatalf("lookup failure log leaked client address: %s", output.String())
	}
}
