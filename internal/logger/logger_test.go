package logger

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiHandler_Enabled(t *testing.T) {
	h1 := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})

	mh := NewMultiHandler(h1, h2)
	ctx := context.Background()

	assert.True(t, mh.Enabled(ctx, slog.LevelInfo))
	assert.True(t, mh.Enabled(ctx, slog.LevelWarn))
	assert.False(t, mh.Enabled(ctx, slog.LevelDebug))
}

func TestMultiHandler_Handle_WritesToAll(t *testing.T) {
	var b1, b2 bytes.Buffer
	h1 := slog.NewJSONHandler(&b1, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewJSONHandler(&b2, &slog.HandlerOptions{Level: slog.LevelInfo})

	mh := NewMultiHandler(h1, h2)
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "test message", 0)
	rec.AddAttrs(slog.String("key", "value"))

	err := mh.Handle(context.Background(), rec)
	require.NoError(t, err)

	assert.Contains(t, b1.String(), "test message")
	assert.Contains(t, b1.String(), "key")
	assert.Contains(t, b2.String(), "test message")
	assert.Contains(t, b2.String(), "key")
}

func TestMultiHandler_Handle_RespectsLevel(t *testing.T) {
	var b1, b2 bytes.Buffer
	h1 := slog.NewJSONHandler(&b1, &slog.HandlerOptions{Level: slog.LevelWarn})
	h2 := slog.NewJSONHandler(&b2, &slog.HandlerOptions{Level: slog.LevelInfo})

	mh := NewMultiHandler(h1, h2)
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "info msg", 0)

	err := mh.Handle(context.Background(), rec)
	require.NoError(t, err)

	// h1 is Warn-only, should not receive Info
	assert.Empty(t, b1.String())
	// h2 is Info-level, should receive it
	assert.Contains(t, b2.String(), "info msg")
}

func TestMultiHandler_WithAttrs(t *testing.T) {
	var b bytes.Buffer
	h := slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo})
	mh := NewMultiHandler(h)

	child := mh.WithAttrs([]slog.Attr{slog.String("shared", "yes")})
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "with attrs", 0)

	err := child.Handle(context.Background(), rec)
	require.NoError(t, err)

	assert.Contains(t, b.String(), "shared")
	assert.Contains(t, b.String(), "yes")
}

func TestMultiHandler_WithGroup(t *testing.T) {
	var b bytes.Buffer
	h := slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo})
	mh := NewMultiHandler(h)

	child := mh.WithGroup("subsystem")
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "grouped", 0)
	rec.AddAttrs(slog.String("key", "val"))

	err := child.Handle(context.Background(), rec)
	require.NoError(t, err)

	assert.Contains(t, b.String(), "subsystem")
}

func TestMultiHandler_Handle_ErrorInOneHandler(t *testing.T) {
	// handler that always fails.
	// MultiHandler returns on first error, so the second handler is never called.
	failing := &failingHandler{}
	var b bytes.Buffer
	ok := slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo})

	mh := NewMultiHandler(failing, ok)
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "partial fail", 0)

	err := mh.Handle(context.Background(), rec)
	assert.Error(t, err)

	// MultiHandler stops at the first failing handler — the second handler is not called.
	assert.Empty(t, b.String())
}

func TestColorHandler_NoColor(t *testing.T) {
	var buf bytes.Buffer
	h := NewColorHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello world", 0)
	rec.AddAttrs(slog.String("component", "test"))

	err := h.Handle(context.Background(), rec)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "hello world")
	assert.Contains(t, out, "component=test")
	// No ANSI escapes when not a terminal
	assert.NotContains(t, out, "\033[")
}

func TestColorHandler_LevelColoring(t *testing.T) {
	var buf bytes.Buffer
	h := NewColorHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h.noColor = true // force no-color for deterministic output

	levels := []struct {
		level   slog.Level
		label   string
		message string
	}{
		{slog.LevelDebug, "DEBUG", "debug msg"},
		{slog.LevelInfo, "INFO", "info msg"},
		{slog.LevelWarn, "WARN", "warn msg"},
		{slog.LevelError, "ERROR", "error msg"},
	}

	for _, tc := range levels {
		buf.Reset()
		rec := slog.NewRecord(time.Time{}, tc.level, tc.message, 0)
		err := h.Handle(context.Background(), rec)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), tc.label)
		assert.Contains(t, buf.String(), tc.message)
	}
}

func TestColorHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewColorHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h.noColor = true

	child := h.WithAttrs([]slog.Attr{slog.String("base", "val")})
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "prefixed", 0)

	err := child.Handle(context.Background(), rec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "base=val")
}

func TestColorHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewColorHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h.noColor = true

	child := h.WithGroup("db")
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "query", 0)
	rec.AddAttrs(slog.String("sql", "SELECT 1"))

	err := child.Handle(context.Background(), rec)
	require.NoError(t, err)
	// Group prefix should appear before the attr key
	assert.True(t, strings.Contains(buf.String(), "db.") || strings.Contains(buf.String(), "db.sql"))
}

// failingHandler is a slog.Handler that always returns an error from Handle.
type failingHandler struct{}

func (f *failingHandler) Enabled(_ context.Context, _ slog.Level) bool  { return true }
func (f *failingHandler) Handle(_ context.Context, _ slog.Record) error { return errTestFailed }

var errTestFailed = errors.New("test: always fails")

func (f *failingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return f }
func (f *failingHandler) WithGroup(_ string) slog.Handler      { return f }
