package traefik_plugin_state_geo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type pluginLogger struct {
	logger          *slog.Logger
	includeClientIP bool
}

func newPluginLogger(name, rawLevel string, includeClientIP bool) (*pluginLogger, error) {
	levelName := strings.ToLower(strings.TrimSpace(rawLevel))
	if levelName == "" {
		levelName = "warn"
	}
	if levelName == "off" {
		return &pluginLogger{includeClientIP: includeClientIP}, nil
	}

	var level slog.Level
	switch levelName {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf(
			"invalid logLevel %q: expected off, error, warn, info, or debug",
			rawLevel,
		)
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler).With(
		slog.String("plugin", "state-geo-block"),
		slog.String("middleware", name),
	)

	return &pluginLogger{
		logger:          logger,
		includeClientIP: includeClientIP,
	}, nil
}

func (l *pluginLogger) debug(ctx context.Context, message, clientIP string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelDebug, message, clientIP, attrs...)
}

func (l *pluginLogger) warn(ctx context.Context, message, clientIP string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelWarn, message, clientIP, attrs...)
}

func (l *pluginLogger) error(ctx context.Context, message, clientIP string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelError, message, clientIP, attrs...)
}

func (l *pluginLogger) log(
	ctx context.Context,
	level slog.Level,
	message string,
	clientIP string,
	attrs ...slog.Attr,
) {
	if l == nil || l.logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.logger.Enabled(ctx, level) {
		return
	}
	if l.includeClientIP && clientIP != "" {
		attrs = append(attrs, slog.String("client_ip", clientIP))
	}

	l.logger.LogAttrs(ctx, level, message, attrs...)
}
