package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSnapshot(t *testing.T, dir, name string, out jsonOutput) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(f).Encode(out))
	require.NoError(t, f.Close())
	return path
}

func pf(v float64) *float64 { return &v }

func TestRunDiff_NoRegressions(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{RPS: 100, AvgTime: 20, P99: pf(80)},
		Process: fetcher.ProcessMetrics{CPUPercent: 5, RSS: 50 * 1024 * 1024},
		Hosts:   []jsonHost{{Host: "api.com", RPS: 100, AvgTime: 20, P99: pf(80)}},
	}
	after := jsonOutput{
		Derived: &jsonDerived{RPS: 105, AvgTime: 19, P99: pf(75)},
		Process: fetcher.ProcessMetrics{CPUPercent: 4, RSS: 48 * 1024 * 1024},
		Hosts:   []jsonHost{{Host: "api.com", RPS: 105, AvgTime: 19, P99: pf(75)}},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No regressions")
}

func TestRunDiff_LatencyRegression(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{RPS: 100, AvgTime: 20},
		Process: fetcher.ProcessMetrics{CPUPercent: 5, RSS: 50 * 1024 * 1024},
	}
	after := jsonOutput{
		Derived: &jsonDerived{RPS: 100, AvgTime: 50},
		Process: fetcher.ProcessMetrics{CPUPercent: 5, RSS: 50 * 1024 * 1024},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "regressions")
	assert.Contains(t, buf.String(), "Regressions detected")
	assert.Contains(t, buf.String(), "Avg latency")
}

func TestRunDiff_RPSDropRegression(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{RPS: 500},
		Process: fetcher.ProcessMetrics{},
	}
	after := jsonOutput{
		Derived: &jsonDerived{RPS: 200},
		Process: fetcher.ProcessMetrics{},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "RPS")
}

func TestRunDiff_NewHost(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{},
		Hosts:   []jsonHost{{Host: "old.com", InFlight: 2}},
	}
	after := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{},
		Hosts: []jsonHost{
			{Host: "old.com", InFlight: 2},
			{Host: "new.com", InFlight: 5},
		},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	_ = runDiff(&buf, bp, ap)

	assert.Contains(t, buf.String(), "new.com")
}

func TestRunDiff_IdenticalSnapshots(t *testing.T) {
	dir := t.TempDir()

	snap := jsonOutput{
		Derived: &jsonDerived{RPS: 100, AvgTime: 20},
		Process: fetcher.ProcessMetrics{CPUPercent: 5, RSS: 50 * 1024 * 1024},
		Hosts:   []jsonHost{{Host: "api.com", RPS: 100, AvgTime: 20}},
	}

	bp := writeSnapshot(t, dir, "before.json", snap)
	ap := writeSnapshot(t, dir, "after.json", snap)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No regressions")
}

func TestRunDiff_FileNotFound(t *testing.T) {
	var buf bytes.Buffer
	err := runDiff(&buf, "/nonexistent/before.json", "/nonexistent/after.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load")
}

func TestRunDiff_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(empty, []byte{}, 0o600))
	good := writeSnapshot(t, dir, "good.json", jsonOutput{Derived: &jsonDerived{}, Process: fetcher.ProcessMetrics{}})

	var buf bytes.Buffer
	err := runDiff(&buf, empty, good)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestRunDiff_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("not json"), 0o600))
	good := writeSnapshot(t, dir, "good.json", jsonOutput{Derived: &jsonDerived{}, Process: fetcher.ProcessMetrics{}})

	var buf bytes.Buffer
	err := runDiff(&buf, bad, good)
	require.Error(t, err)
}

func TestRunDiff_ErrorRateRegression(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{RPS: 100, ErrorRate: 0},
		Process: fetcher.ProcessMetrics{},
	}
	after := jsonOutput{
		Derived: &jsonDerived{RPS: 100, ErrorRate: 5},
		Process: fetcher.ProcessMetrics{},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "Error rate")
}

func TestRun_DiffCmd_ExecutesRunE(t *testing.T) {
	dir := t.TempDir()
	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")
	require.NoError(t, os.WriteFile(beforePath, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(afterPath, []byte("{}"), 0o600))

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"diff", beforePath, afterPath})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute(),
		"diffing two empty snapshots must succeed end-to-end through cobra")
	assert.Contains(t, buf.String(), "No regressions detected")
}

func TestRun_DiffCmd_RequiresExactlyTwoArgs(t *testing.T) {
	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"diff", "only-one.json"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err, "cobra must reject diff with one argument")
}

