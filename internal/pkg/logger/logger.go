// Package logger 结构化日志（标准库 slog，Go 1.21+）。
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New 按 level 构建 slog.Logger（debug/info/warn/error）。
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
