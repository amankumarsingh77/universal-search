package logger

import (
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// New creates a structured logger with dual output:
//   - Colored text to stderr at Info level (for terminal)
//   - JSON to a rotated log file at Debug level (for troubleshooting)
//
// The log file is written to <dataDir>/universal-search.log with rotation:
// 50 MB max size, 3 backups, 28 days retention, gzip compression.
func New(dataDir string) *slog.Logger {
	fileWriter := &lumberjack.Logger{
		Filename:   filepath.Join(dataDir, "universal-search.log"),
		MaxSize:    50, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
	}

	termHandler := NewColorHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})

	logger := slog.New(NewMultiHandler(termHandler, fileHandler))
	slog.SetDefault(logger)
	return logger
}
