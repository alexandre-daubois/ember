package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestErrorThrottle_FirstErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)

	assert.Contains(t, buf.String(), "fetch failed")
	assert.True(t, et.failing)
}

func TestErrorThrottle_SubsequentErrorsSuppressed(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.record(log, assert.AnError)
	et.record(log, assert.AnError)

	assert.Empty(t, buf.String(), "repeated errors within interval should be suppressed")
	assert.Equal(t, 2, et.suppressed)
}

func TestErrorThrottle_LogsAfterInterval(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.suppressed = 5
	et.lastLogged = time.Now().Add(-errorThrottleInterval - time.Second)

	et.record(log, assert.AnError)

	require.Contains(t, buf.String(), "fetch failed")
	assert.Contains(t, buf.String(), "suppressed=5")
	assert.Equal(t, 0, et.suppressed)
}

func TestErrorThrottle_RecoverLogs(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.record(log, assert.AnError)
	buf.Reset()

	et.recover(log)

	assert.Contains(t, buf.String(), "fetch recovered")
	assert.False(t, et.failing)
}

func TestErrorThrottle_RecoverNoopWhenNotFailing(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	var et errorThrottle
	et.recover(log)

	assert.Empty(t, buf.String(), "recover should not log when not failing")
}

func TestReloadTLS_NoTLSConfig(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	f := fetcher.NewHTTPFetcher("http://localhost:2019", 0)
	cfg := &config{}

	reloadTLS(f, cfg, log)

	assert.Contains(t, buf.String(), "TLS certificates reloaded")
}

func TestReloadTLS_InvalidCert(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	f := fetcher.NewHTTPFetcher("http://localhost:2019", 0)
	cfg := &config{caCert: "/nonexistent/ca.pem"}

	reloadTLS(f, cfg, log)

	assert.Contains(t, buf.String(), "TLS reload failed")
}

type mockFetcher struct{}

func (m *mockFetcher) Fetch(_ context.Context) (*fetcher.Snapshot, error) {
	return &fetcher.Snapshot{}, nil
}

func TestReloadTLS_NonHTTPFetcher(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	reloadTLS(&mockFetcher{}, &config{}, log)

	assert.Contains(t, buf.String(), "not supported")
}

type daemonFetchPlugin struct {
	testPlugin
	fetchData any
	fetchErr  error
}

func (p *daemonFetchPlugin) Fetch(_ context.Context) (any, error) {
	return p.fetchData, p.fetchErr
}

type daemonExportPlugin struct {
	daemonFetchPlugin
}

func (p *daemonExportPlugin) WriteMetrics(w io.Writer, _ any, _ string) {
	_, _ = io.WriteString(w, "daemon_test_metric 1\n")
}

type daemonPanicFetchPlugin struct {
	testPlugin
}

func (p *daemonPanicFetchPlugin) Fetch(_ context.Context) (any, error) {
	panic("daemon fetch boom")
}

func TestNewDaemonPlugins_Empty(t *testing.T) {
	dps := newDaemonPlugins(nil)
	assert.Nil(t, dps)
}

func TestNewDaemonPlugins_SkipsBarePlugin(t *testing.T) {
	bare := &testPlugin{name: "bare"}
	dps := newDaemonPlugins([]plugin.Plugin{bare})
	assert.Empty(t, dps)
}

func TestNewDaemonPlugins_IncludesFetcher(t *testing.T) {
	p := &daemonFetchPlugin{testPlugin: testPlugin{name: "fetchy"}, fetchData: "data"}
	dps := newDaemonPlugins([]plugin.Plugin{p})
	require.Len(t, dps, 1)
	assert.Equal(t, "fetchy", dps[0].name)
	assert.NotNil(t, dps[0].fetcher)
	assert.Nil(t, dps[0].exporter)
}

func TestNewDaemonPlugins_IncludesExporter(t *testing.T) {
	p := &daemonExportPlugin{daemonFetchPlugin: daemonFetchPlugin{testPlugin: testPlugin{name: "exporty"}}}
	dps := newDaemonPlugins([]plugin.Plugin{p})
	require.Len(t, dps, 1)
	assert.NotNil(t, dps[0].fetcher)
	assert.NotNil(t, dps[0].exporter)
}

func TestSafeFetch_Normal(t *testing.T) {
	p := &daemonFetchPlugin{testPlugin: testPlugin{name: "ok"}, fetchData: "hello"}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.NoError(t, err)
	assert.Equal(t, "hello", data)
}

func TestSafeFetch_RecoversPanic(t *testing.T) {
	p := &daemonPanicFetchPlugin{testPlugin: testPlugin{name: "panic"}}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.Nil(t, data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during Fetch")
}

func TestFetchDaemonPlugins_UpdatesData(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	dps := []daemonPlugin{
		{name: "a", fetcher: &daemonFetchPlugin{fetchData: "result"}},
	}
	fetchDaemonPlugins(context.Background(), dps, log)
	assert.Equal(t, "result", dps[0].data)
	assert.Empty(t, buf.String())
}

func TestFetchDaemonPlugins_LogsOnPanic(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	dps := []daemonPlugin{
		{name: "panicky", fetcher: &daemonPanicFetchPlugin{testPlugin: testPlugin{name: "panicky"}}},
	}
	fetchDaemonPlugins(context.Background(), dps, log)
	assert.Nil(t, dps[0].data)
	assert.Contains(t, buf.String(), "plugin fetch failed")
	assert.Contains(t, buf.String(), "panicky")
}

func TestFetchDaemonPlugins_SkipsNilFetcher(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	dps := []daemonPlugin{
		{name: "no-fetch", fetcher: nil, data: "old"},
	}
	fetchDaemonPlugins(context.Background(), dps, log)
	assert.Equal(t, "old", dps[0].data)
}

func TestDaemonPluginExports_IncludesOnlyExporters(t *testing.T) {
	exp := &daemonExportPlugin{}
	dps := []daemonPlugin{
		{name: "no-export", fetcher: &daemonFetchPlugin{}, data: "x"},
		{name: "has-export", exporter: exp, data: "y"},
	}
	exports := daemonPluginExports(dps)
	require.Len(t, exports, 1)
	assert.Equal(t, "y", exports[0].Data)
}

func TestDaemonPluginExports_Empty(t *testing.T) {
	exports := daemonPluginExports(nil)
	assert.Nil(t, exports)
}
