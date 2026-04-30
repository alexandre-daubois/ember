package app

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *config) *cobra.Command {
	var statusJSONFlag bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "One-line health check of Caddy",
		Long: `Performs two fetches separated by the polling interval to compute
derived metrics (RPS, latency), then prints a compact status line and exits.

Exit code 0 means Caddy is reachable, 1 means unreachable. With multiple
--addr values, output is one block per instance and the exit code is 0 only
when all instances are reachable.`,
		Example: `  ember status
  ember status --json
  ember status --addr http://prod:2019
  ember status --interval 2s
  ember status --addr web1=https://a --addr web2=https://b`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			if len(cfg.addrs) >= 2 {
				return runStatusMulti(ctx, cmd.OutOrStdout(), cfg, statusJSONFlag)
			}

			pid := int32(cfg.frankenphpPID)
			if pid == 0 {
				detected, err := fetcher.FindFrankenPHPProcess(ctx)
				if err != nil {
					detected, _ = fetcher.FindCaddyProcess(ctx)
				}
				pid = detected
			}

			addr := cfg.addrs[0].url
			f := fetcher.NewHTTPFetcher(addr, pid)
			if err := configureTLS(f, cfg); err != nil {
				return err
			}
			return runStatus(ctx, cmd.OutOrStdout(), f, addr, cfg.interval, statusJSONFlag)
		},
	}

	cmd.Flags().BoolVar(&statusJSONFlag, "json", false, "Output status as JSON")

	return cmd
}

func runStatus(ctx context.Context, w io.Writer, f *fetcher.HTTPFetcher, addr string, interval time.Duration, jsonMode bool) error {
	f.DetectFrankenPHP(ctx)
	f.FetchServerNames(ctx)

	snap, _ := f.Fetch(ctx)
	if !isReachable(snap) {
		if jsonMode {
			_ = json.NewEncoder(w).Encode(statusJSON{Status: "unreachable", Addr: addr})
		} else {
			fmt.Fprintf(w, "Caddy UNREACHABLE | %s\n", addr)
		}
		return fmt.Errorf("caddy unreachable at %s", addr)
	}

	var state model.State
	state.Update(snap)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(interval):
	}

	snap2, _ := f.Fetch(ctx)
	state.Update(snap2)

	if jsonMode {
		return json.NewEncoder(w).Encode(buildStatusJSON(&state, f.HasFrankenPHP()))
	}
	fmt.Fprintln(w, formatStatusLine(&state, f.HasFrankenPHP()))
	return nil
}

type statusJSON struct {
	Name        string   `json:"name,omitempty"`
	Status      string   `json:"status"`
	Addr        string   `json:"addr,omitempty"`
	Hosts       int      `json:"hosts,omitempty"`
	RPS         float64  `json:"rps"`
	P99         *float64 `json:"p99,omitempty"`
	CPUPercent  float64  `json:"cpuPercent"`
	RSSBytes    uint64   `json:"rssBytes"`
	UptimeHuman string   `json:"uptime,omitempty"`
	FrankenPHP  *fpJSON  `json:"frankenphp,omitempty"`
}

type fpJSON struct {
	Busy    int `json:"busy"`
	Total   int `json:"total"`
	Workers int `json:"workers"`
}

type statusMultiJSON struct {
	Status    string       `json:"status"`
	Instances []statusJSON `json:"instances"`
}

func buildStatusJSON(state *model.State, hasFrankenPHP bool) statusJSON {
	snap := state.Current
	d := state.Derived

	s := statusJSON{
		Status:     "ok",
		Hosts:      len(snap.Metrics.Hosts),
		RPS:        d.RPS,
		CPUPercent: snap.Process.CPUPercent,
		RSSBytes:   snap.Process.RSS,
	}

	if d.HasPercentiles {
		s.P99 = &d.P99
	}
	if snap.Process.Uptime > 0 {
		s.UptimeHuman = model.FormatUptime(snap.Process.Uptime)
	}
	if hasFrankenPHP {
		total := d.TotalBusy + d.TotalIdle
		s.FrankenPHP = &fpJSON{
			Busy:    d.TotalBusy,
			Total:   total,
			Workers: len(snap.Metrics.Workers),
		}
	}

	return s
}

func isReachable(snap *fetcher.Snapshot) bool {
	if snap == nil {
		return false
	}
	return snap.Metrics.HasHTTPMetrics ||
		len(snap.Threads.ThreadDebugStates) > 0 ||
		snap.Process.RSS > 0 ||
		snap.Metrics.ProcessRSSBytes > 0
}

