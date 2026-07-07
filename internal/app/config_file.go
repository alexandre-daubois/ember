package app

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// fileConfig mirrors the .ember.toml schema. The top-level TLS and interval
// keys are global fallbacks; the same keys inside an endpoint override them per
// instance, mirroring --ca-cert vs the ,ca= suffix today. Pointers and empty
// strings mark a key as absent so the file never clobbers a flag the user set
// explicitly.
type fileConfig struct {
	Default   string         `toml:"default"`
	CACert    string         `toml:"ca_cert"`
	Cert      string         `toml:"cert"`
	Key       string         `toml:"key"`
	Insecure  *bool          `toml:"insecure"`
	Interval  string         `toml:"interval"`
	Endpoints []fileEndpoint `toml:"endpoints"`
}

type fileEndpoint struct {
	Name     string `toml:"name"`
	Addr     string `toml:"addr"`
	CACert   string `toml:"ca_cert"`
	Cert     string `toml:"cert"`
	Key      string `toml:"key"`
	Insecure *bool  `toml:"insecure"`
	Interval string `toml:"interval"`
}

func parseConfigFile(path string) (*fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	md, err := toml.Decode(string(data), &fc)
	if err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	// Surface unknown keys (e.g. the singular [[endpoint]] or a misspelt
	// "intervall") as a hard error instead of silently ignoring them and
	// falling back to the default endpoint.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("config file %s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	return &fc, nil
}

// loadConfigFile resolves the fleet from .ember.toml when no higher-precedence
// source won. It is a no-op for commands that do not dial Caddy (so a malformed
// file never breaks `diff` or `version`) and when --addr/EMBER_ADDR is set (the
// file is then ignored entirely). A missing default file falls back silently;
// an explicit -f/EMBER_CONFIG that is missing, or any malformed file, is a hard
// error.
func loadConfigFile(cmd *cobra.Command, cfg *config) error {
	if !commandConsumesAddrs(cmd) || cmd.Flag("addr").Changed {
		return nil
	}

	fc, err := parseConfigFile(cfg.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !cmd.Flag("config").Changed {
			return nil
		}
		return err
	}

	if err := applyFileGlobals(cmd, cfg, fc); err != nil {
		return err
	}
	return applyFileEndpoints(cfg, fc)
}

// commandConsumesAddrs reports whether cmd dials Caddy instances and therefore
// reads the config file. The root (TUI/daemon/json) and the status/wait/init
// subcommands do; diff, version, config and completion do not.
func commandConsumesAddrs(cmd *cobra.Command) bool {
	if cmd.Parent() == nil {
		return true
	}
	switch cmd.Name() {
	case "status", "wait", "init":
		return true
	default:
		return false
	}
}

func applyFileGlobals(cmd *cobra.Command, cfg *config, fc *fileConfig) error {
	if fc.CACert != "" && !cmd.Flag("ca-cert").Changed {
		cfg.caCert = fc.CACert
	}
	if fc.Cert != "" && !cmd.Flag("client-cert").Changed {
		cfg.clientCert = fc.Cert
	}
	if fc.Key != "" && !cmd.Flag("client-key").Changed {
		cfg.clientKey = fc.Key
	}
	if fc.Insecure != nil && !cmd.Flag("insecure").Changed {
		cfg.insecure = *fc.Insecure
	}
	if fc.Interval != "" && !cmd.Flag("interval").Changed {
		d, err := time.ParseDuration(fc.Interval)
		if err != nil {
			return fmt.Errorf("config file: interval %q: %w", fc.Interval, err)
		}
		cfg.interval = d
	}
	return nil
}

// applyFileEndpoints rewrites the raw --addr list from the file's endpoints,
// each rendered in the name=url,suffix syntax parseAddrs already handles. Zero
// endpoints leaves the built-in default untouched.
func applyFileEndpoints(cfg *config, fc *fileConfig) error {
	if len(fc.Endpoints) == 0 {
		return nil
	}
	raws := make([]string, len(fc.Endpoints))
	seen := make(map[string]int, len(fc.Endpoints))
	for i, e := range fc.Endpoints {
		if err := e.validate(i); err != nil {
			return err
		}
		// Catch duplicates here so the error speaks in TOML terms rather than
		// the "use explicit aliases like name=url" message parseAddrs emits for
		// the --addr syntax.
		if prev, dup := seen[e.Name]; dup {
			return fmt.Errorf("config file: duplicate endpoint name %q (endpoints #%d and #%d)", e.Name, prev+1, i+1)
		}
		seen[e.Name] = i
		raws[i] = e.toAddrArg()
	}
	cfg.addrsRaw = raws
	cfg.addrsFromFile = true
	cfg.configDefault = fc.Default
	return nil
}

var fileEndpointNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// validate rejects field values that would break the name=url,suffix rendering
// of toAddrArg: a comma would be re-parsed as a suffix separator (silently
// injecting TLS options), and a name outside the alias grammar would misparse
// with an error phrased in flag terms the user never typed.
func (e fileEndpoint) validate(i int) error {
	if e.Name == "" {
		return fmt.Errorf("config file: endpoint #%d: name is required", i+1)
	}
	if !fileEndpointNameRe.MatchString(e.Name) {
		return fmt.Errorf("config file: endpoint name %q must start with a letter and contain only letters, digits and underscores", e.Name)
	}
	if e.Addr == "" {
		return fmt.Errorf("config file: endpoint %q: addr is required", e.Name)
	}
	for _, f := range []struct{ key, value string }{
		{"addr", e.Addr},
		{"ca_cert", e.CACert},
		{"cert", e.Cert},
		{"key", e.Key},
		{"interval", e.Interval},
	} {
		if strings.Contains(f.value, ",") {
			return fmt.Errorf("config file: endpoint %q: %s must not contain a comma", e.Name, f.key)
		}
	}
	return nil
}

// toAddrArg renders an endpoint as a single --addr token so the file path and
// the flag path share one parser and one set of validation messages.
func (e fileEndpoint) toAddrArg() string {
	var b strings.Builder
	b.WriteString(e.Name)
	b.WriteByte('=')
	b.WriteString(e.Addr)
	if e.CACert != "" {
		b.WriteString(",ca=" + e.CACert)
	}
	if e.Cert != "" {
		b.WriteString(",cert=" + e.Cert)
	}
	if e.Key != "" {
		b.WriteString(",key=" + e.Key)
	}
	if e.Insecure != nil {
		if *e.Insecure {
			b.WriteString(",insecure=true")
		} else {
			b.WriteString(",insecure=false")
		}
	}
	if e.Interval != "" {
		b.WriteString(",interval=" + e.Interval)
	}
	return b.String()
}
