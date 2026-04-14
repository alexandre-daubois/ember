package app

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr runs fn and returns whatever it wrote to os.Stderr.
// Serialized globally because os.Stderr is a process-wide resource.
var stderrMu sync.Mutex

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	stderrMu.Lock()
	defer stderrMu.Unlock()

	orig := os.Stderr
	defer func() { os.Stderr = orig }()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	fn()
	_ = w.Close()
	return string(<-done)
}

func TestWarnIfPublicListener_QuietForLoopback(t *testing.T) {
	cases := []string{"127.0.0.1:9210", "[::1]:9210"}
	for _, addr := range cases {
		t.Run(addr, func(t *testing.T) {
			got := captureStderr(t, func() { warnIfPublicListener(addr) })
			assert.Empty(t, got, "loopback addr %s must not trigger a warning", addr)
		})
	}
}

func TestWarnIfPublicListener_WarnsOnPublic(t *testing.T) {
	got := captureStderr(t, func() { warnIfPublicListener("0.0.0.0:9210") })
	assert.Contains(t, got, "warning")
	assert.Contains(t, got, "0.0.0.0:9210")
}

func TestWarnIfPublicListener_IgnoresMalformed(t *testing.T) {
	// Malformed or hostname-based addresses cannot be classified; we stay
	// silent rather than misreporting.
	for _, addr := range []string{"", "not-a-host:port", "example.com:9210"} {
		got := captureStderr(t, func() { warnIfPublicListener(addr) })
		assert.Empty(t, got, "addr %q must not warn", addr)
	}
}

func TestIsLocalAdminAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"http://localhost:2019", true},
		{"http://127.0.0.1:2019", true},
		{"https://localhost:2019", true},
		{"http://[::1]:2019", true},
		{"http://[::1]/some/path", true},
		{"http://localhost", true},
		{"http://localhost/", true},
		{"unix//tmp/caddy.sock", true},
		{"unix:///tmp/caddy.sock", true},
		{"http://prod.example.com:2019", false},
		{"https://10.0.0.5:2019", false},
		{"http://caddy:2019", false},
		{"http://[2001:db8::1]:8080", false},
		{"http://[2001:db8::1]", false},
		// Schemeless garbage starting with "/" must not fall into the empty
		// host branch via the old "cut at first slash" shortcut. Found by
		// FuzzIsLocalAdminAddr; regression guard.
		{"/10.0.0.5", false},
		{"/some/path", false},
		{"10.0.0.5", false},
		// A scheme with no authority is malformed — classifying it "local"
		// would silently auto-bind a listener for a URL Caddy cannot reach.
		// Also found by FuzzIsLocalAdminAddr.
		{"http:///CAddY:2019", false},
		{"http:///", false},
		{"http://", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isLocalAdminAddr(c.addr), "addr=%s", c.addr)
	}
}
