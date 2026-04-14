package app

import (
	"net"
	"strings"
	"testing"
)

// FuzzIsLocalAdminAddr exercises the URL-ish parser that decides whether
// Ember can auto-bind a loopback log listener. Callers pass the raw --addr
// value which may include scheme, port, path, IPv6 brackets or garbage.
// The function must never panic on any input, and when it returns true for
// a clean "scheme://host[:port]" shape the extracted authority must match
// one of the known loopback hosts, otherwise we'd silently hand Caddy a
// 127.0.0.1 listener address it cannot reach.
func FuzzIsLocalAdminAddr(f *testing.F) {
	f.Add("http://localhost:2019")
	f.Add("http://127.0.0.1:2019")
	f.Add("https://localhost:2019")
	f.Add("http://[::1]:2019")
	f.Add("http://[::1]/some/path")
	f.Add("http://localhost")
	f.Add("http://localhost/")
	f.Add("unix//tmp/caddy.sock")
	f.Add("unix:///tmp/caddy.sock")

	f.Add("http://prod.example.com:2019")
	f.Add("https://10.0.0.5:2019")
	f.Add("http://caddy:2019")
	f.Add("http://[2001:db8::1]:8080")
	f.Add("http://[2001:db8::1]")

	f.Add("")
	f.Add(":")
	f.Add("://")
	f.Add("http://")
	f.Add("http://[")
	f.Add("http://]")
	f.Add("http://[not-closed")
	f.Add("not-a-url")
	f.Add(strings.Repeat("a", 1024))
	f.Add("http://localhost:99999")
	f.Add("http://127.0.0.1:abc")
	f.Add("http:///leading-slash")
	f.Add("/10.0.0.5")
	f.Add(":10.0.0.5")

	localHosts := map[string]struct{}{
		"":          {}, // ":port" has an empty host; canonically localhost
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
	}

	f.Fuzz(func(t *testing.T, addr string) {
		// Panic safety is the primary goal.
		got := isLocalAdminAddr(addr)
		if !got {
			return
		}

		// Only assert when we can cheaply extract an unambiguous authority.
		// For anything fancier (path, query, userinfo), defer to the classic
		// table-driven test rather than try to reimplement URL parsing here.
		rest := addr
		for _, prefix := range []string{"http://", "https://"} {
			if r, ok := strings.CutPrefix(rest, prefix); ok {
				rest = r
				break
			}
		}
		// Bail on anything that is not a plain "host[:port]" shape.
		if strings.ContainsAny(rest, "/?#@") {
			return
		}

		// Strip port (rightmost colon outside an IPv6 literal). Reuse Go's
		// own parser for correctness on bracketed IPv6.
		host := rest
		if h, _, err := net.SplitHostPort(rest); err == nil {
			host = h
		} else {
			host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		}

		if _, ok := localHosts[strings.ToLower(host)]; !ok {
			t.Fatalf("isLocalAdminAddr(%q) returned true but authority %q is not a loopback", addr, host)
		}
	})
}
