package telemetry

import (
	"log/slog"
	"os"
	"strings"

	"finance-sys/internal/config"
)

func NewLogger(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case string(config.LogLevelDebug):
		return slog.LevelDebug
	case string(config.LogLevelWarn):
		return slog.LevelWarn
	case string(config.LogLevelError):
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