func formatStatusLine(state *model.State, hasFrankenPHP bool) string {
	snap := state.Current
	d := state.Derived

	parts := []string{"Caddy OK"}

	if hostCount := len(snap.Metrics.Hosts); hostCount > 0 {
		parts = append(parts, fmt.Sprintf("%d hosts", hostCount))
	}

	parts = append(parts, fmt.Sprintf("%.0f rps", d.RPS))

	if d.HasPercentiles {
		parts = append(parts, fmt.Sprintf("P99 %.0fms", d.P99))
	}

	parts = append(parts, fmt.Sprintf("CPU %.1f%%", snap.Process.CPUPercent))
	parts = append(parts, fmt.Sprintf("RSS %s", formatRSS(snap.Process.RSS)))

	if snap.Process.Uptime > 0 {
		parts = append(parts, fmt.Sprintf("up %s", model.FormatUptime(snap.Process.Uptime)))
	}

	if upCount := len(snap.Metrics.Upstreams); upCount > 0 {
		healthy := 0
		for _, u := range snap.Metrics.Upstreams {
			if u.Healthy >= 1 {
				healthy++
			}
		}
		parts = append(parts, fmt.Sprintf("%d/%d upstreams healthy", healthy, upCount))
	}

	if hasFrankenPHP {
		total := d.TotalBusy + d.TotalIdle
		fpPart := fmt.Sprintf("FrankenPHP %d/%d busy", d.TotalBusy, total)
		if workerCount := len(snap.Metrics.Workers); workerCount > 0 {
			fpPart += fmt.Sprintf(" | %d workers", workerCount)
		}
		parts = append(parts, fpPart)
	}

	return strings.Join(parts, " | ")
}

func formatRSS(rss uint64) string {
	mb := float64(rss) / 1024 / 1024
	if mb >= 1024 {
		return fmt.Sprintf("%.1fGB", mb/1024)
	}
	return fmt.Sprintf("%.0fMB", mb)
}

// statusResult holds the per-instance outcome of a multi status run. Only one
// of (line, payload) is populated based on the requested output format; both
// are filled when reachable.
type statusResult struct {
	name       string
	addr       string
	reachable  bool
	line       string
	payload    statusJSON
	frankenPHP bool
}

func runStatusMulti(ctx context.Context, w io.Writer, cfg *config, jsonMode bool) error {
	results := make([]statusResult, len(cfg.addrs))
	var wg sync.WaitGroup
	for i, spec := range cfg.addrs {
		wg.Add(1)
		go func(i int, spec addrSpec) {
			defer wg.Done()
			results[i] = collectInstanceStatus(ctx, cfg, spec)
		}(i, spec)
	}
	wg.Wait()

	slices.SortFunc(results, func(a, b statusResult) int { return cmp.Compare(a.name, b.name) })

	allOk := true
	allDown := true
	for _, r := range results {
		if r.reachable {
			allDown = false
		} else {
			allOk = false
		}
	}

	overall := "ok"
	switch {
	case allDown:
		overall = "unreachable"
	case !allOk:
		overall = "degraded"
	}

	if jsonMode {
		body := statusMultiJSON{Status: overall, Instances: make([]statusJSON, len(results))}
		for i, r := range results {
			r.payload.Name = r.name
			r.payload.Addr = r.addr
			if !r.reachable {
				r.payload.Status = "unreachable"
			}
			body.Instances[i] = r.payload
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			if r.reachable {
				fmt.Fprintf(w, "[%s] %s\n", r.name, r.line)
			} else {
				fmt.Fprintf(w, "[%s] Caddy UNREACHABLE | %s\n", r.name, r.addr)
			}
		}
	}

	if !allOk {
		var down []string
		for _, r := range results {
			if !r.reachable {
				down = append(down, r.name)
			}
		}
		return fmt.Errorf("caddy unreachable at instance(s): %s", strings.Join(down, ", "))
	}
	return nil
}

func collectInstanceStatus(ctx context.Context, cfg *config, spec addrSpec) statusResult {
	res := statusResult{name: spec.name, addr: spec.url}
	f := fetcher.NewHTTPFetcher(spec.url, 0)
	if err := configureTLS(f, cfg); err != nil {
		return res
	}
	f.DetectFrankenPHP(ctx)
	f.FetchServerNames(ctx)

	snap, _ := f.Fetch(ctx)
	if !isReachable(snap) {
		return res
	}

	var state model.State
	state.Update(snap)

	select {
	case <-ctx.Done():
		return res
	case <-time.After(cfg.interval):
	}

	snap2, _ := f.Fetch(ctx)
	state.Update(snap2)

	res.reachable = true
	res.frankenPHP = f.HasFrankenPHP()
	res.line = formatStatusLine(&state, res.frankenPHP)
	res.payload = buildStatusJSON(&state, res.frankenPHP)
	return res
}
