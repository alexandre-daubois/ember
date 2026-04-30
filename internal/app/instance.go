package app

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/internal/model"
)

type addrSpec struct {
	name string
	url  string
}

type instance struct {
	name     string
	addr     string
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
	var name, url string
	hasAlias := aliasPrefixRe.MatchString(raw)
	if hasAlias {
		i := strings.Index(raw, "=")
		name = raw[:i]
		url = raw[i+1:]
	} else {
		url = raw
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
	return addrSpec{name: name, url: url}, nil
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
// HTTPFetcher with TLS uniformly applied, plus a recorder when --expose is set.
// In single-instance mode the lone instance's recorder is also stored on cfg
// so the TUI metrics handler keeps surfacing self-metrics through the legacy
// path that reads cfg.recorder.
func newInstances(ctx context.Context, cfg *config, version string) ([]*instance, error) {
	instances := make([]*instance, 0, len(cfg.addrs))
	multi := len(cfg.addrs) >= 2
	for _, spec := range cfg.addrs {
		var pid int32
		if !multi {
			pid = resolveFrankenPHPPID(ctx, cfg)
		}

		f := fetcher.NewHTTPFetcher(spec.url, pid)
		if err := configureTLS(f, cfg); err != nil {
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
			fetcher:  f,
			recorder: rec,
		})
	}
	if !multi && len(instances) > 0 {
		cfg.recorder = instances[0].recorder
	}
	return instances, nil
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
