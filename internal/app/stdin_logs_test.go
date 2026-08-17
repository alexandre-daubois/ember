package app

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsJSONLogLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "valid caddy access log",
			line:     `{"level":"info","ts":1710000000.123,"logger":"http.log.access","msg":"handled request"}`,
			expected: true,
		},
		{
			name:     "valid caddy runtime log",
			line:     `{"level":"warn","ts":1710000000.123,"logger":"tls","msg":"certificate expiring soon"}`,
			expected: true,
		},
		{
			name:     "non-json line",
			line:     `hello world`,
			expected: false,
		},
		{
			name:     "json but not log structure",
			line:     `{"hello":"world"}`,
			expected: false,
		},
		{
			name:     "empty object",
			line:     `{}`,
			expected: false,
		},
		{
			name:     "valid general log",
			line:     `{"msg":"started service"}`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isJSONLogLine(tt.line))
		})
	}
}

func TestStartStdinListener(t *testing.T) {
	// Keep a backup of os.Stdin and restore it afterwards
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	uiCfg := &ui.Config{}

	startStdinListener(uiCfg)

	// Write some valid logs and some non-log lines to the pipe
	logs := []string{
		// Valid access log
		`{"level":"info","ts":1710000000.123,"logger":"http.log.access.log0","msg":"handled request","request":{"host":"localhost","method":"GET","uri":"/","remote_ip":"127.0.0.1"},"status":200}`,
		// Non-JSON noise (should be ignored)
		`Some Kubernetes controller stdout line`,
		// Prefixed valid runtime log
		`2026-07-23T14:31:00Z stdout F {"level":"warn","ts":1710000000.123,"logger":"tls","msg":"certificate warning"}`,
	}

	for _, log := range logs {
		_, err := io.WriteString(w, log+"\n")
		require.NoError(t, err)
	}

	// Close write end of pipe so scanner finishes
	_ = w.Close()

	// The runtime buffer takes a second entry once the stream ends: the
	// listener records the end so a dead pipe is visible in the UI instead of
	// looking like a quiet server.
	assert.Eventually(t, func() bool {
		return uiCfg.LogBuffer != nil && uiCfg.RuntimeLogBuffer != nil &&
			uiCfg.LogBuffer.Len() == 1 && uiCfg.RuntimeLogBuffer.Len() == 2
	}, 1*time.Second, 10*time.Millisecond)

	// Verify access log content
	accessLogs := uiCfg.LogBuffer.Snapshot(model.LogFilter{}, 0)
	require.Len(t, accessLogs, 1)
	assert.Equal(t, "localhost", accessLogs[0].Host)
	assert.Equal(t, "GET", accessLogs[0].Method)
	assert.Equal(t, 200, accessLogs[0].Status)

	// Verify runtime log content (Snapshot returns the newest entry first).
	runtimeLogs := uiCfg.RuntimeLogBuffer.Snapshot(model.LogFilter{}, 0)
	require.Len(t, runtimeLogs, 2)
	assert.Equal(t, "stdin log stream ended", runtimeLogs[0].Message)
	assert.Equal(t, "error", runtimeLogs[0].Level)
	assert.Equal(t, "certificate warning", runtimeLogs[1].Message)
	assert.Equal(t, "warn", runtimeLogs[1].Level)
}

func TestStartStdinListener_OverlongLineIsReportedNotSilentlyDropped(t *testing.T) {
	// bufio.Scanner stops for good on the first line past its ceiling, so the
	// stream is dead from that point: the listener must say so rather than
	// leaving the TUI showing stale entries as if logs were still flowing.
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	uiCfg := &ui.Config{}
	startStdinListener(uiCfg)

	go func() {
		_, _ = io.WriteString(w, `{"level":"warn","ts":1710000000.123,"logger":"tls","msg":"before"}`+"\n")
		_, _ = io.WriteString(w, `{"msg":"`+strings.Repeat("x", stdinMaxLineBytes)+`"}`+"\n")
		_, _ = io.WriteString(w, `{"level":"warn","ts":1710000000.123,"logger":"tls","msg":"after"}`+"\n")
		_ = w.Close()
	}()

	assert.Eventually(t, func() bool {
		return uiCfg.RuntimeLogBuffer != nil && uiCfg.RuntimeLogBuffer.Len() == 2
	}, 2*time.Second, 10*time.Millisecond)

	runtimeLogs := uiCfg.RuntimeLogBuffer.Snapshot(model.LogFilter{}, 0)
	require.Len(t, runtimeLogs, 2)
	assert.Contains(t, runtimeLogs[0].Message, "stdin log stream stopped")
	assert.Equal(t, "error", runtimeLogs[0].Level)
	assert.Equal(t, "before", runtimeLogs[1].Message)
}
