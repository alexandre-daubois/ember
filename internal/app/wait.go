package app

import (
	"context"
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
	"github.com/spf13/cobra"
)

func newWaitCmd(cfg *config) *cobra.Command {
	var quiet bool
	var anyMode bool

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait until Caddy is reachable",
		Long: `Blocks until the Caddy admin API responds, then exits with code 0.
If --timeout is set and Caddy is still unreachable, exits with code 1.

With multiple --addr values, the default is to wait for every instance to be
reachable; pass --any to return as soon as the first one responds.

Useful in deployment scripts, Docker entrypoints, and CI pipelines.`,
		Example: `  ember wait
  ember wait --timeout 30s
  ember wait -q --timeout 10s && ./deploy.sh
  ember wait --addr http://prod:2019 && ember status
  ember wait --addr web1=https://a --addr web2=https://b --timeout 30s
  ember wait --any --addr http://primary --addr http://fallback`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			instances, err := newWaitInstances(cfg)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if quiet {
				w = io.Discard
			}
			return runWait(ctx, w, instances, cfg.interval, anyMode)
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output (exit code only)")
	cmd.Flags().BoolVar(&anyMode, "any", false, "Return as soon as any instance is reachable (default: wait for all)")

	return cmd
}

type waitInstance struct {
	name    string
	addr    string
	fetcher *fetcher.HTTPFetcher
	ready   bool
	printed bool
}

func newWaitInstances(cfg *config) ([]*waitInstance, error) {
	out := make([]*waitInstance, 0, len(cfg.addrs))
	for _, spec := range cfg.addrs {
		f := fetcher.NewHTTPFetcher(spec.url, 0)
		if err := configureTLS(f, effectiveTLS(spec, cfg)); err != nil {
			return nil, err
		}
		out = append(out, &waitInstance{name: spec.name, addr: spec.url, fetcher: f})
	}
	return out, nil
}

func runWait(ctx context.Context, w io.Writer, instances []*waitInstance, interval time.Duration, anyMode bool) error {
	pollWait(ctx, instances, anyMode)
	for _, inst := range instances {
		if inst.ready {
			fmt.Fprintf(w, "Caddy is ready at %s\n", inst.addr)
			inst.printed = true
		} else {
			fmt.Fprintf(w, "Waiting for Caddy at %s...\n", inst.addr)
		}
	}
	if waitDone(instances, anyMode) {
		return nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return waitTimeoutErr(instances, anyMode)
		case <-ticker.C:
			pollWait(ctx, instances, anyMode)
			for _, inst := range instances {
				if inst.ready && !inst.printed {
					fmt.Fprintf(w, "Caddy is ready at %s\n", inst.addr)
					inst.printed = true
				}
			}
			if waitDone(instances, anyMode) {
				return nil
			}
		}
	}
}

// pollWait fans out one Fetch per not-yet-ready instance. In --any mode, the
// first goroutine to find its instance reachable cancels the others' in-flight
// requests so a black-hole peer cannot keep us waiting once the answer is known.
func pollWait(ctx context.Context, instances []*waitInstance, anyMode bool) {
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for _, inst := range instances {
		if inst.ready {
			continue
		}
		wg.Add(1)
		go func(inst *waitInstance) {
			defer wg.Done()
			snap, _ := inst.fetcher.Fetch(pollCtx)
			if isReachable(snap) {
				inst.ready = true
				if anyMode {
					cancel()
				}
			}
		}(inst)
	}
	wg.Wait()
}

func waitDone(instances []*waitInstance, anyMode bool) bool {
	if anyMode {
		return slices.ContainsFunc(instances, func(i *waitInstance) bool { return i.ready })
	}
	return !slices.ContainsFunc(instances, func(i *waitInstance) bool { return !i.ready })
}

func waitTimeoutErr(instances []*waitInstance, anyMode bool) error {
	if len(instances) == 1 {
		return fmt.Errorf("timeout waiting for Caddy at %s", instances[0].addr)
	}
	var lagging []string
	for _, inst := range instances {
		if !inst.ready {
			lagging = append(lagging, inst.name)
		}
	}
	if anyMode {
		return fmt.Errorf("timeout: no Caddy instance became reachable (%s)", strings.Join(lagging, ", "))
	}
	return fmt.Errorf("timeout waiting for Caddy at instance(s): %s", strings.Join(lagging, ", "))
}
