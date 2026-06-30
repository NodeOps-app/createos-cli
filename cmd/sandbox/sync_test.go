package sandbox

import (
	"reflect"
	"testing"
	"time"

	"github.com/urfave/cli/v2"
)

func TestSyncModeToMutagen(t *testing.T) {
	ok := map[string]string{
		"":         "two-way-safe",
		"two-way":  "two-way-safe",
		"TWO-WAY":  "two-way-safe",
		" mirror ": "one-way-replica",
		"one-way":  "one-way-safe",
		"mirror":   "one-way-replica",
	}
	for in, want := range ok {
		got, err := syncModeToMutagen(in)
		if err != nil {
			t.Errorf("syncModeToMutagen(%q) unexpected err: %v", in, err)
		}
		if got != want {
			t.Errorf("syncModeToMutagen(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := syncModeToMutagen("sideways"); err == nil {
		t.Error(`syncModeToMutagen("sideways") expected error, got nil`)
	}
}

func TestMutagenCreateArgs(t *testing.T) {
	// mirror mode + ignore-vcs + exclude (blank patterns dropped).
	got := mutagenCreateArgs("sess1", "one-way-replica", "/local", "u@h:22:/r", true, []string{"*.log", " ", "node_modules"})
	want := []string{
		"sync", "create",
		"--name=sess1",
		"--sync-mode=one-way-replica",
		"--ignore-vcs",
		"--ignore=*.log",
		"--ignore=node_modules",
		"/local", "u@h:22:/r",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mutagenCreateArgs mirror:\n got=%v\nwant=%v", got, want)
	}

	// no ignore-vcs => flag omitted; source/target stay the final two args.
	got = mutagenCreateArgs("s", "two-way-safe", "/l", "/r", false, nil)
	want = []string{"sync", "create", "--name=s", "--sync-mode=two-way-safe", "/l", "/r"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mutagenCreateArgs no-vcs:\n got=%v\nwant=%v", got, want)
	}
	if got[len(got)-2] != "/l" || got[len(got)-1] != "/r" {
		t.Errorf("source/target must be last two args, got %v", got)
	}
}

// runParse builds a cli context with the real sync command's flags, runs
// it with argv, and returns the parsed syncOptions.
func runParse(t *testing.T, argv ...string) syncOptions {
	t.Helper()
	var got syncOptions
	app := &cli.App{
		Commands: []*cli.Command{{
			Name:  "sync",
			Flags: newSyncCommand().Flags,
			Action: func(c *cli.Context) error {
				got = parseSyncArgs(c)
				return nil
			},
		}},
	}
	if err := app.Run(append([]string{"app", "sync"}, argv...)); err != nil {
		t.Fatalf("run %v: %v", argv, err)
	}
	return got
}

// The blocker: urfave/cli v2 drops flags after the first positional.
// parseSyncArgs must recover every flag placed after the sandbox ref.
func TestParseSyncArgs_FlagsAfterPositional(t *testing.T) {
	o := runParse(t, "my-box",
		"--mode", "mirror",
		"--exclude", "*.log", "--exclude", "node_modules",
		"--quiet", "--no-ignore-vcs",
		"--remote", "/root/work")

	if o.ref != "my-box" {
		t.Errorf("ref = %q, want my-box", o.ref)
	}
	if o.mode != "mirror" {
		t.Errorf("mode = %q, want mirror", o.mode)
	}
	if !o.quiet {
		t.Error("quiet = false, want true")
	}
	if !o.noIgnoreVCS {
		t.Error("noIgnoreVCS = false, want true")
	}
	if o.remote != "/root/work" {
		t.Errorf("remote = %q, want /root/work", o.remote)
	}
	if !reflect.DeepEqual(o.exclude, []string{"*.log", "node_modules"}) {
		t.Errorf("exclude = %v, want [*.log node_modules]", o.exclude)
	}
}

// Short aliases and --flag=value form, also after the positional.
func TestParseSyncArgs_ShortAndInlineAfterPositional(t *testing.T) {
	o := runParse(t, "my-box", "-i", "/k/id", "--mode=one-way", "-q", "-y", "-u", "app")
	if o.ref != "my-box" {
		t.Errorf("ref = %q, want my-box", o.ref)
	}
	if o.identity != "/k/id" {
		t.Errorf("identity = %q, want /k/id", o.identity)
	}
	if o.user != "app" {
		t.Errorf("user = %q, want app", o.user)
	}
	if o.mode != "one-way" {
		t.Errorf("mode = %q, want one-way", o.mode)
	}
	if !o.quiet || !o.assumeYes {
		t.Errorf("quiet=%v assumeYes=%v, want both true", o.quiet, o.assumeYes)
	}
}

// Flags before the positional (the only form urfave handles natively)
// must keep working.
func TestParseSyncArgs_FlagsBeforePositional(t *testing.T) {
	o := runParse(t, "--mode", "mirror", "--exclude", "*.tmp", "my-box")
	if o.ref != "my-box" {
		t.Errorf("ref = %q, want my-box", o.ref)
	}
	if o.mode != "mirror" {
		t.Errorf("mode = %q, want mirror", o.mode)
	}
	if !reflect.DeepEqual(o.exclude, []string{"*.tmp"}) {
		t.Errorf("exclude = %v, want [*.tmp]", o.exclude)
	}
}

// Defaults survive when no flags are given.
func TestParseSyncArgs_Defaults(t *testing.T) {
	o := runParse(t, "my-box")
	if o.mode != "two-way" {
		t.Errorf("mode = %q, want two-way (flag default)", o.mode)
	}
	if o.sshdWait != 5*time.Second {
		t.Errorf("sshdWait = %v, want 5s (flag default)", o.sshdWait)
	}
	if o.quiet || o.force || o.assumeYes || o.noIgnoreVCS {
		t.Errorf("bool flags should default false, got %+v", o)
	}
}
