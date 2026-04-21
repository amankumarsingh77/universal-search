package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/term"
)

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

// ColorHandler writes human-readable, colored log output to a terminal.
type ColorHandler struct {
	w       io.Writer
	level   slog.Leveler
	attrs   []slog.Attr
	groups  []string
	mu      *sync.Mutex
	noColor bool
}

// NewColorHandler creates a handler that writes colored text to w.
// Color is auto-detected based on whether w is a terminal.
func NewColorHandler(w io.Writer, opts *slog.HandlerOptions) *ColorHandler {
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}
	return &ColorHandler{
		w:       w,
		level:   level,
		mu:      &sync.Mutex{},
		noColor: !isTerminal(w),
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle formats and writes a log record with ANSI colors when supported.
func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	levelColor := h.colorForLevel(r.Level)
	levelStr := r.Level.String()

	h.mu.Lock()
	defer h.mu.Unlock()

	// Time
	t := r.Time.Format("15:04:05.000")

	// Level with color
	if h.noColor {
		fmt.Fprintf(h.w, "%s %5s %s", t, levelStr, r.Message)
	} else {
		fmt.Fprintf(h.w, "%s%s %5s%s %s", colorGray, t, levelColor, levelStr, colorReset+r.Message)
	}

	// Groups prefix
	prefix := ""
	for _, g := range h.groups {
		prefix += g + "."
	}

	// Pre-defined attrs
	for _, a := range h.attrs {
		h.writeAttr(prefix, a)
	}

	// Record attrs
	r.Attrs(func(a slog.Attr) bool {
		h.writeAttr(prefix, a)
		return true
	})

	fmt.Fprintln(h.w)
	return nil
}

func (h *ColorHandler) writeAttr(prefix string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	if h.noColor {
		fmt.Fprintf(h.w, " %s%s=%v", prefix, a.Key, a.Value)
	} else {
		fmt.Fprintf(h.w, " %s%s%s=%v%s", colorGray, prefix, a.Key, a.Value, colorReset)
	}
}

// WithAttrs returns a new handler whose records carry the given attrs.
func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{
		w:       h.w,
		level:   h.level,
		attrs:   append(cloneAttrs(h.attrs), attrs...),
		groups:  cloneStrings(h.groups),
		mu:      h.mu,
		noColor: h.noColor,
	}
}

// WithGroup returns a new handler that prefixes attrs with the given group name.
func (h *ColorHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &ColorHandler{
		w:       h.w,
		level:   h.level,
		attrs:   cloneAttrs(h.attrs),
		groups:  append(cloneStrings(h.groups), name),
		mu:      h.mu,
		noColor: h.noColor,
	}
}

func (h *ColorHandler) colorForLevel(level slog.Level) string {
	if h.noColor {
		return ""
	}
	switch {
	case level >= slog.LevelError:
		return colorRed
	case level >= slog.LevelWarn:
		return colorYellow
	case level >= slog.LevelInfo:
		return colorCyan
	default:
		return colorGray
	}
}

func cloneAttrs(a []slog.Attr) []slog.Attr {
	c := make([]slog.Attr, len(a))
	copy(c, a)
	return c
}

func cloneStrings(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	return c
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
