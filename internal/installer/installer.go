package installer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// InstallScope represents local (project) or global install
type InstallScope int

const (
	ScopeLocal  InstallScope = iota // installs into all 3 project dirs
	ScopeGlobal                     // installs into all 3 global dirs
)

// scopeDirs returns all target dirs for a given scope
func scopeDirs(uniqueName string, scope InstallScope) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if scope == ScopeLocal {
		return []string{
			filepath.Join(cwd, ".opencode", "skills", uniqueName),
			filepath.Join(cwd, ".claude", "skills", uniqueName),
			filepath.Join(cwd, ".agents", "skills", uniqueName),
		}, nil
	}
	return []string{
		filepath.Join(home, ".config", "opencode", "skills", uniqueName),
		filepath.Join(home, ".claude", "skills", uniqueName),
		filepath.Join(home, ".agents", "skills", uniqueName),
	}, nil
}

// IsScopeInstalled returns true if any of the scope dirs have SKILL.md
func IsScopeInstalled(uniqueName string, scope InstallScope) bool {
	dirs, err := scopeDirs(uniqueName, scope)
	if err != nil {
		return false
	}
	for _, d := range dirs {
		if isInstalled(d) {
			return true
		}
	}
	return false
}

// InstallScope downloads zip and extracts into all dirs for the given scope
func InstallToScope(downloadURL, uniqueName string, scope InstallScope) ([]string, error) {
	dirs, err := scopeDirs(uniqueName, scope)
	if err != nil {
		return nil, err
	}

	// Download once
	resp, err := http.Get(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Extract into each dir
	var installed []string
	for _, dir := range dirs {
		if err := unzip(data, dir); err != nil {
			return installed, fmt.Errorf("failed to unzip to %s: %w", dir, err)
		}
		installed = append(installed, dir)
	}
	return installed, nil
}

// UninstallScope removes all dirs for the given scope
func UninstallScope(uniqueName string, scope InstallScope) error {
	dirs, err := scopeDirs(uniqueName, scope)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

func isInstalled(path string) bool {
	_, err := os.Stat(filepath.Join(path, "SKILL.md"))
	return err == nil
}

func unzip(data []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	for _, f := range r.File {
		// sanitize path to prevent zip slip
		target := filepath.Join(destDir, filepath.Clean("/"+f.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
