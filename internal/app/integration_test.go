//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func caddyAddr() string {
	if addr := os.Getenv("EMBER_TEST_CADDY_ADDR"); addr != "" {
		return addr
	}
	return "http://localhost:2019"
}

func TestIntegration_Wait(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, f, addr, 500*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")
}

func TestIntegration_WaitQuiet(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := runWait(ctx, io.Discard, f, addr, 500*time.Millisecond)
	require.NoError(t, err)
}

func TestIntegration_Init(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runInit(ctx, &buf, strings.NewReader("y\n"), f, addr, true)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Admin API reachable")
	assert.Contains(t, out, "HTTP metrics enabled")
	assert.Contains(t, out, "Ember is ready")
}

func TestIntegration_InitEnablesMetrics(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	enabled, err := f.CheckMetricsEnabled(ctx)
	require.NoError(t, err)

	if !enabled {
		err = f.EnableMetrics(ctx)
		require.NoError(t, err)
	}

	enabled, err = f.CheckMetricsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled, "metrics should be enabled after init")
}

func TestIntegration_Status(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, addr, 500*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Caddy OK")
}

func TestIntegration_JSONOnce(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap)

	var state model.State
	state.Update(snap)
	out := buildJSONOutput(snap, &state)

	data, err := json.Marshal(out)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "JSON output must be valid")
	assert.Contains(t, parsed, "threads")
	assert.Contains(t, parsed, "metrics")
	assert.Contains(t, parsed, "process")
	assert.Contains(t, parsed, "fetchedAt")

	threads, ok := parsed["threads"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, threads, "threadDebugStates", "thread fields should be camelCase")
	assert.Contains(t, threads, "reservedThreadCount", "thread fields should be camelCase")
}

func TestIntegration_Diff(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap1, err := f.Fetch(ctx)
	require.NoError(t, err)
	var state1 model.State
	state1.Update(snap1)
	out1 := buildJSONOutput(snap1, &state1)

	snap2, err := f.Fetch(ctx)
	require.NoError(t, err)
	var state2 model.State
	state2.Update(snap2)
	out2 := buildJSONOutput(snap2, &state2)

	before, err := os.CreateTemp(t.TempDir(), "before-*.json")
	require.NoError(t, err)
	after, err := os.CreateTemp(t.TempDir(), "after-*.json")
	require.NoError(t, err)

	require.NoError(t, json.NewEncoder(before).Encode(out1))
	require.NoError(t, json.NewEncoder(after).Encode(out2))
	before.Close()
	after.Close()

	var buf bytes.Buffer
	err = runDiff(&buf, before.Name(), after.Name())

	require.NoError(t, err, "identical-state diff should not report regressions")
	assert.Contains(t, buf.String(), "No regressions detected")
}

func TestIntegration_FetchServerNames(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	names := f.FetchServerNames(ctx)
	assert.NotEmpty(t, names, "Caddy should have at least one server configured")
}

func TestIntegration_AdminAPI(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := f.CheckAdminAPI(ctx)
	require.NoError(t, err, "admin API should be reachable")
}
