// Package sshkey resolves (or auto-generates) the ed25519 keypair used
// by `createos sb dc up` to talk to the in-sandbox sshd via Mutagen.
//
// Resolution order:
//
//  1. --identity flag on the command (caller passes it explicitly)
//  2. ~/.ssh/id_ed25519
//  3. ~/.ssh/id_rsa
//  4. ~/.ssh/id_ecdsa
//  5. ~/.createos/dc_ed25519 — our managed key. Auto-generated on
//     first run if none of the above exist.
//
// The managed key is the "just works" default: zero prompts, no
// ssh-keygen step. Users who care can still pass --identity.
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// ManagedKeyName is the filename of the createos-managed ed25519 key
// under ~/.createos/. The public counterpart is ManagedKeyName + ".pub".
const ManagedKeyName = "dc_ed25519"

// Pair holds the on-disk paths for a resolved keypair. Both paths are
// absolute and the files definitely exist.
type Pair struct {
	PrivPath string
	PubPath  string
	// Managed reports whether this pair is the auto-generated managed
	// key (as opposed to a user-provided key under ~/.ssh). Callers
	// can use this to decide whether a 0o600 mode check is necessary.
	Managed bool
}

// ResolveOrGenerate finds an existing keypair using the precedence
// listed in the package doc, or generates the managed key if none
// exist. `explicit` is the value of --identity (private key path);
// "" means "auto-pick". Errors are returned for unreadable user-
// specified keys but NOT for missing default keys (we just fall
// through).
func ResolveOrGenerate(explicit string) (*Pair, error) {
	if explicit != "" {
		return readPair(explicit)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			priv := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(priv); err == nil {
				return readPair(priv)
			}
		}
	}
	return generateManagedKey()
}

// readPair validates that a user-provided private key + its .pub
// counterpart exist and returns a Pair. Mode-permission errors on the
// private key are surfaced as a friendly hint.
func readPair(privPath string) (*Pair, error) {
	st, err := os.Stat(privPath)
	if err != nil {
		return nil, fmt.Errorf("SSH key %s: %w", privPath, err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("SSH key %s is world/group readable (mode %o); run 'chmod 600 %s'", privPath, st.Mode().Perm(), privPath)
	}
	pubPath := privPath + ".pub"
	if _, err := os.Stat(pubPath); err != nil {
		return nil, fmt.Errorf("public key %s not found alongside %s", pubPath, privPath)
	}
	return &Pair{PrivPath: privPath, PubPath: pubPath, Managed: false}, nil
}

// generateManagedKey creates the managed keypair under ~/.createos/.
// Idempotent — if the files already exist, it returns them without
// re-generating (so the public key the sandbox already trusts stays
// valid across runs).
//
// Private key is OpenSSH-format PEM (no passphrase, mode 0600).
// Public key is the standard "ssh-ed25519 AAAA... createos-cli" line.
func generateManagedKey() (*Pair, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve $HOME: %w", err)
	}
	dir := filepath.Join(home, ".createos")
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return nil, fmt.Errorf("create %s: %w", dir, mkErr)
	}
	priv := filepath.Join(dir, ManagedKeyName)
	pub := priv + ".pub"

	// Idempotent: reuse if both halves exist.
	if _, perr := os.Stat(priv); perr == nil {
		if _, perr := os.Stat(pub); perr == nil {
			return &Pair{PrivPath: priv, PubPath: pub, Managed: true}, nil
		}
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	block, mErr := ssh.MarshalPrivateKey(privKey, "createos-cli managed key")
	if mErr != nil {
		return nil, fmt.Errorf("marshal private key: %w", mErr)
	}
	if wErr := os.WriteFile(priv, pem.EncodeToMemory(block), 0o600); wErr != nil { // #nosec G306 -- 0600 is intentional
		return nil, fmt.Errorf("write %s: %w", priv, wErr)
	}
	sshPub, npErr := ssh.NewPublicKey(pubKey)
	if npErr != nil {
		// Clean up the private half so future runs don't inherit a
		// broken pair.
		_ = os.Remove(priv) //nolint:errcheck
		return nil, fmt.Errorf("encode public key: %w", npErr)
	}
	authLine := append([]byte{}, ssh.MarshalAuthorizedKey(sshPub)...)
	// MarshalAuthorizedKey adds a trailing \n but no comment; tack on
	// our identifier so users see WHICH key this is in their sandbox's
	// authorized_keys.
	if len(authLine) > 0 && authLine[len(authLine)-1] == '\n' {
		authLine = authLine[:len(authLine)-1]
	}
	authLine = append(authLine, ' ', 'c', 'r', 'e', 'a', 't', 'e', 'o', 's', '-', 'c', 'l', 'i', '\n')
	if wErr := os.WriteFile(pub, authLine, 0o644); wErr != nil { // #nosec G306 -- pubkey is meant to be world-readable
		_ = os.Remove(priv) //nolint:errcheck
		return nil, fmt.Errorf("write %s: %w", pub, wErr)
	}
	return &Pair{PrivPath: priv, PubPath: pub, Managed: true}, nil
}

// ErrManagedKeyMissing is returned by ReadManaged when neither half of
// the managed key exists. Callers can branch on it to fall back to
// ResolveOrGenerate.
var ErrManagedKeyMissing = errors.New("managed key not present")
