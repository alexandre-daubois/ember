package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
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

	reloadTLS(f, fetcher.TLSOptions{}, log)

	assert.Contains(t, buf.String(), "TLS certificates reloaded")
}

func TestReloadTLS_InvalidCert(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	f := fetcher.NewHTTPFetcher("http://localhost:2019", 0)

	reloadTLS(f, fetcher.TLSOptions{CACert: "/nonexistent/ca.pem"}, log)

	assert.Contains(t, buf.String(), "TLS reload failed")
}

type mockFetcher struct{}

func (m *mockFetcher) Fetch(_ context.Context) (*fetcher.Snapshot, error) {
	return &fetcher.Snapshot{}, nil
}

func TestReloadTLS_NonHTTPFetcher(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	reloadTLS(&mockFetcher{}, fetcher.TLSOptions{}, log)

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

type daemonMetricsSubPlugin struct {
	testPlugin
	called bool
}

func (p *daemonMetricsSubPlugin) OnMetrics(_ *fetcher.Snapshot) { p.called = true }

type daemonPanicMetricsSubPlugin struct {
	testPlugin
}

func (p *daemonPanicMetricsSubPlugin) OnMetrics(_ *fetcher.Snapshot) { panic("onmetrics boom") }

func TestNotifyDaemonSubscribers_CallsOnMetrics(t *testing.T) {
	sub := &daemonMetricsSubPlugin{testPlugin: testPlugin{name: "sub"}}
	dps := []daemonPlugin{{p: sub, name: "sub"}}

	notifyDaemonSubscribers(dps, &fetcher.Snapshot{})
	assert.True(t, sub.called)
}

func TestPollAll_MultiInstance_OneDownStillExportsOther(t *testing.T) {
	var buf bytes.Buffer
	log := testLogger(&buf)

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{host=\"test.com\",code=\"200\"} 1\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer good.Close()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	holder := &exporter.StateHolder{}
	holder.SetMulti(true)

	instances := []*instance{
		{name: "alive", addr: good.URL, fetcher: fetcher.NewHTTPFetcher(good.URL, 0)},
		{name: "broken", addr: bad.URL, fetcher: fetcher.NewHTTPFetcher(bad.URL, 0)},
	}

	pollAll(context.Background(), instances, holder, nil, log)

	rec := httptest.NewRecorder()
	exporter.Handler(holder, "", nil)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// the "alive" instance was reachable so its label must appear; the "broken"
	// one populated nothing, so no panic and no broken metrics.
	assert.Contains(t, body, `ember_instance="alive"`)
}

func TestPollAll_MultiInstance_CounterResetIsolated(t *testing.T) {
	// Each instance owns its own model.State, so a counter reset on one must
	// not wipe the percentiles already computed on the other.
	now := time.Now()
	bucket := func(count float64) []fetcher.HistogramBucket {
		return []fetcher.HistogramBucket{
			{UpperBound: 0.01, CumulativeCount: count / 2},
			{UpperBound: 0.05, CumulativeCount: count * 9 / 10},
			{UpperBound: 0.1, CumulativeCount: count},
		}
	}
	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			DurationBuckets:          bucket(0),
			HTTPRequestDurationCount: 0,
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			DurationBuckets:          bucket(100),
			HTTPRequestDurationCount: 100,
		},
	}
	stable := &instance{name: "stable"}
	stable.state.Update(prev)
	stable.state.Update(curr)
	require.True(t, stable.state.Derived.HasPercentiles, "stable instance should have percentiles after two snapshots")

	// Drive a counter reset on a sibling instance: cumulative count drops.
	resetSnap := &fetcher.Snapshot{
		FetchedAt: now.Add(time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			DurationBuckets:          bucket(5),
			HTTPRequestDurationCount: 5,
		},
	}
	resetting := &instance{name: "resetting"}
	resetting.state.Update(prev)
	resetting.state.Update(curr)
	resetting.state.Update(resetSnap)
	assert.False(t, resetting.state.Derived.HasPercentiles, "reset instance should clear its own percentiles")

	// The stable instance's derived state must remain intact.
	assert.True(t, stable.state.Derived.HasPercentiles, "stable instance percentiles must survive sibling reset")
	assert.Greater(t, stable.state.Derived.P99, 0.0)
}

func TestNotifyDaemonSubscribers_PanicDoesNotCrash(t *testing.T) {
	panicSub := &daemonPanicMetricsSubPlugin{testPlugin: testPlugin{name: "panic-sub"}}
	normalSub := &daemonMetricsSubPlugin{testPlugin: testPlugin{name: "normal-sub"}}
	dps := []daemonPlugin{
		{p: panicSub, name: "panic-sub"},
		{p: normalSub, name: "normal-sub"},
	}

	assert.NotPanics(t, func() {
		notifyDaemonSubscribers(dps, &fetcher.Snapshot{})
	})
	assert.True(t, normalSub.called, "subscriber after panicking one should still be called")
}
