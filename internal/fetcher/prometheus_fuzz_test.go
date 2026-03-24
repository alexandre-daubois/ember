package fetcher

import (
	"strings"
	"testing"
)

func FuzzParsePrometheusMetrics(f *testing.F) {
	f.Add(sampleMetrics)
	f.Add(sampleCaddyMetrics)
	f.Add(samplePerHostMetrics)
	f.Add(sampleRealCaddyMetrics)
	f.Add(samplePerHostNoCounterCodes)
	f.Add(sampleServerLabelMetrics)

	f.Add("")
	f.Add("# just a comment\n")
	f.Add("# TYPE unknown_metric gauge\nunknown_metric 42\n")
	f.Add("invalid prometheus text {{{}}}\n")
	f.Add("metric_without_type 123\n")
	f.Add("# TYPE m counter\nm{label=\"value with \\\"escapes\\\"\"} 1\n")
	f.Add("# TYPE m gauge\nm NaN\nm +Inf\nm -Inf\n")
	f.Add("# TYPE m histogram\nm_bucket{le=\"0.01\"} 0\nm_bucket{le=\"+Inf\"} 0\nm_sum 0\nm_count 0\n")

	f.Fuzz(func(t *testing.T, input string) {
		// parsePrometheusMetrics must never panic, regardless of input
		snap, err := parsePrometheusMetrics(strings.NewReader(input))
		if err != nil {
			return
		}

		for _, w := range snap.Workers {
			if w == nil {
				t.Fatal("nil worker in map")
			}
		}
		for _, h := range snap.Hosts {
			if h == nil {
				t.Fatal("nil host in map")
			}
		}
		for _, b := range snap.DurationBuckets {
			if b.CumulativeCount < 0 {
				t.Fatalf("negative cumulative count: %f", b.CumulativeCount)
			}
		}
	})
}
