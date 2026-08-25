package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A version gate that reads "0.10.0" as older than "0.7.5" would refuse to
// install on a Herdr that is in fact newer, so compare numerically per part.
func TestHerdrVersionLess(t *testing.T) {
	older := []string{"0.7.4", "0.6.9", "0.0.1", "0.7", "0"}
	for _, v := range older {
		if !herdrVersionLess(v, herdrMinVersion) {
			t.Errorf("herdrVersionLess(%q, %q) = false, want true", v, herdrMinVersion)
		}
	}
	newer := []string{"0.7.5", "0.7.6", "0.8.2", "0.10.0", "1.0.0", "0.8.2-rc1"}
	for _, v := range newer {
		if herdrVersionLess(v, herdrMinVersion) {
			t.Errorf("herdrVersionLess(%q, %q) = true, want false", v, herdrMinVersion)
		}
	}
}

// The keys land in a file the user owns and edits, so a second run must add
// nothing, and the first run must keep a copy of what was there before.
func TestHerdrWriteKeysIsIdempotentAndBacksUp(t *testing.T) {
	root := t.TempDir()
	pluginConfigDir := filepath.Join(root, "plugins", "config", herdrPluginID)
	if err := os.MkdirAll(pluginConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.toml")
	original := "[[keys.command]]\nkey = \"prefix+z\"\ntype = \"noop\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	added, path, err := herdrWriteKeys(pluginConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if path != configPath {
		t.Fatalf("wrote %q, want %q", path, configPath)
	}
	if added != len(herdrKeys) {
		t.Fatalf("added %d bindings, want %d", added, len(herdrKeys))
	}

	backup, err := os.ReadFile(configPath + ".before-createos")
	if err != nil {
		t.Fatalf("no backup written: %v", err)
	}
	if string(backup) != original {
		t.Error("backup does not match the file that was replaced")
	}

	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "prefix+z") {
		t.Error("the user's own binding was dropped")
	}
	for _, k := range herdrKeys {
		if !strings.Contains(string(updated), herdrPluginID+"."+k.action) {
			t.Errorf("binding for %s is missing", k.action)
		}
	}

	again, _, err := herdrWriteKeys(pluginConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("second run added %d bindings, want 0", again)
	}
}

// Every agent the flag accepts must be one the plugin can actually install.
func TestHerdrAgentNamesAreListed(t *testing.T) {
	names := herdrAgentNames()
	for agent := range herdrAgents {
		if !strings.Contains(names, agent) {
			t.Errorf("%q is accepted but not listed in the flag help", agent)
		}
	}
	if _, ok := herdrAgents[herdrDefaultAgent]; !ok {
		t.Errorf("the default agent %q is not a known agent", herdrDefaultAgent)
	}
}
