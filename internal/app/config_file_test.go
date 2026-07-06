package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".ember.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestFileEndpoint_ToAddrArg(t *testing.T) {
	cases := []struct {
		name string
		ep   fileEndpoint
		want string
	}{
		{"minimal", fileEndpoint{Name: "web", Addr: "http://a"}, "web=http://a"},
		{"ca", fileEndpoint{Name: "web", Addr: "https://a", CACert: "/ca.pem"}, "web=https://a,ca=/ca.pem"},
		{"cert+key", fileEndpoint{Name: "web", Addr: "https://a", Cert: "/c.pem", Key: "/k.pem"}, "web=https://a,cert=/c.pem,key=/k.pem"},
		{"insecure true", fileEndpoint{Name: "web", Addr: "https://a", Insecure: boolPtr(true)}, "web=https://a,insecure=true"},
		{"insecure false", fileEndpoint{Name: "web", Addr: "https://a", Insecure: boolPtr(false)}, "web=https://a,insecure=false"},
		{"interval", fileEndpoint{Name: "web", Addr: "https://a", Interval: "2s"}, "web=https://a,interval=2s"},
		{"all", fileEndpoint{Name: "web", Addr: "https://a", CACert: "/ca", Cert: "/c", Key: "/k", Insecure: boolPtr(true), Interval: "5s"}, "web=https://a,ca=/ca,cert=/c,key=/k,insecure=true,interval=5s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.ep.toAddrArg())
		})
	}
}

func TestParseConfigFile_Valid(t *testing.T) {
	file := writeConfigFile(t, `
default = "prod"
interval = "2s"

[[endpoints]]
name = "prod"
addr = "https://prod:2019"

[[endpoints]]
name = "staging"
addr = "https://staging:2019"
`)
	fc, err := parseConfigFile(file)
	require.NoError(t, err)
	assert.Equal(t, "prod", fc.Default)
	assert.Equal(t, "2s", fc.Interval)
	require.Len(t, fc.Endpoints, 2)
	assert.Equal(t, "staging", fc.Endpoints[1].Name)
	assert.Equal(t, "https://staging:2019", fc.Endpoints[1].Addr)
}

