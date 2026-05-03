package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// EnableServerAccessLogs activates Caddy's HTTP access logs on the given
// server when none are configured yet. It is a no-op (returns false, nil)
// when the server already has its own logs block, so a user's existing
// configuration is never overwritten.
//
// Returns enabled=true only when Ember actually flipped the switch; pass that
// value to RestoreServerAccessLogs at shutdown to undo only what we changed.
func (f *HTTPFetcher) EnableServerAccessLogs(ctx context.Context, serverName string) (enabled bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.serverLogsEndpoint(serverName)

	existing, err := f.getServerLogs(ctx, endpoint)
	if err != nil {
		return false, err
	}
	if !isEmptyOrNull(existing) {
		// User-defined logs config: leave it alone.
		return false, nil
	}

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return false, err
	}
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := f.httpClient.Do(postReq)
	if err != nil {
		return false, fmt.Errorf("enable server logs: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, postResp.Body)
		_ = postResp.Body.Close()
	}()
	if postResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("enable server logs: HTTP %d", postResp.StatusCode)
	}
	return true, nil
}

// RestoreServerAccessLogs removes the access-logs block that
// EnableServerAccessLogs added. We parse the current value as JSON (rather
// than comparing raw bytes) so whitespace or Caddy-side canonicalization of
// the empty object we posted does not leave our config behind.
//
// If the current value is not an empty object, a user or another tool
// modified the server's logs config in the meantime: we leave it alone.
func (f *HTTPFetcher) RestoreServerAccessLogs(ctx context.Context, serverName string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.serverLogsEndpoint(serverName)

	existing, err := f.getServerLogs(ctx, endpoint)
	if err != nil {
		return err
	}
	// Only DELETE when the current value is an empty object -- the shape we
	// installed. If fields have appeared (user config) or the value is null
	// (already gone), skip.
	if !isEmptyObject(existing) {
		return nil
	}

	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	delResp, err := f.httpClient.Do(delReq)
	if err != nil {
		return fmt.Errorf("restore server logs: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, delResp.Body)
		_ = delResp.Body.Close()
	}()
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("restore server logs: HTTP %d", delResp.StatusCode)
	}
	return nil
}

// serverLogsEndpoint returns the admin API URL for a server's logs config,
// URL-escaping the server name so unusual but valid names (containing
// spaces, slashes or other special characters) cannot break the path.
func (f *HTTPFetcher) serverLogsEndpoint(serverName string) string {
	return f.baseURL + "/config/apps/http/servers/" + url.PathEscape(serverName) + "/logs"
}

// getServerLogs retrieves the current /logs config for a server as raw bytes.
// Returns a non-nil error for any non-200 response so callers never silently
// fall through to a write on a broken admin API.
func (f *HTTPFetcher) getServerLogs(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get server logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("get server logs: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// isEmptyOrNull reports whether body represents a missing or empty logs
// config -- either "null", "", or an object with no fields.
func isEmptyOrNull(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	return isEmptyObjectTrimmed(trimmed)
}

// isEmptyObject reports whether body parses as a JSON object with zero
// fields. It deliberately rejects "null", arrays and scalars, which all
// unmarshal successfully into a nil map but are not what we installed.
func isEmptyObject(body []byte) bool {
	return isEmptyObjectTrimmed(bytes.TrimSpace(body))
}

func isEmptyObjectTrimmed(trimmed []byte) bool {
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return false
	}
	return len(m) == 0
}
