package app

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
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
  ember init -yq
  ember init -y --addr web1=https://a --addr web2=https://b`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, tCancel := contextWithTimeout(ctx, cfg.timeout)
			defer tCancel()

			w := cmd.OutOrStdout()
			if quiet {
				w = io.Discard
			}

			if len(cfg.addrs) >= 2 {
				if !yes {
					return fmt.Errorf("`ember init` requires --yes (-y) when multiple --addr are provided (interactive prompts are disabled in multi-instance mode)")
				}
				return runInitMulti(ctx, w, cfg)
			}

			addr := cfg.addrs[0].url
			f := fetcher.NewHTTPFetcher(addr, 0)
			if err := configureTLS(f, cfg); err != nil {
				return err
			}
			return runInit(ctx, w, os.Stdin, f, addr, yes)
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

	if snap != nil && hasWildcardHost(snap.Metrics.Hosts) {
		printCheck(w, initCheck{
			label:  "All traffic grouped under \"*\" (no per-host breakdown)",
			ok:     false,
			detail: "add host matchers to your Caddyfile routes for per-host metrics",
		})
	}

	fmt.Fprintln(w, "\nAccess logs:")
	printCheck(w, initCheck{
		label:  "Live streaming via hot-registered Caddy sink",
		ok:     true,
		detail: "Ember enables access logs on each HTTP server at run time and removes its config on exit",
	})

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

func hasWildcardHost(hosts map[string]*fetcher.HostMetrics) bool {
	h, ok := hosts["*"]
	return ok && h.RequestsTotal > 0
}

type initResult struct {
	name   string
	output string
	err    error
}

func runInitMulti(ctx context.Context, w io.Writer, cfg *config) error {
	results := make([]initResult, len(cfg.addrs))
	var wg sync.WaitGroup
	for i, spec := range cfg.addrs {
		wg.Add(1)
		go func(i int, spec addrSpec) {
			defer wg.Done()
			results[i] = collectInitResult(ctx, cfg, spec)
		}(i, spec)
	}
	wg.Wait()

	slices.SortFunc(results, func(a, b initResult) int { return cmp.Compare(a.name, b.name) })

	var failed []string
	for i, r := range results {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "--- %s ---\n", r.name)
		fmt.Fprint(w, r.output)
		if r.err != nil {
			failed = append(failed, r.name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("init failed for instance(s): %s", strings.Join(failed, ", "))
	}
	return nil
}

func collectInitResult(ctx context.Context, cfg *config, spec addrSpec) initResult {
	var buf bytes.Buffer
	f := fetcher.NewHTTPFetcher(spec.url, 0)
	if err := configureTLS(f, cfg); err != nil {
		fmt.Fprintf(&buf, "  ✗ TLS configuration failed (%s)\n", err)
		return initResult{name: spec.name, output: buf.String(), err: err}
	}
	err := runInit(ctx, &buf, strings.NewReader(""), f, spec.url, true)
	return initResult{name: spec.name, output: buf.String(), err: err}
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
