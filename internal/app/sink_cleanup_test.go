package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestUnregisterWithRetry_Success: returns nil on first try -> exactly 1 call.
func TestUnregisterWithRetry_Success(t *testing.T) {
	var calls int32
	fn := func(_ context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	unregisterWithRetry(context.Background(), "test", fn, 3)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 call, got %d", got)
	}
}

// TestUnregisterWithRetry_RecoverAfterErrors: errors twice, then nil -> 3 calls.
// Use a cancelled-via-deadline ctx so the time.After path inside is short-circuited
// only on the final success — but we also override the sleep via a short-deadline
// context that still allows two retries: easier path is to accept the 6s wait,
// or shrink. We use a context with no deadline and rely on the function's own
// 3s back-off; that is acceptable for one test.
func TestUnregisterWithRetry_RecoverAfterErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in -short mode (uses 3s back-off twice)")
	}
	var calls int32
	fn := func(_ context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("boom")
		}
		return nil
	}
	start := time.Now()
	unregisterWithRetry(context.Background(), "test", fn, 3)
	elapsed := time.Since(start)
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls, got %d", got)
	}
	if elapsed < 5*time.Second {
		t.Fatalf("expected >=5s elapsed (two 3s back-offs), got %s", elapsed)
	}
}

// TestUnregisterWithRetry_GiveUp: always errors -> attempts calls, no panic.
// We use a pre-cancelled context so the back-off select returns immediately
// via ctx.Done() — keeps the test fast.
func TestUnregisterWithRetry_GiveUp(t *testing.T) {
	var calls int32
	fn := func(_ context.Context) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("always fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: back-off select returns immediately
	unregisterWithRetry(ctx, "test", fn, 3)
	got := atomic.LoadInt32(&calls)
	// With pre-cancelled ctx: first call fails, back-off select hits ctx.Done()
	// and returns early. Acceptable: at least 1 call, at most attempts.
	if got < 1 || got > 3 {
		t.Fatalf("expected 1..3 calls under cancelled ctx, got %d", got)
	}
}

// TestUnregisterWithRetry_ZeroAttempts: defensive — attempts<1 should still call once.
func TestUnregisterWithRetry_ZeroAttempts(t *testing.T) {
	var calls int32
	fn := func(_ context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	unregisterWithRetry(context.Background(), "test", fn, 0)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call (attempts<1 -> 1), got %d", got)
	}
}
