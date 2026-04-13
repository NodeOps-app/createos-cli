// Package git provides helpers for interacting with the local git repository.
package git

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// repoFullNamePattern extracts owner/repo from GitHub URLs.
// Matches both HTTPS and SSH formats:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
var repoFullNamePattern = regexp.MustCompile(`github\.com[:/]([^/]+/[^/.\s]+?)(?:\.git)?$`)

// GetRemoteFullName returns the "owner/repo" for the git remote origin in the
// given directory, or empty string if it cannot be determined.
func GetRemoteFullName(dir string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "remote", "get-url", "origin") // #nosec G204 -- dir comes from os.Getwd()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	m := repoFullNamePattern.FindStringSubmatch(url)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
