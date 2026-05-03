package app

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/internal/model"
)

type addrSpec struct {
	name     string
	url      string
	tls      addrTLS
	interval time.Duration
}

// addrTLS holds the per-instance TLS knobs parsed from --addr suffixes.
// insecureSet distinguishes ",insecure=false" (force off, overrides a
// global --insecure) from omission (fall back to global).
type addrTLS struct {
	caCert      string
	clientCert  string
	clientKey   string
	insecure    bool
	insecureSet bool
}

func (t addrTLS) any() bool {
	return t.caCert != "" || t.clientCert != "" || t.clientKey != "" || t.insecureSet
}

type instance struct {
	name     string
	addr     string
	tls      fetcher.TLSOptions
	interval time.Duration
	fetcher  *fetcher.HTTPFetcher
	state    model.State
	throttle errorThrottle
	recorder *instrumentation.Recorder
}

var (
	instanceNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	aliasPrefixRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*=`)
)

// parseAddrs converts raw --addr inputs into addrSpec entries with validated
// URLs. Instance names only need to satisfy Prometheus label rules when more
// than one address is supplied, since N=1 emits no ember_instance label.
func parseAddrs(raws []string) ([]addrSpec, error) {
	if len(raws) == 0 {
		return nil, fmt.Errorf("--addr is required")
	}
	multi := len(raws) >= 2
	out := make([]addrSpec, 0, len(raws))
	seen := make(map[string]string, len(raws))
	for _, raw := range raws {
		if raw == "" {
			return nil, fmt.Errorf("--addr cannot be empty")
		}
		spec, err := parseOneAddr(raw, multi)
		if err != nil {
			return nil, err
		}
		if multi {
			if existing, ok := seen[spec.name]; ok {
				return nil, fmt.Errorf("duplicate instance name %q (from %q and %q); use explicit aliases like name=url", spec.name, existing, raw)
			}
			seen[spec.name] = raw
		}
		out = append(out, spec)
	}
	return out, nil
}

func parseOneAddr(raw string, validateName bool) (addrSpec, error) {
	parts := strings.Split(raw, ",")
	head, suffixes := parts[0], parts[1:]

	var name, url string
	hasAlias := aliasPrefixRe.MatchString(head)
	if hasAlias {
		name, url, _ = strings.Cut(head, "=")
	} else {
		url = head
		name = deriveInstanceName(url)
	}

	if url == "" {
		return addrSpec{}, fmt.Errorf("--addr %q has empty URL", raw)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !fetcher.IsUnixAddr(url) {
		return addrSpec{}, fmt.Errorf("--addr must start with http://, https://, or unix// (got %q)", url)
	}
	if fetcher.IsUnixAddr(url) {
		if _, ok := fetcher.ParseUnixAddr(url); !ok {
			return addrSpec{}, fmt.Errorf("--addr must include a non-empty Unix socket path (got %q)", url)
		}
	}
	if validateName && !instanceNameRe.MatchString(name) {
		if hasAlias {
			return addrSpec{}, fmt.Errorf("alias name %q must match [a-zA-Z_][a-zA-Z0-9_]* (Prometheus label rules; underscores only, no hyphens or dots)", name)
		}
		return addrSpec{}, fmt.Errorf("instance name %q derived from %q must match [a-zA-Z_][a-zA-Z0-9_]* (use alias=url to set explicitly)", name, raw)
	}

	tls, interval, err := parseAddrSuffixes(suffixes)
	if err != nil {
		return addrSpec{}, fmt.Errorf("--addr %q: %w", raw, err)
	}
	if fetcher.IsUnixAddr(url) && tls.any() {
		return addrSpec{}, fmt.Errorf("--addr %q: TLS options cannot be used with Unix socket addresses", raw)
	}
	return addrSpec{name: name, url: url, tls: tls, interval: interval}, nil
}

func parseAddrSuffixes(suffixes []string) (addrTLS, time.Duration, error) {
	var (
		tls      addrTLS
		interval time.Duration
	)
	paths := map[string]*string{"ca": &tls.caCert, "cert": &tls.clientCert, "key": &tls.clientKey}
	for _, s := range suffixes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key, value, hasValue := strings.Cut(s, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		if target, ok := paths[key]; ok {
			if !hasValue || value == "" {
				return addrTLS{}, 0, fmt.Errorf("suffix %q requires a non-empty path", key)
			}
			*target = value
			continue
		}
		if key == "insecure" {
			tls.insecureSet = true
			if !hasValue {
				tls.insecure = true
				continue
			}
			b, err := strconv.ParseBool(value)
			if err != nil {
				return addrTLS{}, 0, fmt.Errorf("suffix %q expects true|false (got %q)", key, value)
			}
			tls.insecure = b
			continue
		}
		if key == "interval" {
			if !hasValue || value == "" {
				return addrTLS{}, 0, fmt.Errorf("suffix %q requires a duration (e.g. 2s, 500ms)", key)
			}
			d, err := time.ParseDuration(value)
			if err != nil {
				return addrTLS{}, 0, fmt.Errorf("suffix %q expects a Go duration (got %q): %w", key, value, err)
			}
			if d < minInterval {
				return addrTLS{}, 0, fmt.Errorf("suffix %q must be at least %s (got %s)", key, minInterval, d)
			}
			interval = d
			continue
		}
		return addrTLS{}, 0, fmt.Errorf("unknown suffix %q (allowed: ca, cert, key, insecure, interval)", key)
	}
	if (tls.clientCert != "") != (tls.clientKey != "") {
		return addrTLS{}, 0, fmt.Errorf("suffixes cert= and key= must be set together")
	}
	return tls, interval, nil
}

func deriveInstanceName(url string) string {
	if path, ok := fetcher.ParseUnixAddr(url); ok {
		return slugifyHost(filepath.Base(path))
	}
	host := strings.TrimPrefix(url, "https://")
	host = strings.TrimPrefix(host, "http://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	return slugifyHost(host)
}

func slugifyHost(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == '.', r == '-', r == ':':
			b.WriteByte('_')
		}
	}
	return b.String()
}

// newInstances builds runtime instances ready to poll: each gets its own
// HTTPFetcher with effective TLS options applied (per-instance suffixes
// override the global flags field by field), plus a recorder when --expose
// is set. In single-instance mode the lone instance's recorder is also
// stored on cfg so the TUI metrics handler keeps surfacing self-metrics
// through the legacy path that reads cfg.recorder.
func newInstances(ctx context.Context, cfg *config, version string) ([]*instance, error) {
	instances := make([]*instance, 0, len(cfg.addrs))
	multi := len(cfg.addrs) >= 2
	for _, spec := range cfg.addrs {
		var pid int32
		switch {
		case !multi:
			pid = resolveFrankenPHPPID(ctx, cfg)
		case fetcher.IsLocalAddr(spec.url):
			pid = resolveLocalListenerPID(ctx, cfg, spec)
		}

		f := fetcher.NewHTTPFetcher(spec.url, pid)
		opts := effectiveTLS(spec, cfg)
		if err := configureTLS(f, opts); err != nil {
			return nil, err
		}

		var rec *instrumentation.Recorder
		if cfg.expose != "" {
			rec = instrumentation.New(version)
			f.SetRecorder(rec)
		}

		instances = append(instances, &instance{
			name:     spec.name,
			addr:     spec.url,
			tls:      opts,
			interval: effectiveInterval(spec, cfg),
			fetcher:  f,
			recorder: rec,
		})
	}
	if !multi && len(instances) > 0 {
		cfg.recorder = instances[0].recorder
	}
	return instances, nil
}

func effectiveInterval(spec addrSpec, cfg *config) time.Duration {
	if spec.interval > 0 {
		return spec.interval
	}
	return cfg.interval
}

func effectiveTLS(spec addrSpec, cfg *config) fetcher.TLSOptions {
	opts := fetcher.TLSOptions{
		CACert:     cfg.caCert,
		ClientCert: cfg.clientCert,
		ClientKey:  cfg.clientKey,
		Insecure:   cfg.insecure,
	}
	if spec.tls.caCert != "" {
		opts.CACert = spec.tls.caCert
	}
	if spec.tls.clientCert != "" {
		opts.ClientCert = spec.tls.clientCert
	}
	if spec.tls.clientKey != "" {
		opts.ClientKey = spec.tls.clientKey
	}
	if spec.tls.insecureSet {
		opts.Insecure = spec.tls.insecure
	}
	return opts
}

func resolveFrankenPHPPID(ctx context.Context, cfg *config) int32 {
	if cfg.frankenphpPID != 0 {
		return int32(cfg.frankenphpPID)
	}
	pid, err := fetcher.FindFrankenPHPProcess(ctx)
	if err != nil {
		pid, err = fetcher.FindCaddyProcess(ctx)
		if err != nil && (cfg.jsonMode || cfg.daemon) {
			cfg.logger.Warn("no frankenphp or caddy process found")
		}
	}
	return pid
}

// resolveLocalListenerPID maps a local --addr to the PID of the process
// bound to its admin endpoint. Failures are silent: callers fall back to the
// process_* metrics already exposed by Caddy.
func resolveLocalListenerPID(ctx context.Context, cfg *config, spec addrSpec) int32 {
	ll, err := fetcher.FindLocalListener(ctx, spec.url)
	if err != nil {
		return 0
	}
	if ll.Ambiguous {
		cfg.logger.Debug("multiple processes listen on instance admin endpoint; using first match",
			"instance", spec.name, "addr", spec.url, "pid", ll.PID)
	}
	return ll.PID
}
