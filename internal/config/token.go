package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDir = ".createos"
	tokenFile = ".token"
)

// configPath returns the path to ~/.createos
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir), nil
}

// tokenPath returns the path to ~/.createos/.token
func tokenPath() (string, error) {
	dir, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFile), nil
}

// SaveToken writes the token to ~/.createos/.token
func SaveToken(token string) error {
	dir, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path, err := tokenPath()
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(strings.TrimSpace(token)), 0600)
}

// LoadToken reads the token from ~/.createos/.token
func LoadToken() (string, error) {
	path, err := tokenPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is from tokenPath() under ~/.createos/
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("you're not signed in — run 'createos login' to get started")
		}
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

// DeleteToken removes ~/.createos/.token
func DeleteToken() error {
	path, err := tokenPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// tokenFileExists returns true if the API key token file exists
func tokenFileExists() bool {
	path, err := tokenPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// IsLoggedIn returns true if the user is signed in via API key or OAuth
func IsLoggedIn() bool {
	return tokenFileExists() || HasOAuthSession()
}
