package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
)

// EmberLogSinkName is the well-known name Ember uses when it auto-registers
// a logging sink in Caddy. A fixed name lets re-registrations from a fresh
// Ember session overwrite a stale entry left over from a prior crash.
const EmberLogSinkName = "__ember__"

// emberRuntimeLogSinkName is the well-known name for the second sink Ember
// registers to capture Caddy's runtime logs (startup, reloads, TLS, admin
// API, plugins): anything that is not an access log.
const emberRuntimeLogSinkName = "__ember_runtime__"

// RegisterEmberLogSink hot-installs a Caddy logging sink that
// pushes JSON access logs to the given listener address. The sink uses
// soft_start so Caddy does not fail config load when the listener is briefly
// unavailable.
//
// The address must be reachable from Caddy's process; on the same host this
// is typically "127.0.0.1:<port>" but for remote Caddy instances you must
// supply a routable host:port pair.
func (f *HTTPFetcher) RegisterEmberLogSink(ctx context.Context, listenerAddr string) error {
	return f.registerLogSink(ctx, EmberLogSinkName, logSinkPayload(listenerAddr, map[string]any{
		"include": []string{"http.log.access"},
	}))
}

// RegisterEmberRuntimeLogSink hot-installs a second logging sink that pushes
// Caddy's runtime logs (everything that is not an access log) to the given
// listener address. Uses `exclude` so the sink is a catch-all minus access
// logs, which are handled by the primary __ember__ sink.
func (f *HTTPFetcher) RegisterEmberRuntimeLogSink(ctx context.Context, listenerAddr string) error {
	return f.registerLogSink(ctx, emberRuntimeLogSinkName, logSinkPayload(listenerAddr, map[string]any{
		"exclude": []string{"http.log.access"},
	}))
}

// logSinkPayload builds the JSON body for a Caddy sink definition. Using
// json.Marshal for the address ensures user-supplied values (via --log-listen)
// cannot inject extra fields through string interpolation.
func logSinkPayload(listenerAddr string, extra map[string]any) map[string]any {
	p := map[string]any{
		"writer": map[string]any{
			"output":     "net",
			"address":    "tcp/" + listenerAddr,
			"soft_start": true,
		},
		"encoder": map[string]any{"format": "json"},
	}
	maps.Copy(p, extra)
	return p
}

func (f *HTTPFetcher) registerLogSink(ctx context.Context, name string, payload map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s log sink payload: %w", name, err)
	}

	if err := f.putLogSink(ctx, name, body); err == nil {
		return nil
	}

	// The PUT fails when /config/logging/logs does not exist yet (Caddyfile
	// has no log directive). Bootstrap the path, then retry.
	if err := f.ensureLoggingPath(ctx); err != nil {
		return fmt.Errorf("register %s log sink: %w", name, err)
	}
	return f.putLogSink(ctx, name, body)
}

func (f *HTTPFetcher) putLogSink(ctx context.Context, name string, body []byte) error {
	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ensureLoggingPath creates /config/logging/logs in Caddy's config when it
// does not exist yet. A Caddyfile with no log directive produces a JSON config
// that omits the logging section entirely; Caddy's admin API refuses to PUT
// at a deep path whose parents are missing.
func (f *HTTPFetcher) ensureLoggingPath(ctx context.Context) error {
	// Try creating just the logs key (works when /config/logging exists).
	if err := f.putJSON(ctx, f.baseURL+"/config/logging/logs", []byte("{}")); err == nil {
		return nil
	}
	// /config/logging itself is missing. Create it with an empty logs map.
	body, _ := json.Marshal(map[string]any{"logs": map[string]any{}})
	return f.putJSON(ctx, f.baseURL+"/config/logging", body)
}

func (f *HTTPFetcher) putJSON(ctx context.Context, endpoint string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// CheckEmberLogSink reports whether the Ember access log sink is currently
// registered in Caddy's configuration. Returns false on any error.
func (f *HTTPFetcher) CheckEmberLogSink(ctx context.Context) bool {
	return f.checkLogSink(ctx, EmberLogSinkName)
}

// CheckEmberRuntimeLogSink is the runtime-sink counterpart of CheckEmberLogSink.
func (f *HTTPFetcher) CheckEmberRuntimeLogSink(ctx context.Context) bool {
	return f.checkLogSink(ctx, emberRuntimeLogSinkName)
}

func (f *HTTPFetcher) checkLogSink(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	return !isEmptyOrNull(body)
}

// UnregisterEmberLogSink removes the sink installed by RegisterEmberLogSink.
// It is safe to call when no sink exists.
func (f *HTTPFetcher) UnregisterEmberLogSink(ctx context.Context) error {
	return f.unregisterLogSink(ctx, EmberLogSinkName)
}

// UnregisterEmberRuntimeLogSink is the runtime-sink counterpart.
func (f *HTTPFetcher) UnregisterEmberRuntimeLogSink(ctx context.Context) error {
	return f.unregisterLogSink(ctx, emberRuntimeLogSinkName)
}

func (f *HTTPFetcher) unregisterLogSink(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unregister %s log sink: %w", name, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	// 200 = removed, 404 = nothing to remove, both fine.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unregister %s log sink: HTTP %d", name, resp.StatusCode)
	}
	return nil
}