func TestRunDiff_NilDerived(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Process: fetcher.ProcessMetrics{CPUPercent: 10, RSS: 30 * 1024 * 1024},
		Metrics: fetcher.MetricsSnapshot{HTTPRequestDurationCount: 500, HTTPRequestDurationSum: 25},
	}
	after := jsonOutput{
		Process: fetcher.ProcessMetrics{CPUPercent: 10, RSS: 31 * 1024 * 1024},
		Metrics: fetcher.MetricsSnapshot{HTTPRequestDurationCount: 800, HTTPRequestDurationSum: 40},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Requests")
	assert.Contains(t, buf.String(), "Avg (cumul.)")
	assert.NotContains(t, buf.String(), "RPS")
}

func TestRunDiff_HostDisappeared(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{},
		Hosts: []jsonHost{
			{Host: "alive.com", InFlight: 3},
			{Host: "gone.com", InFlight: 2},
		},
	}
	after := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{},
		Hosts:   []jsonHost{{Host: "alive.com", InFlight: 3}},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	_ = runDiff(&buf, bp, ap)

	assert.Contains(t, buf.String(), "gone.com")
}

func TestRunDiff_RawCountersOnly(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{RSS: 40 * 1024 * 1024},
		Metrics: fetcher.MetricsSnapshot{
			HTTPRequestDurationCount: 1000,
			HTTPRequestDurationSum:   50,
		},
	}
	after := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{RSS: 41 * 1024 * 1024},
		Metrics: fetcher.MetricsSnapshot{
			HTTPRequestDurationCount: 2000,
			HTTPRequestDurationSum:   100,
		},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Requests")
	assert.Contains(t, out, "1000")
	assert.Contains(t, out, "2000")
	assert.Contains(t, out, "Avg (cumul.)")
}

func TestRunDiff_CPURegression(t *testing.T) {
	dir := t.TempDir()

	before := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{CPUPercent: 10},
	}
	after := jsonOutput{
		Derived: &jsonDerived{},
		Process: fetcher.ProcessMetrics{CPUPercent: 50},
	}

	bp := writeSnapshot(t, dir, "before.json", before)
	ap := writeSnapshot(t, dir, "after.json", after)

	var buf bytes.Buffer
	err := runDiff(&buf, bp, ap)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "CPU")
	assert.Contains(t, buf.String(), "Regressions")
}

func TestRunDiff_SecondFileInvalid(t *testing.T) {
	dir := t.TempDir()
	good := writeSnapshot(t, dir, "good.json", jsonOutput{Derived: &jsonDerived{}, Process: fetcher.ProcessMetrics{}})
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{invalid"), 0o600))

	var buf bytes.Buffer
	err := runDiff(&buf, good, bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad.json")
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestNumericDiff_BothZero(t *testing.T) {
	l := numericDiff("RPS", 0, 0, "/s", false)
	assert.Equal(t, "=", l.delta)
	assert.False(t, l.regression)
}

func TestNumericDiff_NewValueNotBad(t *testing.T) {
	l := numericDiff("RPS", 0, 100, "/s", false)
	assert.Equal(t, "new", l.delta)
	assert.False(t, l.regression)
}

func TestNumericDiff_NewValueBad(t *testing.T) {
	l := numericDiff("Errors", 0, 5, "/s", true)
	assert.Equal(t, "new", l.delta)
	assert.True(t, l.regression)
}

func TestNumericDiff_SmallChange(t *testing.T) {
	l := numericDiff("Latency", 100, 105, "ms", true)
	assert.False(t, l.regression, "5% increase should not be a regression (threshold is 10%)")
}

func TestFormatVal(t *testing.T) {
	assert.Equal(t, "0", formatVal(0, "ms"))
	assert.Equal(t, "12.3ms", formatVal(12.3, "ms"))
	assert.Equal(t, "1500ms", formatVal(1500, "ms"))
}

func TestNumericDiff_Equal(t *testing.T) {
	l := numericDiff("RPS", 100, 100, "/s", false)
	assert.Equal(t, "=", l.delta)
	assert.False(t, l.regression)
}

func TestNumericDiff_HigherIsBadRegression(t *testing.T) {
	l := numericDiff("Latency", 20, 30, "ms", true)
	assert.True(t, l.regression)
	assert.Contains(t, l.delta, "+50")
}

func TestNumericDiff_LowerIsBadRegression(t *testing.T) {
	l := numericDiff("RPS", 100, 50, "/s", false)
	assert.True(t, l.regression)
	assert.Contains(t, l.delta, "-50")
}

func TestRun_DiffHelp(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"diff", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "before.json")
	assert.Contains(t, out, "after.json")
}

func TestRun_DiffRequiresTwoArgs(t *testing.T) {
	err := Run([]string{"diff"}, "0.0.0")
	require.Error(t, err)
}