func TestParseConfigFile_Malformed(t *testing.T) {
	file := writeConfigFile(t, "this is not = valid = toml [[[")
	_, err := parseConfigFile(file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file")
}

func TestParseConfigFile_Missing(t *testing.T) {
	_, err := parseConfigFile(filepath.Join(t.TempDir(), "absent.toml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestApplyFileEndpoints_Empty(t *testing.T) {
	cfg := &config{addrsRaw: []string{"http://localhost:2019"}}
	require.NoError(t, applyFileEndpoints(cfg, &fileConfig{}))
	assert.Equal(t, []string{"http://localhost:2019"}, cfg.addrsRaw, "no endpoints keeps the built-in default")
	assert.False(t, cfg.addrsFromFile)
}

func TestApplyFileEndpoints_Multi(t *testing.T) {
	cfg := &config{addrsRaw: []string{"http://localhost:2019"}}
	fc := &fileConfig{
		Default:   "web1",
		Endpoints: []fileEndpoint{{Name: "web1", Addr: "http://a"}, {Name: "web2", Addr: "http://b"}},
	}
	require.NoError(t, applyFileEndpoints(cfg, fc))
	assert.Equal(t, []string{"web1=http://a", "web2=http://b"}, cfg.addrsRaw)
	assert.True(t, cfg.addrsFromFile)
	assert.Equal(t, "web1", cfg.configDefault)
}

func TestApplyFileEndpoints_Single(t *testing.T) {
	cfg := &config{addrsRaw: []string{"http://localhost:2019"}}
	require.NoError(t, applyFileEndpoints(cfg, &fileConfig{Endpoints: []fileEndpoint{{Name: "solo", Addr: "http://a"}}}))
	assert.Equal(t, []string{"solo=http://a"}, cfg.addrsRaw)
	assert.True(t, cfg.addrsFromFile)
}

func TestApplyFileEndpoints_MissingName(t *testing.T) {
	cfg := &config{}
	err := applyFileEndpoints(cfg, &fileConfig{Endpoints: []fileEndpoint{{Addr: "http://a"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestApplyFileEndpoints_MissingAddr(t *testing.T) {
	cfg := &config{}
	err := applyFileEndpoints(cfg, &fileConfig{Endpoints: []fileEndpoint{{Name: "web"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "addr is required")
}

func TestApplyFileEndpoints_InvalidName(t *testing.T) {
	for _, name := range []string{"web.prod", "web,prod", "web prod", "web=prod", "1web", "-web"} {
		err := applyFileEndpoints(&config{}, &fileConfig{Endpoints: []fileEndpoint{{Name: name, Addr: "http://a"}}})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "config file: endpoint name", name)
	}
}

func TestApplyFileEndpoints_ValidNames(t *testing.T) {
	for _, name := range []string{"web", "web_1", "Web2"} {
		cfg := &config{}
		require.NoError(t, applyFileEndpoints(cfg, &fileConfig{Endpoints: []fileEndpoint{{Name: name, Addr: "http://a"}}}))
		assert.Equal(t, []string{name + "=http://a"}, cfg.addrsRaw)
	}
}

func TestApplyFileEndpoints_CommaInjectionRejected(t *testing.T) {
	cases := []struct {
		field string
		ep    fileEndpoint
	}{
		{"addr", fileEndpoint{Name: "web", Addr: "https://h,ca=/evil.pem"}},
		{"ca_cert", fileEndpoint{Name: "web", Addr: "https://h", CACert: "/a,cert=/evil.pem"}},
		{"cert", fileEndpoint{Name: "web", Addr: "https://h", Cert: "/a,key=/evil.pem"}},
		{"key", fileEndpoint{Name: "web", Addr: "https://h", Key: "/a,insecure=true"}},
		{"interval", fileEndpoint{Name: "web", Addr: "https://h", Interval: "2s,insecure=true"}},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			err := applyFileEndpoints(&config{}, &fileConfig{Endpoints: []fileEndpoint{tc.ep}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.field+" must not contain a comma")
		})
	}
}

func TestApplyFileGlobals_AppliesWhenFlagsUnset(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cfg := &config{interval: time.Second}
	fc := &fileConfig{CACert: "/ca", Cert: "/c", Key: "/k", Insecure: boolPtr(true), Interval: "3s"}
	require.NoError(t, applyFileGlobals(cmd, cfg, fc))
	assert.Equal(t, "/ca", cfg.caCert)
	assert.Equal(t, "/c", cfg.clientCert)
	assert.Equal(t, "/k", cfg.clientKey)
	assert.True(t, cfg.insecure)
	assert.Equal(t, 3*time.Second, cfg.interval)
}

func TestApplyFileGlobals_IntervalFlagWins(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.Flag("interval").Changed = true
	cfg := &config{interval: 5 * time.Second}
	require.NoError(t, applyFileGlobals(cmd, cfg, &fileConfig{Interval: "3s"}))
	assert.Equal(t, 5*time.Second, cfg.interval, "explicit --interval must beat the file global")
}

func TestApplyFileGlobals_InsecureFlagWins(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.Flag("insecure").Changed = true
	cfg := &config{insecure: true}
	require.NoError(t, applyFileGlobals(cmd, cfg, &fileConfig{Insecure: boolPtr(false)}))
	assert.True(t, cfg.insecure, "explicit --insecure must beat the file global")
}

func TestApplyFileGlobals_CACertFlagWins(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.Flag("ca-cert").Changed = true
	cfg := &config{caCert: "/flag/ca.pem"}
	require.NoError(t, applyFileGlobals(cmd, cfg, &fileConfig{CACert: "/file/ca.pem"}))
	assert.Equal(t, "/flag/ca.pem", cfg.caCert)
}

func TestApplyFileGlobals_InvalidInterval(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	err := applyFileGlobals(cmd, &config{}, &fileConfig{Interval: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interval")
}

func TestCommandConsumesAddrs(t *testing.T) {
	root := newRootCmd("0.0.0")
	assert.True(t, commandConsumesAddrs(root), "root consumes addrs")

	sub := func(name string) *cobra.Command {
		for _, c := range root.Commands() {
			if c.Name() == name {
				return c
			}
		}
		t.Fatalf("subcommand %q not found", name)
		return nil
	}
	for _, name := range []string{"status", "wait", "init"} {
		assert.True(t, commandConsumesAddrs(sub(name)), name)
	}
	for _, name := range []string{"diff", "version", "config"} {
		assert.False(t, commandConsumesAddrs(sub(name)), name)
	}
	// nested: `config use` must not consume addrs either
	cfgCmd := sub("config")
	for _, c := range cfgCmd.Commands() {
		if c.Name() == "use" {
			assert.False(t, commandConsumesAddrs(c), "config use")
		}
	}
}

func TestLoadConfigFile_LoadsEndpoints(t *testing.T) {
	file := writeConfigFile(t, `
default = "web1"

[[endpoints]]
name = "web1"
addr = "http://a"

[[endpoints]]
name = "web2"
addr = "http://b"
`)
	cmd := newRootCmd("0.0.0")
	cfg := &config{configPath: file}
	require.NoError(t, loadConfigFile(cmd, cfg))
	assert.True(t, cfg.addrsFromFile)
	assert.Equal(t, []string{"web1=http://a", "web2=http://b"}, cfg.addrsRaw)
	assert.Equal(t, "web1", cfg.configDefault)
}

func TestLoadConfigFile_AddrChangedSkips(t *testing.T) {
	file := writeConfigFile(t, "[[endpoints]]\nname = \"web1\"\naddr = \"http://a\"\n")
	cmd := newRootCmd("0.0.0")
	cmd.Flag("addr").Changed = true
	cfg := &config{configPath: file, addrsRaw: []string{"http://flag:2019"}}
	require.NoError(t, loadConfigFile(cmd, cfg))
	assert.False(t, cfg.addrsFromFile, "--addr present means the file is ignored entirely")
	assert.Equal(t, []string{"http://flag:2019"}, cfg.addrsRaw)
}

func TestLoadConfigFile_DiffSkips(t *testing.T) {
	file := writeConfigFile(t, "garbage [[[ not toml")
	root := newRootCmd("0.0.0")
	var diff *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "diff" {
			diff = c
		}
	}
	require.NotNil(t, diff)
	cfg := &config{configPath: file}
	require.NoError(t, loadConfigFile(diff, cfg), "diff must not parse the config file")
}

func TestLoadConfigFile_MissingDefaultSilent(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cfg := &config{configPath: filepath.Join(t.TempDir(), "absent.toml")}
	require.NoError(t, loadConfigFile(cmd, cfg))
	assert.False(t, cfg.addrsFromFile)
}

func TestLoadConfigFile_MissingExplicitErrors(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	cmd.Flag("config").Changed = true
	cfg := &config{configPath: filepath.Join(t.TempDir(), "absent.toml")}
	err := loadConfigFile(cmd, cfg)
	require.Error(t, err, "explicit -f/EMBER_CONFIG missing file is a hard error")
}

func TestLoadConfigFile_MalformedErrors(t *testing.T) {
	file := writeConfigFile(t, "not = valid [[[")
	cmd := newRootCmd("0.0.0")
	cfg := &config{configPath: file}
	err := loadConfigFile(cmd, cfg)
	require.Error(t, err, "a malformed default file is a hard error even without -f")
}

func TestLoadConfigFile_PerEndpointTLSRoundTrip(t *testing.T) {
	file := writeConfigFile(t, `
[[endpoints]]
name = "secure"
addr = "https://a"
ca_cert = "/etc/ca.pem"
insecure = true

[[endpoints]]
name = "plain"
addr = "https://b"
`)
	cmd := newRootCmd("0.0.0")
	cfg := &config{configPath: file}
	require.NoError(t, loadConfigFile(cmd, cfg))

	specs, err := parseAddrs(cfg.addrsRaw)
	require.NoError(t, err)
	require.Len(t, specs, 2)
	assert.Equal(t, "/etc/ca.pem", specs[0].tls.caCert)
	assert.True(t, specs[0].tls.insecure)
	assert.True(t, specs[0].tls.insecureSet)
	assert.Empty(t, specs[1].tls.caCert)
}

func TestLoadConfigFile_GlobalCACertFallback(t *testing.T) {
	file := writeConfigFile(t, `
ca_cert = "/global/ca.pem"

[[endpoints]]
name = "a"
addr = "https://a"

[[endpoints]]
name = "b"
addr = "https://b"
ca_cert = "/b/ca.pem"
`)
	cmd := newRootCmd("0.0.0")
	cfg := &config{configPath: file}
	require.NoError(t, loadConfigFile(cmd, cfg))
	assert.Equal(t, "/global/ca.pem", cfg.caCert)

	specs, err := parseAddrs(cfg.addrsRaw)
	require.NoError(t, err)
	require.Len(t, specs, 2)
	assert.Equal(t, "/global/ca.pem", effectiveTLS(specs[0], cfg).CACert, "endpoint without ca_cert inherits the global")
	assert.Equal(t, "/b/ca.pem", effectiveTLS(specs[1], cfg).CACert, "endpoint ca_cert overrides the global")
}

func TestRun_ConfigFile_MultiStatusFanOut(t *testing.T) {
	file := writeConfigFile(t, `
[[endpoints]]
name = "alpha"
addr = "http://192.0.2.1:1"

[[endpoints]]
name = "beta"
addr = "http://192.0.2.1:2"
`)
	err := Run([]string{"-f", file, "status", "--timeout", "2s"}, "0.0.0")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "single-instance by design")
	assert.Contains(t, err.Error(), "alpha")
	assert.Contains(t, err.Error(), "beta")
}

func TestRun_ConfigFile_DiffIgnoresMalformed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ember.toml"), []byte("garbage [[[ not toml"), 0o644))
	t.Chdir(dir)

	err := Run([]string{"diff", "missing-a.json", "missing-b.json"}, "0.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load", "diff surfaces its own error, not a config parse error")
	assert.NotContains(t, err.Error(), "config file")
}

func TestRun_ConfigFile_ExplicitMissingErrors(t *testing.T) {
	err := Run([]string{"-f", filepath.Join(t.TempDir(), "absent.toml"), "status", "--timeout", "2s"}, "0.0.0")
	require.Error(t, err)
}

func TestBindEnv_ConfigFromEnv(t *testing.T) {
	t.Setenv("EMBER_CONFIG", "/etc/ember/prod.toml")
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)
	assert.Equal(t, "/etc/ember/prod.toml", cmd.Flag("config").Value.String())
}

func TestBindEnv_AddrFromEnvMarksChanged(t *testing.T) {
	t.Setenv("EMBER_ADDR", "http://remote:2019")
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)
	assert.True(t, cmd.Flag("addr").Changed, "EMBER_ADDR must mark --addr changed so the config file is skipped")
}

func TestBindEnv_IntervalFromEnvMarksChanged(t *testing.T) {
	t.Setenv("EMBER_INTERVAL", "5s")
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)
	assert.True(t, cmd.Flag("interval").Changed, "EMBER_INTERVAL must mark --interval changed so a file global cannot override it")
}

func TestBindEnv_UnsetEnvDoesNotMarkChanged(t *testing.T) {
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)
	assert.False(t, cmd.Flag("addr").Changed, "an unset env var must leave the flag unchanged so the file can load")
}

func TestLoadConfigFile_EnvAddrSkipsFile(t *testing.T) {
	t.Setenv("EMBER_ADDR", "http://192.0.2.9:9")
	file := writeConfigFile(t, `
[[endpoints]]
name = "alpha"
addr = "http://a"

[[endpoints]]
name = "beta"
addr = "http://b"
`)
	cmd := newRootCmd("0.0.0")
	bindEnv(cmd)
	cfg := &config{configPath: file}
	require.NoError(t, loadConfigFile(cmd, cfg))
	assert.False(t, cfg.addrsFromFile, "EMBER_ADDR must make loadConfigFile ignore the file (precedence: env > file)")
}

func TestRun_ConfigFile_FromEnvVar(t *testing.T) {
	file := writeConfigFile(t, `
[[endpoints]]
name = "alpha"
addr = "http://192.0.2.1:1"

[[endpoints]]
name = "beta"
addr = "http://192.0.2.1:2"
`)
	t.Setenv("EMBER_CONFIG", file)
	err := Run([]string{"status", "--timeout", "2s"}, "0.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alpha", "EMBER_CONFIG file must be loaded and fanned out")
}
