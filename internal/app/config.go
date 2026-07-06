package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "config",
		Short:         "Manage the Ember config file",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newConfigUseCmd(cfg))
	return cmd
}

func newConfigUseCmd(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default endpoint used by the TUI",
		Long: `Sets the top-level "default" key in the config file to <name>, so the
single-instance TUI connects to that endpoint instead of showing the picker.
Only the default line is rewritten; comments and formatting are left intact.`,
		Example: `  ember config use production
  ember -f .ember.staging.toml config use staging`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigUse(cmd.OutOrStdout(), cfg.configPath, args[0])
		},
	}
}

func runConfigUse(w io.Writer, path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no config file at %s (create one before setting a default)", path)
		}
		return err
	}

	var fc fileConfig
	if err := toml.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("config file %s: %w", path, err)
	}
	if !slices.ContainsFunc(fc.Endpoints, func(e fileEndpoint) bool { return e.Name == name }) {
		return fmt.Errorf("endpoint %q not found in %s (available: %s)", name, path, endpointNames(fc.Endpoints))
	}

	if err := os.WriteFile(path, []byte(setDefaultKey(string(data), name)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(w, "Default endpoint set to %q in %s\n", name, path)
	return nil
}

var defaultKeyRe = regexp.MustCompile(`(?m)^(\s*default\s*=\s*)("[^"]*"|'[^']*'|\S+)(.*)$`)

// setDefaultKey returns content with the top-level default key set to name. An
// existing default line (above the first table header) is rewritten in place,
// keeping any trailing comment; otherwise the key is prepended.
func setDefaultKey(content, name string) string {
	quoted := strconv.Quote(name)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if m := defaultKeyRe.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + quoted + m[3]
			return strings.Join(lines, "\n")
		}
	}
	return "default = " + quoted + "\n" + content
}

func endpointNames(eps []fileEndpoint) string {
	names := make([]string, len(eps))
	for i, e := range eps {
		names[i] = e.Name
	}
	return strings.Join(names, ", ")
}
