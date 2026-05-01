package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"

	"github.com/denisbrodbeck/machineid"
)

// ResolveDistinctID returns a stable anonymous identifier for this machine.
//
// Resolution order (per spec §7.1):
//  1. machineid.ProtectedID("createos-cli") — HMAC-SHA256 over the OS machine ID.
//  2. sha256(hostname + "|" + username) — fallback for containers / locked-down envs.
//  3. "" — give up (caller should treat empty distinct_id as a no-op).
//
// The result is NEVER cached to disk — recompute each run.
func ResolveDistinctID() string {
	if id, err := machineid.ProtectedID("createos-cli"); err == nil && id != "" {
		return id
	}

	host, _ := os.Hostname()
	username := ""
	if u, err := user.Current(); err == nil && u != nil {
		username = u.Username
	}
	if host == "" && username == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(host + "|" + username))
	return hex.EncodeToString(sum[:])
}
