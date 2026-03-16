package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func runTUI(f *fetcher.HTTPFetcher, cfg *config, hasFrankenPHP bool, version string) error {
	uiCfg := ui.Config{
		Interval:      cfg.interval,
		SlowThreshold: time.Duration(cfg.slowThreshold) * time.Millisecond,
		NoColor:       cfg.noColor,
		Version:       version,
		HasFrankenPHP: hasFrankenPHP,
	}

	var srv *http.Server
	if cfg.expose != "" {
		holder := &exporter.StateHolder{}
		uiCfg.OnStateUpdate = func(s model.State) {
			holder.Store(s.CopyForExport())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", exporter.Handler(holder))
		srv = &http.Server{Addr: cfg.expose, Handler: mux}

		listenErr := make(chan error, 1)
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				listenErr <- fmt.Errorf("metrics server on %s: %w", cfg.expose, err)
			}
		}()

		select {
		case err := <-listenErr:
			return err
		case <-time.After(50 * time.Millisecond):
		}

		uiCfg.MetricsServerErr = listenErr
	}

	app := ui.NewApp(f, uiCfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}

	if srv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	return nil
}
