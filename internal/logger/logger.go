package logger

import (
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// New creates a structured logger with dual output:
//   - Colored text to stderr at Debug level when UNIVERSAL_SEARCH_DEBUG_STDERR=1,
//     otherwise at Info level (production default).
//   - JSON to a rotated log file at Debug level (for troubleshooting).
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

	termLevel := slog.LevelInfo
	if os.Getenv("UNIVERSAL_SEARCH_DEBUG_STDERR") == "1" {
		termLevel = slog.LevelDebug
	}

	termHandler := NewColorHandler(os.Stderr, &slog.HandlerOptions{
		Level: termLevel,
	})

	fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})

	logger := slog.New(NewMultiHandler(termHandler, fileHandler))
	slog.SetDefault(logger)
	return logger
}
