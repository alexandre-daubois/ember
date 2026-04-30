package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newDiffCmd(_ *config) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <before> <after>",
		Short: "Compare two JSON or JSONL snapshots",
		Long: `Compares two snapshots produced by "ember --json --once" and shows
the deltas for key metrics: RPS, latency, error rate, CPU, RSS, and
per-host breakdowns.

Both single-instance JSON and multi-instance JSONL snapshots are
accepted. With multi-instance input, lines are grouped by the
"instance" field (last entry per instance wins) and one diff block is
emitted per instance.

Exit code 0 means no regressions detected, 1 means regressions found
on at least one instance.`,
		Example: `  ember --json --once > before.json
  # ... deploy ...
  ember --json --once > after.json
  ember diff before.json after.json

  ember --json --once --addr web1=… --addr web2=… > before.jsonl
  # ... deploy ...
  ember --json --once --addr web1=… --addr web2=… > after.jsonl
  ember diff before.jsonl after.jsonl`,
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.OutOrStdout(), args[0], args[1])
		},
	}
}

func runDiff(w io.Writer, beforePath, afterPath string) error {
	before, err := loadSnapshots(beforePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", beforePath, err)
	}
	after, err := loadSnapshots(afterPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", afterPath, err)
	}

	hasRegressions := false
	if isSingleInstance(before) && isSingleInstance(after) {
		d := computeDiff(before[""], after[""])
		fmt.Fprint(w, formatDiffBody(d))
		hasRegressions = d.hasRegressions
	} else {
		seen := make(map[string]bool, len(before)+len(after))
		names := make([]string, 0, len(before)+len(after))
		for name := range before {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		for name := range after {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		sort.Strings(names)

		for i, name := range names {
			if i > 0 {
				fmt.Fprintln(w)
			}
			label := name
			if label == "" {
				label = "(unnamed)"
			}
			fmt.Fprintf(w, "== %s ==\n", label)
			d := computeDiff(before[name], after[name])
			fmt.Fprint(w, formatDiffBody(d))
			if d.hasRegressions {
				hasRegressions = true
			}
		}
	}

	if hasRegressions {
		fmt.Fprint(w, "\n!! Regressions detected\n")
		return fmt.Errorf("regressions detected")
	}
	fmt.Fprint(w, "\nNo regressions detected\n")
	return nil
}

func isSingleInstance(snaps map[string]jsonOutput) bool {
	if len(snaps) != 1 {
		return false
	}
	_, ok := snaps[""]
	return ok
}

func loadSnapshots(path string) (map[string]jsonOutput, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("file is empty (did the snapshot command succeed?)")
	}

	out := make(map[string]jsonOutput)
	dec := json.NewDecoder(f)
	for {
		var o jsonOutput
		if err := dec.Decode(&o); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		out[o.Instance] = o
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no JSON object found")
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

func formatDiffBody(d diffResult) string {
	var b strings.Builder

	b.WriteString("Global\n")
	writeDiffLines(&b, d.global)

	if len(d.hosts) > 0 {
		b.WriteString("\nPer-host changes\n")
		for _, h := range d.hosts {
			fmt.Fprintf(&b, "\n  %s\n", h.host)
			writeDiffLines(&b, h.lines)
		}
	}

	return b.String()
}

func writeDiffLines(b *strings.Builder, lines []diffLine) {
	for _, l := range lines {
		marker := "  "
		if l.regression {
			marker = "! "
		}
		fmt.Fprintf(b, "%s  %-14s %10s -> %-10s  %s\n", marker, l.label, l.before, l.after, l.delta)
	}
}
