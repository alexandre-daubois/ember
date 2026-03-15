package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/alexandredaubois/ember/internal/exporter"
	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/alexandredaubois/ember/internal/ui"
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

		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "metrics server error: %v\n", err)
			}
		}()
	}

	app := ui.NewApp(f, uiCfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}

	if srv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}

	return nil
}
