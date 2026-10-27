package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/ui"
	"github.com/spf13/cobra"
)

// localModeFlags are the flags --remote refuses.
var localModeFlags = []string{
	"daemon", "json", "expose", "stdin-logs", "log-listen", "once",
	"metrics-auth", "serve-remote", "expose-cert", "expose-key", "expose-client-ca",
}

func remoteIncompatibleFlag(cmd *cobra.Command) string {
	for _, name := range localModeFlags {
		if f := cmd.Flag(name); f != nil && f.Value.String() != f.DefValue {
			return "--" + name
		}
	}
	return ""
}

func prepareRemote(cmd *cobra.Command, cfg *config) error {
	if flag := remoteIncompatibleFlag(cmd); flag != "" {
		return fmt.Errorf("--remote is incompatible with %s", flag)
	}
	u, err := url.Parse(cfg.remote)
	if err == nil && u.User != nil {
		return errors.New("--remote must not carry credentials: present a client certificate with --client-cert and --client-key")
	}
	// The fetcher appends its paths to this URL: a query or a fragment would swallow them.
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("--remote must be an https:// URL without a query or a fragment")
	}
	if cfg.interval < minInterval {
		return fmt.Errorf("--interval must be at least %s", minInterval)
	}
	if cmd.Flag("addr").Changed {
		cfg.logger.Warn("--addr and EMBER_ADDR are ignored with --remote")
	}
	cfg.remoteURL = u
	return nil
}

func runRemote(ctx context.Context, cfg *config, version string) error {
	f := fetcher.NewRemoteFetcher(cfg.remoteURL)
	if err := configureTLS(f.HTTPFetcher, effectiveTLS(addrSpec{}, cfg)); err != nil {
		return err
	}
	defer f.CloseIdleConnections()
	page, refusal := f.FetchLogs(ctx, 0, -1)
	if remoteFatal(refusal) {
		return fmt.Errorf("remote daemon %s: %w", cfg.remoteURL.Host, refusal)
	}

	hasFrankenPHP := f.DetectFrankenPHP(ctx)
	f.FetchServerNames(ctx)
	return runTUI(f, cfg, cfg.interval, hasFrankenPHP, version, nil, func(u *ui.Config) func() {
		return startRemoteLogs(f, page, refusal, u)
	})
}

func remoteFatal(err error) bool {
	var refused fetcher.RefusedError
	return err != nil && (!errors.As(err, &refused) || refused.Status == http.StatusUnauthorized || refused.Status == http.StatusNotFound)
}
