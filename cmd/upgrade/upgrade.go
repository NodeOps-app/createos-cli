// Package upgrade provides the self-upgrade command.
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/mod/semver"

	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
)

const (
	githubRepo = "NodeOps-app/createos-cli"

	// maxBinarySize caps the download at 200 MB to prevent disk exhaustion.
	maxBinarySize = 200 * 1024 * 1024

	apiTimeout      = 15 * time.Second
	downloadTimeout = 5 * time.Minute
)

var httpClient = &http.Client{Timeout: apiTimeout}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// NewUpgradeCommand creates the upgrade command.
func NewUpgradeCommand() *cli.Command {
	return &cli.Command{
		Name:  "upgrade",
		Usage: "Upgrade createos to the latest version",
		Action: func(_ *cli.Context) error {
			return runUpgrade()
		},
	}
}

func runUpgrade() error {
	pterm.Info.Printf("Current version: %s (channel: %s)\n", version.Version, version.Channel)

	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("could not check for updates: %w", err)
	}

	if version.Channel == "nightly" {
		remoteCommit, err := fetchNightlyCommit(release)
		if err != nil {
			return fmt.Errorf("could not check nightly commit: %w", err)
		}
		if remoteCommit == version.Commit {
			pterm.Success.Printf("Already up to date (commit: %s).\n", shortSHA(version.Commit))
			return nil
		}
		pterm.Info.Printf("New nightly available: %s → %s\n", shortSHA(version.Commit), shortSHA(remoteCommit))
	} else {
		cmp := semver.Compare(version.Version, release.TagName)
		switch {
		case cmp == 0:
			pterm.Success.Println("Already up to date.")
			return nil
		case cmp > 0:
			pterm.Info.Printf("Your version (%s) is ahead of the latest release (%s). Nothing to do.\n", version.Version, release.TagName)
			return nil
		}
	}

	pterm.Info.Printf("New version available: %s\n", release.TagName)

	assetName := binaryAssetName()
	checksumName := assetName + ".sha256"
	downloadURL := ""
	checksumURL := ""
	for _, a := range release.Assets {
		switch a.Name {
		case assetName:
			downloadURL = a.BrowserDownloadURL
		case checksumName:
			checksumURL = a.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no binary found for your platform (%s/%s) in release %s", runtime.GOOS, runtime.GOARCH, release.TagName)
	}
	if checksumURL == "" {
		return fmt.Errorf("no checksum file found for %s in release %s", assetName, release.TagName)
	}

	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("release asset URL failed validation: %w", err)
	}
	if err := validateDownloadURL(checksumURL); err != nil {
		return fmt.Errorf("checksum URL failed validation: %w", err)
	}

	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Downloading %s...", assetName))

	tmp, err := downloadToTemp(downloadURL)
	if err != nil {
		spinner.Fail("Download failed")
		return fmt.Errorf("could not download update: %w", err)
	}
	defer os.Remove(tmp) //nolint:errcheck

	spinner.UpdateText("Verifying checksum...")

	expectedHash, err := fetchChecksum(checksumURL)
	if err != nil {
		spinner.Fail("Checksum fetch failed")
		return fmt.Errorf("could not fetch checksum: %w", err)
	}

	if err := verifyChecksum(tmp, expectedHash); err != nil {
		spinner.Fail("Checksum mismatch")
		return err
	}

	spinner.Success("Downloaded and verified")

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate current binary: %w", err)
	}

	if err := replaceExecutable(exe, tmp); err != nil {
		return fmt.Errorf("could not replace binary: %w", err)
	}

	pterm.Success.Printf("Upgraded to %s. Run 'createos version' to confirm.\n", release.TagName)
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	var apiURL string
	if version.Channel == "nightly" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/nightly", githubRepo)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// validateDownloadURL ensures the URL is HTTPS and hosted on GitHub domains.
func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "objects.githubusercontent.com" {
		return fmt.Errorf("URL must be hosted on github.com (got %q)", host)
	}
	return nil
}

func binaryAssetName() string {
	name := fmt.Sprintf("createos-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func downloadToTemp(rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "createos-upgrade-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close() //nolint:errcheck

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBinarySize)); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

func fetchNightlyCommit(release *githubRelease) (string, error) {
	commitURL := ""
	for _, a := range release.Assets {
		if a.Name == "commit.txt" {
			commitURL = a.BrowserDownloadURL
			break
		}
	}
	if commitURL == "" {
		return "", fmt.Errorf("commit.txt not found in nightly release assets")
	}
	if err := validateDownloadURL(commitURL); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, commitURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("commit.txt download returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func shortSHA(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}

func fetchChecksum(rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download returned %d", resp.StatusCode)
	}

	// Checksum file contains only the hex digest (64 bytes)
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}

	hash := strings.TrimSpace(string(data))
	if len(hash) != 64 {
		return "", fmt.Errorf("unexpected checksum format")
	}
	return hash, nil
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path) //nolint:gosec // path comes from os.CreateTemp, not user input
	if err != nil {
		return fmt.Errorf("could not open downloaded file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("could not hash downloaded file: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch — download may be corrupted or tampered with\n  expected: %s\n  got:      %s", expected, actual)
	}
	return nil
}

func replaceExecutable(dst, src string) error {
	if err := os.Chmod(src, 0o755); err != nil { //nolint:gosec // executable binary requires 0755
		return err
	}

	// On Windows the running exe cannot be overwritten directly;
	// rename it away first then rename the new one into place.
	if strings.EqualFold(runtime.GOOS, "windows") {
		old := dst + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}

	return os.Rename(src, dst)
}
