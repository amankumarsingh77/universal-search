package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"universal-search/internal/apperr"
	"universal-search/internal/config"
)

// TestApp_SetBaseContext_StoresContext verifies SetBaseContext stores ctx for startup use.
func TestApp_SetBaseContext_StoresContext(t *testing.T) {
	a := NewApp(nil)
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	a.SetBaseContext(ctx)
	if a.baseCtx == nil {
		t.Fatal("SetBaseContext did not store ctx")
	}
	if a.baseCtx.Value(key{}) != "marker" {
		t.Fatalf("baseCtx lost its value")
	}
}

// TestApp_EmitBackendError_NoContextNoOp verifies no panic when ctx is nil.
func TestApp_EmitBackendError_NoContextNoOp(t *testing.T) {
	a := &App{logger: slog.Default()}
	// Must not panic despite nil ctx.
	a.emitBackendError("ERR_TEST", "test", nil)
}

// TestApp_EmitBackendError_InvokesSink verifies the backend-error test hook is called.
func TestApp_EmitBackendError_InvokesSink(t *testing.T) {
	a := &App{logger: slog.Default(), ctx: context.Background()}
	var mu sync.Mutex
	got := make([]map[string]any, 0)
	a.backendErrorSink = func(payload map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, payload)
	}
	a.emitBackendError(apperr.ErrEmbedFailed.Code, "embed failed", map[string]any{"file": "/foo"})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(got))
	}
	p := got[0]
	if p["code"] != apperr.ErrEmbedFailed.Code {
		t.Errorf("expected code %q, got %v", apperr.ErrEmbedFailed.Code, p["code"])
	}
	if p["message"] != "embed failed" {
		t.Errorf("expected message 'embed failed', got %v", p["message"])
	}
	if p["file"] != "/foo" {
		t.Errorf("expected file '/foo', got %v", p["file"])
	}
}

// TestApp_BackgroundError_EmitsBackendErrorEvent: when a background task returns
// a non-retriable error, emitBackendError is invoked.
func TestApp_BackgroundError_EmitsBackendErrorEvent(t *testing.T) {
	a := &App{
		cfg:    config.DefaultConfig(),
		logger: slog.Default(),
		ctx:    context.Background(),
	}
	var mu sync.Mutex
	received := 0
	a.backendErrorSink = func(payload map[string]any) {
		mu.Lock()
		received++
		mu.Unlock()
	}
	a.reportBackgroundError("task-x", apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed died", errors.New("boom")))

	mu.Lock()
	defer mu.Unlock()
	if received != 1 {
		t.Fatalf("expected 1 background-error event, got %d", received)
	}
}

// TestShutdown_CleanExitOnCancel: shutdownWithTimeout returns cleanly when all
// background goroutines finish before the timeout elapses.
func TestShutdown_CleanExitOnCancel(t *testing.T) {
	a := &App{
		cfg:    config.DefaultConfig(),
		logger: slog.Default(),
		ctx:    context.Background(),
	}
	a.cfg.App.ShutdownTimeoutMs = 1000
	a.SetBaseContext(context.Background())
	a.startErrgroup()

	// Launch a cooperative goroutine that finishes when ctx cancels.
	a.group.Go(func() error {
		<-a.groupCtx.Done()
		return nil
	})

	timedOut := a.shutdownWithTimeout()
	if timedOut {
		t.Fatal("expected clean shutdown, got timeout")
	}
}

// TestEmitStatusLoop_ExitsOnCtxCancel: cancellable status loop cooperates with shutdown.
func TestEmitStatusLoop_ExitsOnCtxCancel(t *testing.T) {
	a := &App{cfg: config.DefaultConfig(), logger: slog.Default(), ctx: context.Background()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.emitStatusLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emitStatusLoop did not exit on ctx cancel")
	}
}

// TestShutdown_TimeoutReturnsTrue: a stuck goroutine triggers the timeout branch.
func TestShutdown_TimeoutReturnsTrue(t *testing.T) {
	a := &App{
		cfg:    config.DefaultConfig(),
		logger: slog.Default(),
		ctx:    context.Background(),
	}
	a.cfg.App.ShutdownTimeoutMs = 100
	a.SetBaseContext(context.Background())
	a.startErrgroup()

	// Unresponsive goroutine: ignores ctx cancellation for longer than the timeout.
	done := make(chan struct{})
	a.group.Go(func() error {
		<-done
		return nil
	})
	defer close(done)

	start := time.Now()
	timedOut := a.shutdownWithTimeout()
	elapsed := time.Since(start)
	if !timedOut {
		t.Fatal("expected timeout, got clean shutdown")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown exceeded reasonable bound: %v", elapsed)
	}
}
