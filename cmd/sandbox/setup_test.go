package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInsertOpenCodePluginCreatesConfigFromNothing(t *testing.T) {
	out, changed, err := insertOpenCodePlugin("", `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil || !changed {
		t.Fatalf("insert = (%v, %v), want (true, nil)", changed, err)
	}
	if !strings.Contains(out, `"plugins"`) || !strings.Contains(out, "/p/opencode-plugin") {
		t.Fatalf("fresh config missing the plugin:\n%s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("fresh config is not valid JSON: %v\n%s", err, out)
	}
}

func TestInsertOpenCodePluginIsIdempotent(t *testing.T) {
	existing := "{\n  \"plugins\": [\n    \"/p/opencode-plugin\"\n  ]\n}\n"
	out, changed, err := insertOpenCodePlugin(existing, `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a config that already names the plugin must not change")
	}
	if out != existing {
		t.Fatalf("content changed:\n%s", out)
	}
}

func TestInsertOpenCodePluginKeepsExistingEntriesAndComments(t *testing.T) {
	existing := "{\n  // my notes\n  \"plugins\": [\"./other\"],\n  \"model\": \"x\"\n}\n"
	out, changed, err := insertOpenCodePlugin(existing, `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil || !changed {
		t.Fatalf("insert = (%v, %v), want (true, nil)", changed, err)
	}
	for _, want := range []string{"// my notes", `"./other"`, "/p/opencode-plugin", `"model"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q:\n%s", want, out)
		}
	}
	// The new entry must be separated from the one already there.
	if strings.Contains(out, `"/p/opencode-plugin""./other"`) {
		t.Fatalf("entries were not comma separated:\n%s", out)
	}
}

func TestInsertOpenCodePluginAddsKeyWhenAbsent(t *testing.T) {
	out, changed, err := insertOpenCodePlugin("{\n  \"model\": \"x\"\n}\n", `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil || !changed {
		t.Fatalf("insert = (%v, %v), want (true, nil)", changed, err)
	}
	var parsed struct {
		Model   string   `json:"model"`
		Plugins []string `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, out)
	}
	if parsed.Model != "x" {
		t.Fatalf("existing key lost: %#v", parsed)
	}
	if len(parsed.Plugins) != 1 || parsed.Plugins[0] != "/p/opencode-plugin" {
		t.Fatalf("plugins = %#v", parsed.Plugins)
	}
}

func TestInsertOpenCodePluginIntoEmptyArray(t *testing.T) {
	out, changed, err := insertOpenCodePlugin("{\n  \"plugins\": []\n}\n", `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil || !changed {
		t.Fatalf("insert = (%v, %v), want (true, nil)", changed, err)
	}
	var parsed struct {
		Plugins []string `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, out)
	}
	if len(parsed.Plugins) != 1 {
		t.Fatalf("plugins = %#v", parsed.Plugins)
	}
}

func TestInsertOpenCodePluginIgnoresThePluginsWordInProse(t *testing.T) {
	// "plugins" appears in a comment but is not a key; the entry must still
	// land in a real plugins array rather than be spliced into the comment.
	existing := "{\n  // plugins are configured below\n  \"model\": \"x\"\n}\n"
	out, changed, err := insertOpenCodePlugin(existing, `"/p/opencode-plugin"`, "/p/opencode-plugin")
	if err != nil || !changed {
		t.Fatalf("insert = (%v, %v), want (true, nil)", changed, err)
	}
	if !strings.Contains(out, "// plugins are configured below") {
		t.Fatalf("comment was damaged:\n%s", out)
	}
	if !openCodePluginsKey.MatchString(out) {
		t.Fatalf("no plugins key was added:\n%s", out)
	}
}

func TestOpenCodeEntryLocalIsAPlainPath(t *testing.T) {
	entry, err := openCodeEntry("/p/opencode-plugin", "local", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if entry != `"/p/opencode-plugin"` {
		t.Fatalf("entry = %s", entry)
	}
}

func TestOpenCodeEntryRemoteCarriesOptions(t *testing.T) {
	entry, err := openCodeEntry("/p/opencode-plugin", "remote", "s-2vcpu-2gb", "devbox:1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Package string            `json:"package"`
		Options map[string]string `json:"options"`
	}
	if err := json.Unmarshal([]byte(entry), &parsed); err != nil {
		t.Fatalf("entry is not valid JSON: %v\n%s", err, entry)
	}
	if parsed.Package != "/p/opencode-plugin" {
		t.Fatalf("package = %q", parsed.Package)
	}
	if parsed.Options["mode"] != "remote" || parsed.Options["shape"] != "s-2vcpu-2gb" || parsed.Options["rootfs"] != "devbox:1" {
		t.Fatalf("options = %#v", parsed.Options)
	}
}

func TestOpenCodeEntryRemoteOmitsUnsetOptions(t *testing.T) {
	entry, err := openCodeEntry("/p/opencode-plugin", "remote", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry, "shape") || strings.Contains(entry, "rootfs") {
		t.Fatalf("unset options must not be written: %s", entry)
	}
}

func TestDSHNodeOK(t *testing.T) {
	cases := map[string]bool{
		"v22.19.0": true,
		"v22.20.1": true,
		"v24.0.0":  true,
		"v25.1.0":  true,
		"22.19":    true,
		"v22.18.0": false,
		"v23.5.0":  false,
		"v20.11.0": false,
		"v22":      false,
		"":         false,
		"banana":   false,
	}
	for version, want := range cases {
		if got := dshNodeOK(version); got != want {
			t.Errorf("dshNodeOK(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestSetupAlreadyDoneRecognisesHostPhrasing(t *testing.T) {
	for _, out := range []string{
		"Error: marketplace 'createos' already exists",
		"plugin already installed",
		"ALREADY ADDED",
	} {
		if !setupAlreadyDone(out) {
			t.Errorf("setupAlreadyDone(%q) = false, want true", out)
		}
	}
	for _, out := range []string{"network unreachable", "permission denied", ""} {
		if setupAlreadyDone(out) {
			t.Errorf("setupAlreadyDone(%q) = true, want false", out)
		}
	}
}

func TestSetupCommandCoversEveryDocumentedIntegration(t *testing.T) {
	// The subcommand names are the contract with
	// https://createos.sh/docs/Sandbox/Integrations — a host listed there
	// without a setup is the gap this test exists to catch.
	want := []string{
		"claude-code",
		"codex",
		"deepseek",
		"herdr",
		"opencode",
		"orca",
		"pi",
	}
	got := map[string]bool{}
	for _, sub := range newSetupCommand().Subcommands {
		got[sub.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("createos sandbox setup %s is missing", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("setup has %d subcommands, want %d", len(got), len(want))
	}
}
