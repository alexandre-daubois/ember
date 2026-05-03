//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCaddy serves enough of the admin API to make HTTPFetcher.Fetch succeed.
// The exposed counters advance on every /metrics scrape so derived percentiles
// (which need a non-zero delta between Previous and Current snapshots) actually
// populate. step controls the per-scrape increment so each instance grows at a
// distinguishable rate.
func fakeCaddy(t *testing.T, host string, step int) *httptest.Server {
	t.Helper()
	var counter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/metrics":
			n := counter.Add(int64(step))
			fmt.Fprintf(w, `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="srv0",handler="reverse_proxy",host=%q,code="200"} %d
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="srv0",host=%q,le="0.005"} %d
caddy_http_request_duration_seconds_bucket{server="srv0",host=%q,le="0.01"} %d
caddy_http_request_duration_seconds_bucket{server="srv0",host=%q,le="+Inf"} %d
caddy_http_request_duration_seconds_sum{server="srv0",host=%q} %f
caddy_http_request_duration_seconds_count{server="srv0",host=%q} %d
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes %d
`,
				host, n,
				host, n/2,
				host, n*9/10,
				host, n,
				host, float64(n)*0.01,
				host, n,
				n*1024,
			)
		case strings.HasPrefix(r.URL.Path, "/config/"):
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// scrapeUntil polls the /metrics endpoint until predicate returns true or the
// deadline elapses. Replaces brittle fixed sleeps that depend on tick scheduling.
func scrapeUntil(t *testing.T, url string, deadline time.Duration, predicate func(string) bool) string {
	t.Helper()
	end := time.Now().Add(deadline)
	var last string
	for time.Now().Before(end) {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			last = string(body)
			if resp.StatusCode == http.StatusOK && predicate(last) {
				return last
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("predicate not satisfied within %s; last body:\n%s", deadline, last)
	return last
}

func TestIntegration_Multi_DaemonExposesPerInstanceMetrics(t *testing.T) {
	web1 := fakeCaddy(t, "web1.example", 100)
	web2 := fakeCaddy(t, "web2.example", 250)

	cfg := &config{
		addrs: []addrSpec{
			{name: "web1", url: web1.URL},
			{name: "web2", url: web2.URL},
		},
		interval: 200 * time.Millisecond,
		expose:   freePort(t),
		daemon:   true,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	instances, err := newInstances(context.Background(), cfg, "v-test")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, instances, cfg, nil) }()

	url := "http://" + cfg.expose
	waitForServer(t, url+"/healthz", 3*time.Second)

	// Wait until both instances have produced percentile metrics, which
	// requires at least two completed scrapes per instance. Polling avoids
	// the flakiness of a fixed sleep on slow CI runners.
	body := scrapeUntil(t, url+"/metrics", 5*time.Second, func(b string) bool {
		return strings.Contains(b, `frankenphp_request_duration_milliseconds{quantile="0.99",ember_instance="web1"}`) &&
			strings.Contains(b, `frankenphp_request_duration_milliseconds{quantile="0.99",ember_instance="web2"}`)
	})

	// Per-instance host label attribution: web1.example must appear paired
	// with ember_instance="web1", web2.example with ember_instance="web2",
	// never crossed.
	web1RPS := regexp.MustCompile(`ember_host_rps\{host="web1\.example",ember_instance="web1"\} \S+`)
	web2RPS := regexp.MustCompile(`ember_host_rps\{host="web2\.example",ember_instance="web2"\} \S+`)
	assert.Regexp(t, web1RPS, body, "web1 host metric must carry web1 instance label")
	assert.Regexp(t, web2RPS, body, "web2 host metric must carry web2 instance label")
	assert.NotRegexp(t, regexp.MustCompile(`host="web1\.example",ember_instance="web2"`), body, "instances must not cross-pollinate")
	assert.NotRegexp(t, regexp.MustCompile(`host="web2\.example",ember_instance="web1"`), body, "instances must not cross-pollinate")

	// build_info: unlabelled, exactly once.
	assert.Equal(t, 1, strings.Count(body, "ember_build_info{"), "build_info should appear exactly once")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "ember_build_info{") {
			assert.NotContains(t, line, "ember_instance", "ember_build_info must not carry an instance label")
		}
	}

	// Per-stage scrape counters must be present per instance.
	assert.Contains(t, body, `ember_scrape_total{stage="metrics",ember_instance="web1"}`)
	assert.Contains(t, body, `ember_scrape_total{stage="metrics",ember_instance="web2"}`)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "daemon must shut down cleanly on context cancel")
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}
}

