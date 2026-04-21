package plugin

import (
	"strings"
	"sync"
)

var (
	mu       sync.Mutex
	registry []Plugin
)

// Register adds a plugin to the global registry.
// It is intended to be called from init() functions in plugin packages.
//
// Register panics if:
//   - the name is empty
//   - the name contains whitespace or underscores
//   - a plugin with the same name is already registered
//   - another plugin's name collides after hyphen removal
//     (e.g., "my-plugin" and "myplugin" would share the same
//     environment variable prefix EMBER_PLUGIN_MYPLUGIN_)
func Register(p Plugin) {
	mu.Lock()
	defer mu.Unlock()

	name := p.Name()
	if name == "" {
		panic("ember: plugin name must not be empty")
	}
	if strings.ContainsFunc(name, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		panic("ember: plugin name must not contain whitespace: " + name)
	}
	if strings.Contains(name, "_") {
		panic("ember: plugin name must not contain underscores (use hyphens instead): " + name)
	}

	norm := normalizeName(name)
	for _, existing := range registry {
		if existing.Name() == name {
			panic("ember: duplicate plugin name: " + name)
		}
		if normalizeName(existing.Name()) == norm {
			panic("ember: plugin name collision after normalization: " + name + " vs " + existing.Name())
		}
	}
	registry = append(registry, p)
}

// All returns a copy of all registered plugins, in registration order.
func All() []Plugin {
	mu.Lock()
	defer mu.Unlock()
	result := make([]Plugin, len(registry))
	copy(result, registry)
	return result
}

func normalizeName(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", ""))
}

// EnvPrefix returns the environment variable prefix Ember uses to pass
// per-plugin options to [Plugin.Provision]. For a plugin named "rate-limit",
// the prefix is "EMBER_PLUGIN_RATELIMIT_", so EMBER_PLUGIN_RATELIMIT_MAX_RPS
// becomes Options["max_rps"].
func EnvPrefix(name string) string {
	return "EMBER_PLUGIN_" + normalizeName(name) + "_"
}

// Reset clears the registry. Intended for testing only.
func Reset() {
	mu.Lock()
	registry = nil
	mu.Unlock()
}
