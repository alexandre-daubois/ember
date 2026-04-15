package fetcher

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// caddyDuration mirrors caddy.Duration: it marshals as int64 nanoseconds but can
// unmarshal from either an integer (nanoseconds) or a string ("5s"). Ember reads
// from Caddy's admin API which always emits integers today, but accepting strings
// keeps the parser resilient if Caddy ever changes its output, and plays well
// with hand-crafted config dumps.
type caddyDuration int64

func (d *caddyDuration) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = caddyDuration(parsed.Nanoseconds())
		return nil
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	*d = caddyDuration(n)
	return nil
}

// ParseReverseProxyConfigs extracts reverse proxy handler configurations from
// the raw Caddy JSON config (as returned by GET /config/). It descends through
// nested route groups (e.g. handlers of type "subroute" with their own routes)
// so reverse_proxy handlers buried inside typical Caddyfile-generated configs
// are discovered regardless of depth.
func ParseReverseProxyConfigs(raw json.RawMessage) []ReverseProxyConfig {
	var root struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Routes []routeNode `json:"routes"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}

	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}

	var configs []ReverseProxyConfig
	for _, srv := range root.Apps.HTTP.Servers {
		configs = appendRPFromRoutes(configs, srv.Routes)
	}
	return configs
}

type routeNode struct {
	Handle []json.RawMessage `json:"handle"`
}

// appendRPFromRoutes walks routes and their handlers, collecting reverse_proxy
// configs. Handlers that carry a nested "routes" field (subroute, route groups)
// are descended into recursively.
func appendRPFromRoutes(configs []ReverseProxyConfig, routes []routeNode) []ReverseProxyConfig {
	for _, route := range routes {
		for _, h := range route.Handle {
			configs = appendRPFromHandler(configs, h)
		}
	}
	return configs
}

func appendRPFromHandler(configs []ReverseProxyConfig, raw json.RawMessage) []ReverseProxyConfig {
	if rp, ok := parseRPHandler(raw); ok {
		configs = append(configs, rp)
	}

	var nested struct {
		Routes []routeNode `json:"routes"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil && len(nested.Routes) > 0 {
		configs = appendRPFromRoutes(configs, nested.Routes)
	}
	return configs
}

func parseRPHandler(raw json.RawMessage) (ReverseProxyConfig, bool) {
	var probe struct {
		Handler string `json:"handler"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Handler != "reverse_proxy" {
		return ReverseProxyConfig{}, false
	}

	var h struct {
		Handler       string `json:"handler"`
		LoadBalancing *struct {
			SelectionPolicy *struct {
				Policy string `json:"policy"`
			} `json:"selection_policy"`
		} `json:"load_balancing"`
		HealthChecks *struct {
			Active *struct {
				URI      string        `json:"uri"`
				Interval caddyDuration `json:"interval"`
			} `json:"active"`
		} `json:"health_checks"`
		Upstreams []struct {
			Dial        string `json:"dial"`
			MaxRequests int    `json:"max_requests"`
		} `json:"upstreams"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return ReverseProxyConfig{}, false
	}

	rp := ReverseProxyConfig{
		Handler: h.Handler,
	}
	if h.LoadBalancing != nil && h.LoadBalancing.SelectionPolicy != nil {
		rp.LBPolicy = h.LoadBalancing.SelectionPolicy.Policy
	}
	if h.HealthChecks != nil && h.HealthChecks.Active != nil {
		rp.HealthURI = h.HealthChecks.Active.URI
		if h.HealthChecks.Active.Interval > 0 {
			rp.HealthInterval = formatNanoDuration(int64(h.HealthChecks.Active.Interval))
		}
	}
	for _, u := range h.Upstreams {
		rp.Upstreams = append(rp.Upstreams, ReverseProxyUpstreamConfig{
			Address:     u.Dial,
			MaxRequests: u.MaxRequests,
		})
	}

	return rp, true
}

// formatNanoDuration renders a nanosecond duration in a human-friendly, precise form.
// Any sub-second remainder is expressed in ms so values like 1_500_000_000 ("1.5s")
// don't get truncated to "1s". Taking int64 rather than float64 avoids the mantissa
// precision loss that would otherwise show up on large intervals.
func formatNanoDuration(ns int64) string {
	d := time.Duration(ns)
	if d < time.Second || d%time.Second != 0 {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%ds", d/time.Second)
}