func TestIntegration_Multi_HealthzAggregatesPerInstance(t *testing.T) {
	web1 := fakeCaddy(t, "web1.example", 10)
	web2 := fakeCaddy(t, "web2.example", 20)

	holder := &exporter.StateHolder{}
	holder.SetMulti(true)

	pairs := []struct {
		name string
		srv  *httptest.Server
	}{
		{"web1", web1},
		{"web2", web2},
	}
	for _, p := range pairs {
		f := fetcher.NewHTTPFetcher(p.srv.URL, 0)
		snap, err := f.Fetch(context.Background())
		require.NoError(t, err)
		var state model.State
		state.Update(snap)
		holder.StoreInstance(p.name, p.srv.URL, state.CopyForExport(), nil, nil)
	}

	rec := httptest.NewRecorder()
	exporter.HealthHandler(holder, time.Second, nil)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Status    string `json:"status"`
		Instances []struct {
			Name   string `json:"name"`
			Addr   string `json:"addr"`
			Status string `json:"status"`
		} `json:"instances"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body.Status)
	require.Len(t, body.Instances, 2)
	// Ordering must be alphabetical by name regardless of insertion order.
	assert.Equal(t, "web1", body.Instances[0].Name)
	assert.Equal(t, "web2", body.Instances[1].Name)
	assert.Equal(t, web1.URL, body.Instances[0].Addr)
	assert.Equal(t, web2.URL, body.Instances[1].Addr)
	assert.Equal(t, "ok", body.Instances[0].Status)
	assert.Equal(t, "ok", body.Instances[1].Status)
}

func TestIntegration_Multi_JSONOutputsLinePerInstance(t *testing.T) {
	web1 := fakeCaddy(t, "web1.example", 10)
	web2 := fakeCaddy(t, "web2.example", 20)

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = origStdout
		_ = r.Close()
	})

	cfg := &config{
		interval: time.Second,
		once:     true,
		jsonMode: true,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	instances := []*instance{
		{name: "web2", addr: web2.URL, fetcher: fetcher.NewHTTPFetcher(web2.URL, 0)},
		{name: "web1", addr: web1.URL, fetcher: fetcher.NewHTTPFetcher(web1.URL, 0)},
	}
	require.NoError(t, runJSON(context.Background(), instances, cfg))
	w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 2, "--once with N=2 must produce exactly two JSONL lines")

	// Even with web2 listed first in instances slice, output must be sorted
	// alphabetically by name for stable downstream consumption.
	var first, second jsonOutput
	require.NoError(t, json.Unmarshal(lines[0], &first))
	require.NoError(t, json.Unmarshal(lines[1], &second))
	assert.Equal(t, "web1", first.Instance, "lines must be sorted by instance name")
	assert.Equal(t, "web2", second.Instance)

	// Verify content is properly attributed (not crossed). web1 has 10 reqs,
	// web2 has 20 reqs in their respective host metrics.
	require.Len(t, first.Hosts, 1)
	require.Len(t, second.Hosts, 1)
	assert.Equal(t, "web1.example", first.Hosts[0].Host)
	assert.Equal(t, "web2.example", second.Hosts[0].Host)
}

// countingCaddy is a minimal fake that returns a static /metrics body and
// exposes a per-server hit counter so tests can verify polling cadence.
func countingCaddy(t *testing.T, host string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			hits.Add(1)
			_, _ = fmt.Fprintf(w, "# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{server=\"srv0\",host=%q,code=\"200\"} 1\n", host)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestIntegration_Multi_DaemonPerInstanceInterval verifies issue #46: when
// --addr carries ,interval=, that instance polls at its own cadence rather
// than at the global --interval.
func TestIntegration_Multi_DaemonPerInstanceInterval(t *testing.T) {
	fast, fastHits := countingCaddy(t, "fast.example")
	slow, slowHits := countingCaddy(t, "slow.example")

	cfg := &config{
		addrs: []addrSpec{
			{name: "fast", url: fast.URL, interval: 100 * time.Millisecond},
			{name: "slow", url: slow.URL, interval: 500 * time.Millisecond},
		},
		interval: 100 * time.Millisecond,
		expose:   freePort(t),
		daemon:   true,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	instances, err := newInstances(context.Background(), cfg, "v-test")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, instances, cfg, nil) }()
	waitForServer(t, "http://"+cfg.expose+"/healthz", 3*time.Second)

	time.Sleep(1100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}

	fastN := fastHits.Load()
	slowN := slowHits.Load()
	// Initial pollAll fetches every instance once, then per-instance tickers
	// take over. After ~1.1s: fast (100ms) should have polled ~11 times, slow
	// (500ms) ~3 times. Allow generous slack for CI scheduler jitter.
	assert.GreaterOrEqual(t, fastN, int64(6), "fast instance must poll at its own (faster) cadence")
	assert.LessOrEqual(t, slowN, int64(5), "slow instance must not be dragged onto the global cadence")
	assert.Greater(t, fastN, slowN, "fast must out-poll slow over the same window")
}

// TestIntegration_Multi_JSONPerInstanceInterval verifies the JSONL streamer
// emits per-instance lines at each instance's own cadence.
func TestIntegration_Multi_JSONPerInstanceInterval(t *testing.T) {
	fast, _ := countingCaddy(t, "fast.example")
	slow, _ := countingCaddy(t, "slow.example")

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	cfg := &config{
		interval: 100 * time.Millisecond,
		jsonMode: true,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	instances := []*instance{
		{name: "fast", addr: fast.URL, interval: 100 * time.Millisecond, fetcher: fetcher.NewHTTPFetcher(fast.URL, 0)},
		{name: "slow", addr: slow.URL, interval: 500 * time.Millisecond, fetcher: fetcher.NewHTTPFetcher(slow.URL, 0)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	t.Cleanup(cancel)
	require.NoError(t, runJSON(ctx, instances, cfg))
	w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	var fastLines, slowLines int
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var out jsonOutput
		require.NoError(t, json.Unmarshal(line, &out))
		switch out.Instance {
		case "fast":
			fastLines++
		case "slow":
			slowLines++
		}
	}
	assert.Greater(t, fastLines, slowLines, "fast must emit more JSONL lines than slow over the same window")
	assert.GreaterOrEqual(t, fastLines, 6)
}
