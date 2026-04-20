package fetcher

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeJSONLine(method, host, uri string, status int) string {
	return fmt.Sprintf(`{"level":"info","ts":1742472000.0,"msg":"handled request","request":{"method":%q,"host":%q,"uri":%q},"status":%d}`, method, host, uri, status)
}

type netCollector struct {
	mu      sync.Mutex
	entries []LogEntry
}

func (c *netCollector) onBatch(b []LogEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, b...)
	c.mu.Unlock()
}

func (c *netCollector) snapshot() []LogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]LogEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

func (c *netCollector) waitFor(t *testing.T, want int, timeout time.Duration) []LogEntry {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := c.snapshot()
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d entries, have %d", want, len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLogNetListener_ReceivesAndParses(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		listener.Start(ctx, col.onBatch)
		close(done)
	}()

	conn, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte(makeJSONLine("GET", "a.com", "/", 200) + "\n"))
	require.NoError(t, err)
	_, err = conn.Write([]byte(makeJSONLine("POST", "b.com", "/x", 500) + "\n"))
	require.NoError(t, err)

	entries := col.waitFor(t, 2, 2*time.Second)
	assert.Equal(t, "a.com", entries[0].Host)
	assert.Equal(t, 200, entries[0].Status)
	assert.Equal(t, "b.com", entries[1].Host)
	assert.Equal(t, 500, entries[1].Status)

	cancel()
	<-done
}

func TestLogNetListener_ParsesMalformedLines(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		listener.Start(ctx, col.onBatch)
		close(done)
	}()

	conn, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)

	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)
	_, err = conn.Write([]byte(makeJSONLine("GET", "ok.com", "/", 200) + "\n"))
	require.NoError(t, err)

	entries := col.waitFor(t, 2, 2*time.Second)
	assert.True(t, entries[0].ParseError)
	assert.Equal(t, "not json", entries[0].RawLine)
	assert.False(t, entries[1].ParseError)
	assert.Equal(t, "ok.com", entries[1].Host)

	_ = conn.Close()
	cancel()
	<-done
}

func TestLogNetListener_HandlesMultipleConnections(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		listener.Start(ctx, col.onBatch)
		close(done)
	}()

	for range 3 {
		conn, err := net.Dial("tcp", listener.Addr())
		require.NoError(t, err)
		_, err = conn.Write([]byte(makeJSONLine("GET", "h.com", "/", 200) + "\n"))
		require.NoError(t, err)
		_ = conn.Close()
	}

	col.waitFor(t, 3, 2*time.Second)

	cancel()
	<-done
}

func TestLogNetListener_ReconnectAfterDrop(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		listener.Start(ctx, col.onBatch)
		close(done)
	}()

	first, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)
	_, _ = first.Write([]byte(makeJSONLine("GET", "first.com", "/", 200) + "\n"))
	col.waitFor(t, 1, 2*time.Second)
	_ = first.Close()

	time.Sleep(60 * time.Millisecond)

	second, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)
	_, _ = second.Write([]byte(makeJSONLine("GET", "second.com", "/", 200) + "\n"))
	entries := col.waitFor(t, 2, 2*time.Second)
	assert.Equal(t, "first.com", entries[0].Host)
	assert.Equal(t, "second.com", entries[1].Host)
	_ = second.Close()

	cancel()
	<-done
}

func TestLogNetListener_StopsOnContextCancel(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		listener.Start(ctx, func(_ []LogEntry) {})
		close(done)
	}()

	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop within 2s")
	}
}

func TestLogNetListener_StopsOnCloseWithoutCtxCancel(t *testing.T) {
	// Callers are expected to cancel the ctx, but Close() alone must be
	// enough to tear down all internal goroutines. This asserts the Start
	// watchdog is unblocked via the internal done channel, not only via
	// ctx.Done(), preventing a goroutine leak.
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		// Use a never-cancelled context on purpose.
		listener.Start(context.Background(), func(_ []LogEntry) {})
		close(done)
	}()

	time.Sleep(40 * time.Millisecond)
	listener.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Close() without ctx cancel")
	}
}

func TestLogNetListener_BindFailure(t *testing.T) {
	first, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)
	defer first.Close()

	// Reuse the same address: must fail.
	_, err = NewLogNetListener(first.Addr())
	require.Error(t, err)
}

func TestLogNetListener_LastError_NilUntilSet(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	assert.NoError(t, listener.LastError(),
		"a fresh listener has not seen any error yet")
}

func TestLogNetListener_LastError_SurfacesRecentSetErr(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	listener.setErr(fmt.Errorf("synthetic boom"))
	assert.EqualError(t, listener.LastError(), "synthetic boom",
		"the listener must surface the most recent non-fatal error verbatim")
}

func TestLogNetListener_OversizedLineDropped(t *testing.T) {
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		listener.Start(ctx, col.onBatch)
		close(done)
	}()

	conn, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send a line exceeding maxLogLineBytes (1 MiB) without a newline,
	// followed by a normal line. The oversized data triggers a scanner
	// error, closing the connection. The listener must survive and accept
	// a fresh connection carrying the normal line.
	huge := make([]byte, maxLogLineBytes+1024)
	for i := range huge {
		huge[i] = 'x'
	}
	_, _ = conn.Write(huge)
	_ = conn.Close()

	// Reconnect and send a well-formed line.
	conn2, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)
	defer func() { _ = conn2.Close() }()

	_, err = conn2.Write([]byte(makeJSONLine("GET", "ok.com", "/", 200) + "\n"))
	require.NoError(t, err)

	entries := col.waitFor(t, 1, 2*time.Second)
	assert.Equal(t, "ok.com", entries[0].Host)

	cancel()
	<-done
}

func TestLogNetListener_CloseUnblocksFullChannel(t *testing.T) {
	// Verify that Close() without ctx cancel does not leave the reader
	// goroutine stranded when lineCh is full: the l.done select case
	// must unblock it.
	listener, err := NewLogNetListener("127.0.0.1:0")
	require.NoError(t, err)

	col := &netCollector{}
	done := make(chan struct{})
	go func() {
		listener.Start(context.Background(), col.onBatch)
		close(done)
	}()

	conn, err := net.Dial("tcp", listener.Addr())
	require.NoError(t, err)

	// Flood the connection to fill the internal lineCh (capacity 64).
	for range 200 {
		_, _ = conn.Write([]byte(makeJSONLine("GET", "h.com", "/", 200) + "\n"))
	}

	// Give some time for the reader goroutine to fill up the channel.
	time.Sleep(50 * time.Millisecond)

	// Close without cancelling ctx: Start must still return promptly.
	listener.Close()
	_ = conn.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Close() with full lineCh")
	}
}
