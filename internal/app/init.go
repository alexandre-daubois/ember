package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/spf13/cobra"
)

func newInitCmd(cfg *config) *cobra.Command {
	var yes bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Check and configure Caddy for Ember",
		Long: `Verifies that Caddy is reachable, checks if HTTP metrics are enabled,
and offers to enable them via the admin API if missing.

This command does not modify any files on disk. It only uses the Caddy
admin API to read and optionally write configuration.`,
		Example: `  ember init
  ember init --addr https://prod:2019 --ca-cert ca.pem
  ember init -y
  ember init -yq`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			f := fetcher.NewHTTPFetcher(cfg.addr, 0)
			if err := configureTLS(f, cfg); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if quiet {
				w = io.Discard
			}
			return runInit(ctx, w, os.Stdin, f, cfg.addr, yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output (errors still reported via exit code)")

	return cmd
}

type initCheck struct {
	label  string
	ok     bool
	detail string
}

func runInit(ctx context.Context, w io.Writer, r io.Reader, f *fetcher.HTTPFetcher, addr string, autoYes bool) error {
	fmt.Fprintf(w, "Checking Caddy at %s...\n", addr)

	if err := f.CheckAdminAPI(ctx); err != nil {
		printCheck(w, initCheck{label: "Admin API", ok: false, detail: err.Error()})
		return fmt.Errorf("caddy admin API is not reachable at %s", addr)
	}
	printCheck(w, initCheck{label: "Admin API reachable", ok: true})

	servers := f.FetchServerNames(ctx)
	if len(servers) > 0 {
		printCheck(w, initCheck{label: fmt.Sprintf("%d HTTP server(s) configured", len(servers)), ok: true, detail: strings.Join(servers, ", ")})
	} else {
		printCheck(w, initCheck{label: "No HTTP servers found", ok: false})
	}

	metricsEnabled, err := f.CheckMetricsEnabled(ctx)
	if err != nil {
		printCheck(w, initCheck{label: "Metrics check failed", ok: false, detail: err.Error()})
	} else if metricsEnabled {
		printCheck(w, initCheck{label: "HTTP metrics enabled", ok: true})
	} else {
		printCheck(w, initCheck{label: "HTTP metrics not enabled", ok: false})

		if !promptYesNo(w, r, "\nEnable HTTP metrics via the admin API? Caddy does not need to restart.", autoYes) {
			fmt.Fprintln(w, "  Skipped. Add the metrics directive to your Caddyfile manually:")
			fmt.Fprintln(w, "    { metrics }")
		} else {
			if err := f.EnableMetrics(ctx); err != nil {
				printCheck(w, initCheck{label: "Failed to enable metrics", ok: false, detail: err.Error()})
				return fmt.Errorf("could not enable metrics: %w", err)
			}
			printCheck(w, initCheck{label: "HTTP metrics enabled", ok: true})
		}
	}

	fmt.Fprintln(w, "\nChecking FrankenPHP...")
	hasFP := f.DetectFrankenPHP(ctx)
	if !hasFP {
		printCheck(w, initCheck{label: "FrankenPHP not detected (Caddy-only mode)", ok: true})
	} else {
		printCheck(w, initCheck{label: "FrankenPHP detected", ok: true})

		fpCfg, err := f.FetchFrankenPHPConfig(ctx)
		if err == nil && fpCfg != nil {
			if fpCfg.NumThreads > 0 {
				printCheck(w, initCheck{label: fmt.Sprintf("%d threads configured", fpCfg.NumThreads), ok: true})
			}
			for _, wk := range fpCfg.Workers {
				name := wk.FileName
				if wk.Name != "" {
					name = wk.Name + " (" + wk.FileName + ")"
				}
				printCheck(w, initCheck{label: fmt.Sprintf("Worker: %s (%d instances)", name, wk.Num), ok: true})
			}
		}
	}

	fmt.Fprintln(w, "\nVerifying metrics collection...")
	snap, _ := f.Fetch(ctx)
	if snap != nil && snap.Metrics.HasHTTPMetrics {
		printCheck(w, initCheck{label: "caddy_http_* metrics present", ok: true})
	} else if snap != nil {
		printCheck(w, initCheck{label: "No HTTP traffic metrics yet", ok: false, detail: "metrics will appear after the first HTTP request"})
	}

	if snap != nil && hasFP && snap.Metrics.TotalThreads > 0 {
		printCheck(w, initCheck{label: fmt.Sprintf("frankenphp_* metrics present (%.0f threads)", snap.Metrics.TotalThreads), ok: true})
	}

	fmt.Fprintln(w, "\nEmber is ready. Run \"ember\" to start the dashboard.")
	return nil
}

func printCheck(w io.Writer, c initCheck) {
	marker := "  ✓ "
	if !c.ok {
		marker = "  ✗ "
	}
	if c.detail != "" {
		fmt.Fprintf(w, "%s%s (%s)\n", marker, c.label, c.detail)
	} else {
		fmt.Fprintf(w, "%s%s\n", marker, c.label)
	}
}

func promptYesNo(w io.Writer, r io.Reader, prompt string, autoYes bool) bool {
	if autoYes {
		fmt.Fprintf(w, "%s [Y/n] y\n", prompt)
		return true
	}
	fmt.Fprintf(w, "%s [Y/n] ", prompt)
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "" || answer == "y" || answer == "yes"
}
