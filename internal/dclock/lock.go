// Package dclock manages the per-project lockfile that ties a local
// docker-compose project to a remote fc-spawn sandbox.
//
// The file lives at `.createos/dc.lock` next to the compose file. It's
// the single source of truth that links `dc up` to subsequent `dc ps`,
// `dc logs`, `dc exec`, and `dc down` invocations — without it, every
// command would have to re-create the sandbox or prompt the user.
//
// File format is JSON for human grep-ability and forward compatibility:
// unknown fields are preserved on read+write so older builds don't drop
// state written by newer ones.
package dclock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DirName is the per-project state directory (sibling of compose file).
const DirName = ".createos"

// FileName is the per-project lockfile within DirName.
const FileName = "dc.lock"

// Port maps one compose service's published port to the local TCP port
// the createos-cli tunnel is bound to on the user's laptop.
type Port struct {
	Service       string `json:"service"`
	ContainerPort int    `json:"container_port"`
	LocalPort     int    `json:"local_port"`
	Protocol      string `json:"protocol,omitempty"` // "tcp" / "udp" — empty = tcp
}

// Sync records the Mutagen sync session bound to this project.
//
// SessionName is mutagen's internal handle (used for `mutagen sync
// terminate/resume/list <name>`). LocalSSHPort is the port our control-
// plane SSH bridge listens on for mutagen to dial through; we pin it
// across `dc up` invocations so the existing session's URL stays
// valid and the next `dc up` can resume instead of recreating.
// PrivKeyPath is the SSH private key bound to the session — must match
// the pubkey installed in the sandbox.
type Sync struct {
	SessionName  string `json:"session_name"`
	LocalSSHPort int    `json:"local_ssh_port"`
	PrivKeyPath  string `json:"priv_key_path"`
}

// Lock is the persisted per-project state.
//
// SandboxID is the fc-spawn sandbox running this stack.
// ProjectName drives the docker compose `-p` flag (defaults to the
// compose-file directory's basename).
// ComposeFile is the path passed to `docker compose -f` INSIDE the VM
// (always under RemoteWorkdir).
// RemoteWorkdir is the absolute path inside the VM that Mutagen mirrors
// the local project directory to (typically /workspace/<ProjectName>).
type Lock struct {
	SandboxID     string    `json:"sandbox_id"`
	ProjectName   string    `json:"project_name"`
	ComposeFile   string    `json:"compose_file"`
	RemoteWorkdir string    `json:"remote_workdir"`
	Ports         []Port    `json:"ports,omitempty"`
	Sync          *Sync     `json:"sync,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// raw preserves any fields written by a newer build so a Load+Save
	// round-trip never drops state we don't know about.
	raw map[string]json.RawMessage
}

// ErrNotFound is returned by Load when no lockfile exists at the given
// project root. Callers typically translate it to a friendly
// "run 'createos sb dc up' first" message.
var ErrNotFound = errors.New("no dc.lock in this project — run 'createos sb dc up' first")

// Path returns the absolute lockfile path for a project rooted at
// projectDir. projectDir is the directory CONTAINING the compose file,
// not the compose file itself.
func Path(projectDir string) string {
	return filepath.Join(projectDir, DirName, FileName)
}

// Load reads the lockfile under projectDir. Returns ErrNotFound if the
// file doesn't exist (typed so callers can branch on it).
func Load(projectDir string) (*Lock, error) {
	p := Path(projectDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	l.raw = raw
	return &l, nil
}

// Save writes the lockfile under projectDir, creating .createos/ if
// needed. UpdatedAt is stamped automatically; CreatedAt is preserved if
// already set, otherwise stamped now.
func (l *Lock) Save(projectDir string) error {
	now := time.Now().UTC()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	l.UpdatedAt = now

	dir := filepath.Join(projectDir, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil { // #nosec G301 -- per-project state; no other user should read
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// Merge known fields back into raw so unknown keys round-trip.
	known, err := json.Marshal(l)
	if err != nil {
		return err
	}
	var knownMap map[string]json.RawMessage
	if umErr := json.Unmarshal(known, &knownMap); umErr != nil {
		return umErr
	}
	if l.raw == nil {
		l.raw = knownMap
	} else {
		for k, v := range knownMap {
			l.raw[k] = v
		}
	}
	data, err := json.MarshalIndent(l.raw, "", "  ")
	if err != nil {
		return err
	}
	p := Path(projectDir)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// Remove deletes the lockfile. Used by `dc down` after a successful
// destroy. Safe to call when no file exists.
func Remove(projectDir string) error {
	if err := os.Remove(Path(projectDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
