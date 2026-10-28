package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// EmberLogSinkName is the well-known name Ember uses when it auto-registers
// a logging sink in Caddy. A fixed name lets re-registrations from a fresh
// Ember session overwrite a stale entry left over from a prior crash.
const EmberLogSinkName = "__ember__"

// emberRuntimeLogSinkName is the well-known name for the second sink Ember
// registers to capture Caddy's runtime logs (startup, reloads, TLS, admin
// API, plugins): anything that is not an access log.
const emberRuntimeLogSinkName = "__ember_runtime__"

// LogSinks names the access and runtime sinks one Ember registers.
type LogSinks struct{ Access, Runtime string }

// TUILogSinks are the sinks of a TUI. A daemon serving remote TUIs registers
// DaemonLogSinks, so that it and a TUI on the Caddy host neither block nor
// remove each other's sinks.
var (
	TUILogSinks    = LogSinks{Access: EmberLogSinkName, Runtime: emberRuntimeLogSinkName}
	DaemonLogSinks = LogSinks{Access: "__ember_remote__", Runtime: "__ember_remote_runtime__"}
)

// RegisterAccessLogSink hot-installs a Caddy logging sink that
// pushes JSON access logs to the given listener address. The sink uses
// soft_start so Caddy does not fail config load when the listener is briefly
// unavailable.
//
// The address must be reachable from Caddy's process; on the same host this
// is typically "127.0.0.1:<port>" but for remote Caddy instances you must
// supply a routable host:port pair.
func (f *HTTPFetcher) RegisterAccessLogSink(ctx context.Context, name, listenerAddr string) error {
	return f.registerLogSink(ctx, name, listenerAddr, map[string]any{
		"include": []string{"http.log.access"},
	})
}

// RegisterRuntimeLogSink hot-installs a second logging sink that pushes
// Caddy's runtime logs (everything that is not an access log) to the given
// listener address. Uses `exclude` so the sink is a catch-all minus access
// logs, which are handled by the primary __ember__ sink.
func (f *HTTPFetcher) RegisterRuntimeLogSink(ctx context.Context, name, listenerAddr string) error {
	return f.registerLogSink(ctx, name, listenerAddr, map[string]any{
		"exclude": []string{"http.log.access"},
	})
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

func (f *HTTPFetcher) registerLogSink(ctx context.Context, name, listenerAddr string, filter map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(logSinkPayload(listenerAddr, filter))
	if err != nil {
		return fmt.Errorf("marshal %s log sink payload: %w", name, err)
	}

	err = f.sendLogSink(ctx, http.MethodPut, name, body)
	if err == nil {
		return nil
	}

	// Caddy refuses a PUT over an existing key (409), and a POST replaces only
	// one a killed Ember left. A POST does not create the missing parents, which
	// a PUT does.
	var status statusError
	if errors.As(err, &status) && status.code == http.StatusConflict {
		inUse, err := f.LogSinkInUse(ctx, name, listenerAddr)
		if err != nil {
			return fmt.Errorf("register %s log sink: %w", name, err)
		}
		if inUse {
			return fmt.Errorf("register %s log sink: in use by another Ember", name)
		}
		if err := f.sendLogSink(ctx, http.MethodPost, name, body); err != nil {
			return fmt.Errorf("register %s log sink: %w", name, err)
		}
		return nil
	}

	// The PUT fails with 400 "invalid traversal path" when /config/logging/logs
	// does not exist yet (Caddyfile has no log directive). Bootstrap the path,
	// then retry. Only a rejection means that: a timeout or a 500 says nothing
	// about the config tree, and the bootstrap writes into the shared logging
	// section.
	if !errors.As(err, &status) || status.code >= http.StatusInternalServerError {
		return fmt.Errorf("register %s log sink: %w", name, err)
	}

	if err := f.ensureLoggingPath(ctx); err != nil {
		return fmt.Errorf("register %s log sink: %w", name, err)
	}
	return f.sendLogSink(ctx, http.MethodPut, name, body)
}

// LogSinkInUse reports whether the named sink points at a listener other than
// listenerAddr that accepts connections, as a running Ember's does. A sink a
// killed Ember left points at an address nothing accepts connections on.
func (f *HTTPFetcher) LogSinkInUse(ctx context.Context, name, listenerAddr string) (bool, error) {
	addr, err := f.LogSinkAddress(ctx, name)
	if err != nil {
		return false, err
	}
	if addr == "" || addr == listenerAddr {
		return false, nil
	}
	conn := dialSink(ctx, addr)
	if conn == nil {
		return false, nil
	}
	defer func() { _ = conn.Close() }()
	return !reachesOwnListener(conn, listenerAddr), nil
}

// dialSink dials addr from Ember's side. A host that does not resolve here,
// such as host.docker.internal, is one receiveCaddyLogs bound on all
// interfaces instead.
func dialSink(ctx context.Context, addr string) net.Conn {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if dnsErr := (*net.DNSError)(nil); errors.As(err, &dnsErr) {
		_, port, _ := net.SplitHostPort(addr)
		conn, err = d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	}
	if err != nil {
		return nil
	}
	return conn
}

// reachesOwnListener reports whether conn landed on this Ember's listener: a
// stale sink can name it under another spelling (host.docker.internal,
// 0.0.0.0, localhost), and a connection to this host has the same IP at both
// ends.
func reachesOwnListener(conn net.Conn, listenerAddr string) bool {
	_, port, err := net.SplitHostPort(listenerAddr)
	if err != nil {
		return false
	}
	remote, rok := conn.RemoteAddr().(*net.TCPAddr)
	local, lok := conn.LocalAddr().(*net.TCPAddr)
	return rok && lok && strconv.Itoa(remote.Port) == port && remote.IP.Equal(local.IP)
}

// statusError carries the status of an admin API answer so callers can tell a
// rejection apart from a server fault or a dead connection.
type statusError struct{ code int }

func (e statusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func (f *HTTPFetcher) sendLogSink(ctx context.Context, method, name string, body []byte) error {
	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
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
		return statusError{code: resp.StatusCode}
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

// LogSinkAddress returns the address the named sink in Caddy's configuration
// writes to, or "" when there is no such sink.
func (f *HTTPFetcher) LogSinkAddress(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", statusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || isEmptyOrNull(body) {
		return "", err
	}
	var sink struct {
		Writer struct {
			Address string `json:"address"`
		} `json:"writer"`
	}
	if err := json.Unmarshal(body, &sink); err != nil {
		return "", err
	}
	return strings.TrimPrefix(sink.Writer.Address, "tcp/"), nil
}

// UnregisterLogSink removes the named sink.
// It is safe to call when no sink exists.
func (f *HTTPFetcher) UnregisterLogSink(ctx context.Context, name string) error {
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
