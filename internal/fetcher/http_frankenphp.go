package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// FrankenPHPConfig holds the FrankenPHP app configuration from Caddy.
type FrankenPHPConfig struct {
	NumThreads int                      `json:"num_threads"`
	Workers    []FrankenPHPWorkerConfig `json:"workers"`
}

// FrankenPHPWorkerConfig holds a single worker definition.
type FrankenPHPWorkerConfig struct {
	FileName string `json:"file_name"`
	Name     string `json:"name"`
	Num      int    `json:"num"`
}

// DetectFrankenPHP probes the /frankenphp/threads endpoint and updates the
// internal flag. It returns true when FrankenPHP is available.
func (f *HTTPFetcher) DetectFrankenPHP(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/frankenphp/threads", nil)
	if err != nil {
		return false
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	result := resp.StatusCode == http.StatusOK
	f.mu.Lock()
	f.hasFrankenPHP = result
	f.mu.Unlock()
	return result
}

// HasFrankenPHP reports whether FrankenPHP was detected on the last check.
func (f *HTTPFetcher) HasFrankenPHP() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasFrankenPHP
}

// FetchFrankenPHPConfig reads the FrankenPHP app config from the admin API.
func (f *HTTPFetcher) FetchFrankenPHPConfig(ctx context.Context) (*FrankenPHPConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/apps/frankenphp", nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var cfg FrankenPHPConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// RestartWorkers sends a POST to the FrankenPHP worker restart endpoint.
func (f *HTTPFetcher) RestartWorkers(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/frankenphp/workers/restart", nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("restart workers: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("restart workers: HTTP %d", resp.StatusCode)
	}
	return nil
}
