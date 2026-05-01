package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const identityFile = ".identity"

// Identity is the post-login user binding stored in ~/.createos/.identity.
//
// Kept in its own file (NOT inside OAuthSession or .token) so that the two
// existing auth paths — OAuth JSON and flat-string token — stay untouched.
// AliasedForUserID is set after RebindIdentity has emitted PostHog Alias for
// this user_id; the marker keeps the alias one-shot per (machine, user)
// pair.
type Identity struct {
	UserID           string `json:"user_id"`
	AliasedForUserID string `json:"aliased_for_user_id,omitempty"`
}

// identityPath returns the path to ~/.createos/.identity.
func identityPath() (string, error) {
	dir, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, identityFile), nil
}

// SaveIdentity writes the identity file (mode 0600) under ~/.createos.
func SaveIdentity(id Identity) error {
	dir, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := identityPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(id)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadIdentity reads ~/.createos/.identity. Returns (nil, nil) when the file
// is absent (matches the LoadOAuthSession contract).
func LoadIdentity() (*Identity, error) {
	path, err := identityPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is from identityPath() under ~/.createos/
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}
	return &id, nil
}

// DeleteIdentity removes ~/.createos/.identity. Nil on os.ErrNotExist.
func DeleteIdentity() error {
	path, err := identityPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
