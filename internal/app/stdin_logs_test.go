package app

import (
	"io"
	"os"
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

	// Start stdin listener
	cleanup, ok := startStdinListener(nil, uiCfg)
	require.True(t, ok)
	defer cleanup()

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

	// Give the reader goroutine a brief moment to process the logs
	assert.Eventually(t, func() bool {
		return uiCfg.LogBuffer != nil && uiCfg.RuntimeLogBuffer != nil &&
			uiCfg.LogBuffer.Len() == 1 && uiCfg.RuntimeLogBuffer.Len() == 1
	}, 1*time.Second, 10*time.Millisecond)

	// Verify access log content
	accessLogs := uiCfg.LogBuffer.Snapshot(model.LogFilter{}, 0)
	require.Len(t, accessLogs, 1)
	assert.Equal(t, "localhost", accessLogs[0].Host)
	assert.Equal(t, "GET", accessLogs[0].Method)
	assert.Equal(t, 200, accessLogs[0].Status)

	// Verify runtime log content
	runtimeLogs := uiCfg.RuntimeLogBuffer.Snapshot(model.LogFilter{}, 0)
	require.Len(t, runtimeLogs, 1)
	assert.Equal(t, "certificate warning", runtimeLogs[0].Message)
	assert.Equal(t, "warn", runtimeLogs[0].Level)
}
