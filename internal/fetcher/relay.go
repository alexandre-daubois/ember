package fetcher

import (
	"io"
	"net/http"
	"net/url"
	"slices"
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

func (f *HTTPFetcher) Relay() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := tuiRead(r.URL)
		switch {
		case r.Method != http.MethodGet:
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "a remote session is read-only", http.StatusMethodNotAllowed)
			return
		case !ok:
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
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}
