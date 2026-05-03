package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FetchConfig returns the full Caddy configuration as raw JSON.
func (f *HTTPFetcher) FetchConfig(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config: HTTP %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	return raw, nil
}

// CheckAdminAPI returns nil if the Caddy admin API is reachable.
func (f *HTTPFetcher) CheckAdminAPI(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/", nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin API unreachable: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

// CheckMetricsEnabled returns true if the HTTP metrics directive is configured.
func (f *HTTPFetcher) CheckMetricsEnabled(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/apps/http/metrics", nil)
	if err != nil {
		return false, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	// /config/apps/http/metrics returns "null" when not set, or a JSON object when set
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return false, nil
	}
	return string(raw) != "null", nil
}

// EnableMetrics activates the HTTP metrics directive via the admin API.
func (f *HTTPFetcher) EnableMetrics(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/config/apps/http/metrics", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enable metrics: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enable metrics: HTTP %d", resp.StatusCode)
	}
	return nil
}
