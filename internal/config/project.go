package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const projectFile = ".createos.json"

// ProjectConfig holds the local project linking configuration.
type ProjectConfig struct {
	ProjectID     string `json:"projectId"`
	EnvironmentID string `json:"environmentId,omitempty"`
	ProjectName   string `json:"projectName,omitempty"`
}

// SaveProjectConfig writes the project config to .createos.json in the given directory.
func SaveProjectConfig(dir string, cfg ProjectConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, projectFile), data, 0644) // #nosec G306 -- project config needs to be readable by other tools
}

// LoadProjectConfig reads .createos.json from the given directory.
func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, projectFile)) // #nosec G304 -- dir comes from os.Getwd() walk, projectFile is a constant
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FindProjectConfig walks up from the current directory looking for .createos.json.
func FindProjectConfig() (*ProjectConfig, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		cfg, err := LoadProjectConfig(dir)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			return cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// EnsureGitignore adds .createos.json to .gitignore if not already present.
func EnsureGitignore(dir string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignorePath) // #nosec G304 -- gitignorePath is filepath.Join(dir, ".gitignore")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(data)
	for _, line := range splitLines(content) {
		if line == projectFile {
			return nil
		}
	}
	if content != "" && content[len(content)-1] != '\n' {
		content += "\n"
	}
	content += projectFile + "\n"
	return os.WriteFile(gitignorePath, []byte(content), 0644) // #nosec G306,G703 -- .gitignore must be world-readable; path is from filepath.Join
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
