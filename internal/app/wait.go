package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/spf13/cobra"
)

func newWaitCmd(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "wait",
		Short: "Wait until Caddy is reachable",
		Long: `Blocks until the Caddy admin API responds, then exits with code 0.
If --timeout is set and Caddy is still unreachable, exits with code 1.

Useful in deployment scripts, Docker entrypoints, and CI pipelines.`,
		Example: `  ember wait
  ember wait --timeout 30s
  ember wait --addr http://prod:2019 && ember status`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			f := fetcher.NewHTTPFetcher(cfg.addr, 0)
			return runWait(ctx, cmd.OutOrStdout(), f, cfg.addr, cfg.interval)
		},
	}
}

func runWait(ctx context.Context, w io.Writer, f *fetcher.HTTPFetcher, addr string, interval time.Duration) error {
	snap, _ := f.Fetch(ctx)
	if isReachable(snap) {
		fmt.Fprintf(w, "Caddy is ready at %s\n", addr)
		return nil
	}

	fmt.Fprintf(w, "Waiting for Caddy at %s...\n", addr)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Caddy at %s", addr)
		case <-ticker.C:
			snap, _ := f.Fetch(ctx)
			if isReachable(snap) {
				fmt.Fprintf(w, "Caddy is ready at %s\n", addr)
				return nil
			}
		}
	}
}
