package app

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <before.json> <after.json>",
		Short: "Compare two JSON snapshots",
		Long: `Compares two JSON snapshots produced by "ember --json --once" and
shows the deltas for key metrics: RPS, latency, error rate, CPU, RSS,
and per-host breakdowns.

Exit code 0 means no regressions detected, 1 means regressions found.`,
		Example: `  ember --json --once > before.json
  # ... deploy ...
  ember --json --once > after.json
  ember diff before.json after.json`,
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.OutOrStdout(), args[0], args[1])
		},
	}
}

func runDiff(w io.Writer, beforePath, afterPath string) error {
	before, err := loadSnapshot(beforePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", beforePath, err)
	}
	after, err := loadSnapshot(afterPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", afterPath, err)
	}

	d := computeDiff(before, after)
	fmt.Fprint(w, formatDiff(d))

	if d.hasRegressions {
		return fmt.Errorf("regressions detected")
	}
	return nil
}

func loadSnapshot(path string) (jsonOutput, error) {
	f, err := os.Open(path)
	if err != nil {
		return jsonOutput{}, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return jsonOutput{}, err
	}
	if info.Size() == 0 {
		return jsonOutput{}, fmt.Errorf("file is empty (did the snapshot command succeed?)")
	}

	var out jsonOutput
	if err := json.NewDecoder(f).Decode(&out); err != nil {
		return jsonOutput{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return out, nil
}

type diffResult struct {
	global         []diffLine
	hosts          []hostDiff
	hasRegressions bool
}

type diffLine struct {
	label      string
	before     string
	after      string
	delta      string
	regression bool
}

type hostDiff struct {
	host  string
	lines []diffLine
}

func computeDiff(before, after jsonOutput) diffResult {
	var d diffResult

	hasDerived := before.Derived != nil && after.Derived != nil &&
		(before.Derived.RPS > 0 || after.Derived.RPS > 0)

	if hasDerived {
		d.global = append(d.global, numericDiff("RPS", before.Derived.RPS, after.Derived.RPS, "/s", false))
		d.global = append(d.global, numericDiff("Avg latency", before.Derived.AvgTime, after.Derived.AvgTime, "ms", true))
		d.global = append(d.global, numericDiff("Error rate", before.Derived.ErrorRate, after.Derived.ErrorRate, "/s", true))
		if before.Derived.P99 != nil && after.Derived.P99 != nil {
			d.global = append(d.global, numericDiff("P99", *before.Derived.P99, *after.Derived.P99, "ms", true))
		}
	}

	// raw cumulative counters (always available, even from --once snapshots)
	d.global = append(d.global, numericDiff("Requests", before.Metrics.HTTPRequestDurationCount, after.Metrics.HTTPRequestDurationCount, "", false))
	if before.Metrics.HTTPRequestDurationCount > 0 && after.Metrics.HTTPRequestDurationCount > 0 {
		beforeAvg := before.Metrics.HTTPRequestDurationSum / before.Metrics.HTTPRequestDurationCount * 1000
		afterAvg := after.Metrics.HTTPRequestDurationSum / after.Metrics.HTTPRequestDurationCount * 1000
		d.global = append(d.global, numericDiff("Avg (cumul.)", beforeAvg, afterAvg, "ms", true))
	}
	d.global = append(d.global, numericDiff("Errors", before.Metrics.HTTPRequestErrorsTotal, after.Metrics.HTTPRequestErrorsTotal, "", true))
	d.global = append(d.global, numericDiff("In-flight", before.Metrics.HTTPRequestsInFlight, after.Metrics.HTTPRequestsInFlight, "", true))
	d.global = append(d.global, numericDiff("CPU", before.Process.CPUPercent, after.Process.CPUPercent, "%", true))
	d.global = append(d.global, numericDiff("RSS", float64(before.Process.RSS)/1024/1024, float64(after.Process.RSS)/1024/1024, "MB", true))

	afterHosts := make(map[string]jsonHost, len(after.Hosts))
	for _, h := range after.Hosts {
		afterHosts[h.Host] = h
	}
	beforeHosts := make(map[string]jsonHost, len(before.Hosts))
	for _, h := range before.Hosts {
		beforeHosts[h.Host] = h
	}

	allHosts := make(map[string]bool)
	for _, h := range before.Hosts {
		allHosts[h.Host] = true
	}
	for _, h := range after.Hosts {
		allHosts[h.Host] = true
	}

	sortedHosts := make([]string, 0, len(allHosts))
	for h := range allHosts {
		sortedHosts = append(sortedHosts, h)
	}
	sort.Strings(sortedHosts)

	for _, host := range sortedHosts {
		bh := beforeHosts[host]
		ah := afterHosts[host]

		var lines []diffLine
		if hasDerived {
			lines = append(lines, numericDiff("RPS", bh.RPS, ah.RPS, "/s", false))
			lines = append(lines, numericDiff("Avg latency", bh.AvgTime, ah.AvgTime, "ms", true))
			lines = append(lines, numericDiff("Error rate", bh.ErrorRate, ah.ErrorRate, "/s", true))
			if bh.P99 != nil && ah.P99 != nil {
				lines = append(lines, numericDiff("P99", *bh.P99, *ah.P99, "ms", true))
			}
		}
		lines = append(lines, numericDiff("In-flight", bh.InFlight, ah.InFlight, "", true))

		hasChange := false
		for _, l := range lines {
			if l.delta != "=" {
				hasChange = true
			}
		}
		if hasChange {
			d.hosts = append(d.hosts, hostDiff{host: host, lines: lines})
		}
	}

	for _, l := range d.global {
		if l.regression {
			d.hasRegressions = true
		}
	}
	for _, h := range d.hosts {
		for _, l := range h.lines {
			if l.regression {
				d.hasRegressions = true
			}
		}
	}

	return d
}

func numericDiff(label string, before, after float64, unit string, higherIsBad bool) diffLine {
	l := diffLine{
		label:  label,
		before: formatVal(before, unit),
		after:  formatVal(after, unit),
	}

	diff := after - before
	if math.Abs(diff) < 0.01 {
		l.delta = "="
		return l
	}

	if before == 0 {
		l.delta = "new"
		if higherIsBad && after > 0 {
			l.regression = true
		}
		return l
	}

	pct := (diff / before) * 100

	sign := "+"
	if diff < 0 {
		sign = ""
	}
	l.delta = fmt.Sprintf("%s%.1f%%", sign, pct)

	threshold := 10.0
	if higherIsBad && pct > threshold {
		l.regression = true
	}
	if !higherIsBad && pct < -threshold {
		l.regression = true
	}

	return l
}

func formatVal(v float64, unit string) string {
	if v == 0 {
		return "0"
	}
	if v >= 1000 {
		return fmt.Sprintf("%.0f%s", v, unit)
	}
	return fmt.Sprintf("%.1f%s", v, unit)
}

func formatDiff(d diffResult) string {
	var b strings.Builder

	b.WriteString("Global\n")
	writeDiffLines(&b, d.global)

	if len(d.hosts) > 0 {
		b.WriteString("\nPer-host changes\n")
		for _, h := range d.hosts {
			b.WriteString(fmt.Sprintf("\n  %s\n", h.host))
			writeDiffLines(&b, h.lines)
		}
	}

	if d.hasRegressions {
		b.WriteString("\n!! Regressions detected\n")
	} else {
		b.WriteString("\nNo regressions detected\n")
	}

	return b.String()
}

func writeDiffLines(b *strings.Builder, lines []diffLine) {
	for _, l := range lines {
		marker := "  "
		if l.regression {
			marker = "! "
		}
		b.WriteString(fmt.Sprintf("%s  %-14s %10s -> %-10s  %s\n", marker, l.label, l.before, l.after, l.delta))
	}
}
