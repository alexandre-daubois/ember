package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *config) *cobra.Command {
	var statusJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "One-line health check of Caddy",
		Long: `Performs two fetches separated by the polling interval to compute
derived metrics (RPS, latency), then prints a compact status line and exits.

Exit code 0 means Caddy is reachable, 1 means unreachable.`,
		Example: `  ember status
  ember status --json
  ember status --addr http://prod:2019
  ember status --interval 2s`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			pid := int32(cfg.frankenphpPID)
			if pid == 0 {
				detected, err := fetcher.FindFrankenPHPProcess(ctx)
				if err != nil {
					detected, _ = fetcher.FindCaddyProcess(ctx)
				}
				pid = detected
			}

			f := fetcher.NewHTTPFetcher(cfg.addr, pid)
			if err := configureTLS(f, cfg); err != nil {
				return err
			}
			return runStatus(ctx, cmd.OutOrStdout(), f, cfg.addr, cfg.interval, statusJSON)
		},
	}

	cmd.Flags().BoolVar(&statusJSON, "json", false, "Output status as JSON")

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
