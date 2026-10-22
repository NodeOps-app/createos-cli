package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Sandbox names cap at 22 characters, and the same orb must always map to the
// same sandbox, or spawn after pause would create a second one.
func TestAmpSandboxNameIsShortAndStable(t *testing.T) {
	// printf %s amp-T-0001 | sha256sum | cut -c1-18. Pinned: changing the
	// scheme would orphan every paused orb that already exists.
	if name := ampSandboxName("amp-T-0001"); name != "amp-3a1cb29d7032a8429f" {
		t.Fatalf("ampSandboxName = %q, want amp-3a1cb29d7032a8429f", name)
	}
	name := ampSandboxName("amp-T-0001")
	if name != ampSandboxName("amp-T-0001") {
		t.Fatal("ampSandboxName is not deterministic")
	}
	if name == ampSandboxName("amp-T-0002") {
		t.Fatal("different orbs share a sandbox name")
	}
	long := ampSandboxName("amp-T-" + strings.Repeat("x", 200))
	if len(long) != 22 {
		t.Fatalf("long ORB_ID gave %d chars", len(long))
	}
}

// The wrapper is a shell script Amp executes. Shape and template are checked
// by ampFlagValueRE, and the binary path is quoted, so none of them can inject.
func TestAmpFlagValueRejectsShellSyntax(t *testing.T) {
	for _, bad := range []string{"", "a b", "$(id)", "x;id", "a'b", "`id`", strings.Repeat("a", 64)} {
		if ampFlagValueRE.MatchString(bad) {
			t.Errorf("ampFlagValueRE accepted %q", bad)
		}
	}
	for _, good := range []string{"s-4vcpu-8gb", "amp-orb", "devbox:1", "tpl_01ABC"} {
		if !ampFlagValueRE.MatchString(good) {
			t.Errorf("ampFlagValueRE rejected %q", good)
		}
	}
}

func TestAmpWrapperPassesExactArgv(t *testing.T) {
	dir := t.TempDir()
	// A binary path with a space and a quote, standing in for createos.
	self := filepath.Join(dir, "my 'bin'")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "wrapper")
	if err := os.WriteFile(wrapper, []byte(ampWrapperScript(self, "s-1vcpu-1gb", "amp-orb")), 0o700); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), wrapper).Output() // #nosec G204 -- test-owned path
	if err != nil {
		t.Fatalf("wrapper failed: %v", err)
	}
	want := "sandbox\nsetup\namp\n--dispatch\n--shape\ns-1vcpu-1gb\n--template\namp-orb\n"
	if string(out) != want {
		t.Fatalf("wrapper argv = %q, want %q", out, want)
	}
}

// The template builder rejects anything else at submit time.
func TestAmpDockerfileMeetsTemplateRules(t *testing.T) {
	if len(ampDockerfile) > 64<<10 {
		t.Fatalf("Dockerfile is %d bytes, over the 64 KiB limit", len(ampDockerfile))
	}
	froms := regexp.MustCompile(`(?mi)^FROM\s+(\S+)`).FindAllStringSubmatch(ampDockerfile, -1)
	if len(froms) != 1 || froms[0][1] != "nodeops/sandbox:debian" {
		t.Fatalf("want exactly one FROM nodeops/sandbox:debian, got %v", froms)
	}
	if regexp.MustCompile(`(?mi)^\s*(COPY|ADD)\s`).MatchString(ampDockerfile) {
		t.Fatal("COPY and ADD are not allowed in templates")
	}
}

// ampProvision and the Dockerfile install the same packages: the fallback
// rootfs must end up meeting the same contract as the template.
func TestAmpProvisionPackagesMatchDockerfile(t *testing.T) {
	pairs := regexp.MustCompile(`[a-z]+:([a-z-]+)`).FindAllStringSubmatch(
		ampProvision[strings.Index(ampProvision, "for pair in"):strings.Index(ampProvision, "; do")], -1)
	if len(pairs) == 0 {
		t.Fatal("found no packages in ampProvision")
	}
	for _, p := range pairs {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(p[1]) + `\b`).MatchString(ampDockerfile) {
			t.Errorf("ampProvision installs %s, the Dockerfile does not", p[1])
		}
	}
}

// A second job for the same orb waits for the first, and gives up at its
// deadline rather than running alongside it.
func TestAmpLockSerializesJobsForOneOrb(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unlock, err := ampLock(t.Context(), "amp-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 700*time.Millisecond)
	defer cancel()
	if _, err = ampLock(ctx, "amp-test"); err == nil {
		t.Fatal("second lock succeeded while the first was held")
	}
	other, err := ampLock(t.Context(), "amp-other")
	if err != nil {
		t.Fatalf("a different orb was blocked: %v", err)
	}
	other()
	unlock()
	again, err := ampLock(t.Context(), "amp-test")
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	again()
}
