package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newVersionCmd(version string) *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version and optionally check for updates",
		Example: `  ember version
  ember version --check`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "ember %s\n", version)

			if !check {
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			return checkLatestVersion(ctx, cmd.OutOrStdout(), version)
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Check for newer version on GitHub")

	return cmd
}

var latestReleaseURL = "https://api.github.com/repos/alexandre-daubois/ember/releases/latest"

func setLatestReleaseURL(url string) { latestReleaseURL = url }

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func checkLatestVersion(ctx context.Context, w io.Writer, current string) error {
	latest, err := fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("could not check for updates: %w", err)
	}

	latestClean := strings.TrimPrefix(latest.TagName, "v")
	currentClean := strings.TrimPrefix(current, "v")

	if currentClean == latestClean {
		fmt.Fprintln(w, "You are running the latest version.")
		return nil
	}

	// dev builds always show as outdated
	if strings.Contains(currentClean, "-dev") {
		fmt.Fprintf(w, "Development build. Latest release: %s\n  %s\n", latest.TagName, latest.HTMLURL)
		return nil
	}

	fmt.Fprintf(w, "A newer version is available: %s (current: %s)\n  %s\n", latest.TagName, current, latest.HTMLURL)
	return nil
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode response: %w", err)
	}
	return release, nil
}
