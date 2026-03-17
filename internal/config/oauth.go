// Package config manages local configuration and credential storage.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const oauthFile = ".oauth"

// OAuthClientID is the pre-registered public OAuth client ID for the CreateOS CLI
const OAuthClientID = "fbcaaa58-1e30-43fe-8fba-34382ba4fe7f"

// OAuthIssuerURL is the OAuth identity server base URL
const OAuthIssuerURL = "https://id.nodeops.network"

// OAuthSession holds the OAuth tokens
type OAuthSession struct {
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAt     int64  `json:"expires_at"` // Unix timestamp
	TokenEndpoint string `json:"token_endpoint"`
}

// oauthPath returns the path to ~/.createos/.oauth
func oauthPath() (string, error) {
	dir, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, oauthFile), nil
}

// SaveOAuthSession writes the OAuth session to ~/.createos/.oauth
func SaveOAuthSession(session OAuthSession) error {
	dir, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := oauthPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(session) //nolint:gosec
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadOAuthSession reads the OAuth session from ~/.createos/.oauth
func LoadOAuthSession() (*OAuthSession, error) {
	path, err := oauthPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var session OAuthSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteOAuthSession removes ~/.createos/.oauth
func DeleteOAuthSession() error {
	path, err := oauthPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// HasOAuthSession returns true if an OAuth session file exists
func HasOAuthSession() bool {
	path, err := oauthPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// IsTokenExpired returns true if the access token is expired or will expire within 60 seconds
func IsTokenExpired(session *OAuthSession) bool {
	return time.Now().Unix() >= session.ExpiresAt-60
}
