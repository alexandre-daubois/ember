package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJSONOutput_Basic(t *testing.T) {
	snap := &fetcher.Snapshot{
		Process:   fetcher.ProcessMetrics{CPUPercent: 42.5, RSS: 128 * 1024 * 1024},
		FetchedAt: time.Now(),
	}
	var state model.State
	state.Update(snap)

	out := buildJSONOutput(snap, &state)

	assert.Equal(t, snap.Process, out.Process)
	assert.Equal(t, snap.FetchedAt, out.FetchedAt)
	require.NotNil(t, out.Derived)
	assert.Nil(t, out.Derived.P50)
	assert.Empty(t, out.Hosts)
}

func TestBuildJSONOutput_WithHosts(t *testing.T) {
	snap := &fetcher.Snapshot{
		FetchedAt: time.Now(),
	}
	var state model.State
	state.Update(snap)
	state.HostDerived = []model.HostDerived{
		{
			Host:        "api.example.com",
			RPS:         100,
			AvgTime:     25,
			InFlight:    3,
			StatusCodes: map[int]float64{200: 90, 404: 5},
			MethodRates: map[string]float64{"GET": 80, "POST": 20},
		},
		{
			Host:     "web.example.com",
			RPS:      50,
			InFlight: 1,
		},
	}

	out := buildJSONOutput(snap, &state)

	require.Len(t, out.Hosts, 2)

	assert.Equal(t, "api.example.com", out.Hosts[0].Host)
	assert.Equal(t, 100.0, out.Hosts[0].RPS)
	assert.Equal(t, 25.0, out.Hosts[0].AvgTime)
	assert.Equal(t, 3.0, out.Hosts[0].InFlight)
	assert.Equal(t, map[int]float64{200: 90, 404: 5}, out.Hosts[0].StatusCodes)
	assert.Equal(t, map[string]float64{"GET": 80, "POST": 20}, out.Hosts[0].MethodRates)
	assert.Nil(t, out.Hosts[0].P50)

	assert.Equal(t, "web.example.com", out.Hosts[1].Host)
	assert.Equal(t, 50.0, out.Hosts[1].RPS)
	assert.Nil(t, out.Hosts[1].StatusCodes)
}

func TestBuildJSONOutput_HostPercentiles(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)
	state.HostDerived = []model.HostDerived{
		{
			Host:           "api.example.com",
			HasPercentiles: true,
			P50:            10, P90: 30, P95: 50, P99: 120,
		},
		{
			Host: "web.example.com",
		},
	}

	out := buildJSONOutput(snap, &state)

	require.Len(t, out.Hosts, 2)
	require.NotNil(t, out.Hosts[0].P50)
	assert.Equal(t, 10.0, *out.Hosts[0].P50)
	assert.Equal(t, 30.0, *out.Hosts[0].P90)
	assert.Equal(t, 50.0, *out.Hosts[0].P95)
	assert.Equal(t, 120.0, *out.Hosts[0].P99)

	assert.Nil(t, out.Hosts[1].P50)
	assert.Nil(t, out.Hosts[1].P90)
}

func TestBuildJSONOutput_NoHosts_NoPercentiles(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)

	out := buildJSONOutput(snap, &state)

	assert.Empty(t, out.Hosts)
	assert.Nil(t, out.Derived.P50)
	assert.Nil(t, out.Derived.P95)
	assert.Nil(t, out.Derived.P99)
	assert.Empty(t, out.Errors)
}

func TestBuildJSONOutput_WithErrors(t *testing.T) {
	snap := &fetcher.Snapshot{
		FetchedAt: time.Now(),
		Errors:    []string{"fetch threads: connection refused"},
	}
	var state model.State
	state.Update(snap)

	out := buildJSONOutput(snap, &state)

	require.Len(t, out.Errors, 1)
	assert.Equal(t, "fetch threads: connection refused", out.Errors[0])
}

func TestBuildJSONOutput_DerivedErrorRate(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)
	state.Derived.ErrorRate = 4.2

	out := buildJSONOutput(snap, &state)

	assert.Equal(t, 4.2, out.Derived.ErrorRate)
}

func TestBuildJSONOutput_DerivedErrorRate_Zero(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)

	out := buildJSONOutput(snap, &state)

	assert.Zero(t, out.Derived.ErrorRate)
}

func TestBuildJSONOutput_HostErrorRateAndAvgRequestSize(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)
	state.HostDerived = []model.HostDerived{
		{
			Host:           "api.example.com",
			ErrorRate:      3.5,
			AvgRequestSize: 2048,
		},
		{
			Host: "web.example.com",
		},
	}

	out := buildJSONOutput(snap, &state)

	require.Len(t, out.Hosts, 2)
	assert.Equal(t, 3.5, out.Hosts[0].ErrorRate)
	assert.Equal(t, 2048.0, out.Hosts[0].AvgRequestSize)
	assert.Zero(t, out.Hosts[1].ErrorRate)
	assert.Zero(t, out.Hosts[1].AvgRequestSize)
}

func TestBuildJSONOutput_HostTTFB(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)
	state.HostDerived = []model.HostDerived{
		{
			Host:    "api.example.com",
			HasTTFB: true,
			TTFBP50: 5.0, TTFBP90: 15.0, TTFBP95: 25.0, TTFBP99: 50.0,
		},
		{
			Host:    "web.example.com",
			HasTTFB: false,
		},
	}

	out := buildJSONOutput(snap, &state)

	require.Len(t, out.Hosts, 2)

	require.NotNil(t, out.Hosts[0].TTFBP50)
	assert.Equal(t, 5.0, *out.Hosts[0].TTFBP50)
	assert.Equal(t, 15.0, *out.Hosts[0].TTFBP90)
	assert.Equal(t, 25.0, *out.Hosts[0].TTFBP95)
	assert.Equal(t, 50.0, *out.Hosts[0].TTFBP99)

	assert.Nil(t, out.Hosts[1].TTFBP50)
	assert.Nil(t, out.Hosts[1].TTFBP90)
	assert.Nil(t, out.Hosts[1].TTFBP95)
	assert.Nil(t, out.Hosts[1].TTFBP99)
}

func TestBuildJSONOutput_DerivedPercentiles(t *testing.T) {
	snap := &fetcher.Snapshot{FetchedAt: time.Now()}
	var state model.State
	state.Update(snap)
	state.Derived.HasPercentiles = true
	state.Derived.P50 = 12.5
	state.Derived.P95 = 45.0
	state.Derived.P99 = 120.0

	out := buildJSONOutput(snap, &state)

	require.NotNil(t, out.Derived.P50)
	assert.Equal(t, 12.5, *out.Derived.P50)
	assert.Equal(t, 45.0, *out.Derived.P95)
	assert.Equal(t, 120.0, *out.Derived.P99)
}

func TestRunJSON_Once(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			w.Write([]byte(`# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="test.com",code="200"} 100
`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	// capture stdout
	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w

	ctx := context.Background()
	runJSON(ctx, f, time.Second, true)

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 1, "once mode should produce exactly one JSON line")

	var parsed jsonOutput
	require.NoError(t, json.Unmarshal(lines[0], &parsed))
	assert.NotZero(t, parsed.FetchedAt)
	assert.Contains(t, output, "test.com")
}
