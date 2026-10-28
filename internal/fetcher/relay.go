package fetcher

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var tuiReads = []string{
	"/metrics",
	"/frankenphp/threads",
	"/config/",
	"/config/apps/http/servers",
	"/config/apps/pki/certificate_authorities",
}

func tuiRead(u *url.URL) (string, bool) {
	if id, ok := strings.CutPrefix(u.Path, "/pki/ca/"); ok {
		return "/pki/ca/" + url.PathEscape(id), id != "" && id != "." && id != ".." && !strings.Contains(id, "/")
	}
	return u.Path, slices.Contains(tuiReads, u.Path)
}

func acceptsGzip(header string) bool {
	for coding := range strings.SplitSeq(header, ",") {
		name, weight, _ := strings.Cut(coding, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q, ok := strings.CutPrefix(strings.TrimSpace(weight), "q=")
		if !ok {
			return true
		}
		v, err := strconv.ParseFloat(q, 64)
		return err == nil && v > 0
	}
	return false
}

// RemoteTrace is what a route serving remote TUIs reports of a request, for the daemon to log.
type RemoteTrace struct {
	// Refused is the status of a refusal by the daemon, 0 for an answer.
	Refused       int
	SessionOpened bool
	ConfigRead    bool
}

type remoteTraceKey struct{}

func WithRemoteTrace(ctx context.Context, t *RemoteTrace) context.Context {
	return context.WithValue(ctx, remoteTraceKey{}, t)
}

// RemoteTraceOf returns the trace of ctx, or one that nobody reads.
func RemoteTraceOf(ctx context.Context) *RemoteTrace {
	if t, ok := ctx.Value(remoteTraceKey{}).(*RemoteTrace); ok {
		return t
	}
	return &RemoteTrace{}
}

func (f *HTTPFetcher) Relay() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := RemoteTraceOf(r.Context())
		path, ok := tuiRead(r.URL)
		switch {
		case r.Method != http.MethodGet:
			trace.Refused = http.StatusMethodNotAllowed
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "a remote session is read-only", http.StatusMethodNotAllowed)
			return
		case !ok:
			trace.Refused = http.StatusForbidden
			http.Error(w, "not a request the TUI makes", http.StatusForbidden)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, f.baseURL+path, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp, err := f.transport.RoundTrip(req)
		if err != nil {
			http.Error(w, "Caddy's admin API is unreachable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// The Config tab reads the whole config, and so does the Upstreams tab as soon as Caddy reports an upstream. The narrower reads are the TUI's periodic refreshes.
		trace.ConfigRead = resp.StatusCode == http.StatusOK && path == "/config/"
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		var body io.Writer = w
		if acceptsGzip(r.Header.Get("Accept-Encoding")) {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer func() { _ = gz.Close() }()
			body = gz
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(body, resp.Body)
	})
}
