package app

import (
	"context"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"
)

// exitProcess is overridable for tests; default terminates the process.
var exitProcess = func(code int) { os.Exit(code) }

// SetBaseContext stores a context that startup will derive its background
// errgroup from. Typically wired to a signal.NotifyContext in main(). Must be
// called before startup.
func (a *App) SetBaseContext(ctx context.Context) {
	a.baseCtx = ctx
}

// startErrgroup initialises a cancellable errgroup rooted at baseCtx. All
// long-running goroutines launched via a.group.Go participate in coordinated
// shutdown triggered by shutdownCancel().
func (a *App) startErrgroup() {
	base := a.baseCtx
	if base == nil {
		base = context.Background()
	}
	gctx, cancel := context.WithCancel(base)
	a.shutdownCancel = cancel
	a.group, a.groupCtx = errgroup.WithContext(gctx)
}

// shutdownWithTimeout cancels the errgroup context and waits for goroutines to
// finish, bounded by cfg.App.ShutdownTimeoutMs. Returns true when the wait
// timed out (i.e. at least one goroutine did not observe cancellation).
func (a *App) shutdownWithTimeout() bool {
	if a.shutdownCancel == nil || a.group == nil {
		return false
	}
	a.shutdownCancel()
	done := make(chan error, 1)
	go func() { done <- a.group.Wait() }()
	timeout := time.Duration(a.cfg.App.ShutdownTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	select {
	case err := <-done:
		if err != nil && a.logger != nil {
			a.logger.Warn("shutdown errgroup returned", "err", err)
		}
		return false
	case <-time.After(timeout):
		if a.logger != nil {
			a.logger.Warn("shutdown timeout", "ms", a.cfg.App.ShutdownTimeoutMs)
		}
		return true
	}
}

// emitBackendError sends a structured error event the frontend can display
// as a toast. A test-only sink short-circuits the runtime emit path.
func (a *App) emitBackendError(code, message string, fields map[string]any) {
	payload := map[string]any{"code": code, "message": message}
	for k, v := range fields {
		payload[k] = v
	}
	if a.backendErrorSink != nil {
		a.backendErrorSink(payload)
		return
	}
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "backend-error", payload)
}

// reportBackgroundError logs the error and emits a backend-error event if the
// error carries a stable apperr.Error code. Recoverable conditions (quota
// pauses, context cancellation) are intentionally filtered upstream.
func (a *App) reportBackgroundError(source string, err error) {
	if err == nil {
		return
	}
	if a.logger != nil {
		a.logger.Error("background task failed", "source", source, "err", err)
	}
	code, msg := deriveErrorCodeAndMessage(err)
	a.emitBackendError(code, msg, map[string]any{"source": source})
}
