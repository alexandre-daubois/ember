package fetcher

import (
	"encoding/json"
	"net/url"
)

// MercureConfig is one mercure handler from the Caddy JSON config. Durations
// are empty when the hub runs on its defaults.
type MercureConfig struct {
	Name            string
	Transport       string
	Anonymous       bool
	Subscriptions   bool
	Debugger        bool
	Playground      bool
	Heartbeat       string
	WriteTimeout    string
	DispatchTimeout string
	Issuers         []string
	CORSOrigins     []string
	PublishOrigins  []string
	PublicURLs      []string
	ProtocolCompat  int
}

// ParseMercureConfigs extracts every mercure handler from the raw Caddy JSON
// config (as returned by GET /config/).
func ParseMercureConfigs(raw json.RawMessage) []MercureConfig {
	var configs []MercureConfig
	forEachHandler(raw, func(h json.RawMessage) {
		if mc, ok := parseMercureHandler(h); ok {
			configs = append(configs, mc)
		}
	})
	return configs
}

func parseMercureHandler(raw json.RawMessage) (MercureConfig, bool) {
	var h struct {
		Handler         string         `json:"handler"`
		Name            string         `json:"name"`
		Anonymous       bool           `json:"anonymous"`
		Subscriptions   bool           `json:"subscriptions"`
		Debugger        bool           `json:"debugger"`
		Playground      bool           `json:"playground"`
		WriteTimeout    *caddyDuration `json:"write_timeout"`
		DispatchTimeout *caddyDuration `json:"dispatch_timeout"`
		Heartbeat       *caddyDuration `json:"heartbeat"`
		Issuers         []struct {
			Identifier string `json:"identifier"`
		} `json:"issuers"`
		PublisherJWT struct {
			Alg string `json:"alg"`
		} `json:"publisher_jwt"`
		SubscriberJWT struct {
			Alg string `json:"alg"`
		} `json:"subscriber_jwt"`
		PublisherJWKSURL  string   `json:"publisher_jwks_url"`
		SubscriberJWKSURL string   `json:"subscriber_jwks_url"`
		PublishOrigins    []string `json:"publish_origins"`
		CORSOrigins       []string `json:"cors_origins"`
		PublicURLs        []string `json:"public_urls"`
		ProtocolCompat    int      `json:"protocol_version_compatibility"`
		TransportURL      string   `json:"transport_url"`
		Transport         struct {
			Name string `json:"name"`
		} `json:"transport"`
	}
	if err := json.Unmarshal(raw, &h); err != nil || h.Handler != "mercure" {
		return MercureConfig{}, false
	}

	mc := MercureConfig{
		Name:           h.Name,
		Transport:      mercureTransportName(h.Transport.Name, h.TransportURL),
		Anonymous:      h.Anonymous,
		Subscriptions:  h.Subscriptions,
		Debugger:       h.Debugger || h.Playground,
		Playground:     h.Playground,
		CORSOrigins:    h.CORSOrigins,
		PublishOrigins: h.PublishOrigins,
		PublicURLs:     h.PublicURLs,
		ProtocolCompat: h.ProtocolCompat,
	}
	if h.Heartbeat != nil {
		mc.Heartbeat = formatNanoDuration(int64(*h.Heartbeat))
	}
	if h.WriteTimeout != nil {
		mc.WriteTimeout = formatNanoDuration(int64(*h.WriteTimeout))
	}
	if h.DispatchTimeout != nil {
		mc.DispatchTimeout = formatNanoDuration(int64(*h.DispatchTimeout))
	}
	for _, iss := range h.Issuers {
		mc.Issuers = append(mc.Issuers, iss.Identifier)
	}
	// The keys are secrets; only the algorithms are shown.
	if h.PublisherJWT.Alg != "" || h.SubscriberJWT.Alg != "" || h.PublisherJWKSURL != "" || h.SubscriberJWKSURL != "" {
		legacy := "legacy"
		if alg := h.PublisherJWT.Alg; alg != "" {
			legacy += " publisher " + alg
		} else if h.PublisherJWKSURL != "" {
			legacy += " publisher JWKS"
		}
		if alg := h.SubscriberJWT.Alg; alg != "" {
			legacy += " subscriber " + alg
		} else if h.SubscriberJWKSURL != "" {
			legacy += " subscriber JWKS"
		}
		mc.Issuers = append(mc.Issuers, legacy)
	}
	return mc, true
}

// The module loads bolt when no transport is configured.
func mercureTransportName(name, transportURL string) string {
	if name != "" {
		return name
	}
	if transportURL != "" {
		if u, err := url.Parse(transportURL); err == nil && u.Scheme != "" {
			return u.Scheme
		}
		return transportURL
	}
	return "bolt"
}
