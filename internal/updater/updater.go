// Package updater checks for newer CLI versions and caches the result.
package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"

	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
)

const (
	githubRepo   = "NodeOps-app/createos-cli"
	cacheFile    = ".version-check"
	cacheTTL     = 24 * time.Hour
	checkTimeout = 3 * time.Second
)

type versionCache struct {
	CheckedAt int64  `json:"checked_at"`
	Latest    string `json:"latest"`
}

// LatestVersion returns the latest available stable version if it is newer
// than the running binary. Returns empty string if up to date, offline, or
// on the nightly channel.
func LatestVersion() string {
	// Nightly users use commit-based comparison via `upgrade` — skip notification
	if version.Channel == "nightly" || version.Version == "dev" {
		return ""
	}

	latest := cachedVersion()
	if latest == "" {
		latest = fetchLatest()
		saveCache(latest)
	}

	if latest == "" {
		return ""
	}

	if semver.Compare(version.Version, latest) < 0 {
		return latest
	}
	return ""
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".createos", cacheFile)
}

// cachedVersion returns the cached latest version if the cache is fresh.
func cachedVersion() string {
	path := cacheFilePath()
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is from cacheFilePath() under ~/.createos/
	if err != nil {
		return ""
	}

	var c versionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return ""
	}

	if time.Since(time.Unix(c.CheckedAt, 0)) > cacheTTL {
		return "" // stale — trigger a fresh fetch
	}

	return c.Latest
}

func saveCache(latest string) {
	path := cacheFilePath()
	if path == "" {
		return
	}

	data, err := json.Marshal(versionCache{
		CheckedAt: time.Now().Unix(),
		Latest:    latest,
	})
	if err != nil {
		return
	}

	_ = os.WriteFile(path, data, 0o600)
}

func fetchLatest() string {
	url := "https://api.github.com/repos/" + githubRepo + "/releases/latest"

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	return result.TagName
}
