package sandbox

import (
	"os"
	"testing"
)

// urfave/cli stops parsing flags at the first positional argument, so a flag
// written after the sandbox name never reaches the cli.Context. The raw helpers
// recover it from os.Args. Values after "--" belong to the sandbox command and
// must never be read as flags.

func withArgs(t *testing.T, args []string) {
	t.Helper()
	saved := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = saved })
}

func TestRawProcessFlagValueAfterPositional(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "start", "my-box", "--cwd", "/workspace", "--", "claude"})
	if got := rawProcessFlagValue("start", "cwd"); got != "/workspace" {
		t.Fatalf("cwd = %q, want /workspace", got)
	}
}

func TestRawProcessFlagValueEqualsForm(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "start", "my-box", "--cwd=/workspace", "--", "claude"})
	if got := rawProcessFlagValue("start", "cwd"); got != "/workspace" {
		t.Fatalf("cwd = %q, want /workspace", got)
	}
}

func TestRawProcessFlagValueStopsAtDoubleDash(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "run", "my-box", "--", "sh", "-c", "--cwd", "/evil"})
	if got := rawProcessFlagValue("run", "cwd"); got != "" {
		t.Fatalf("cwd = %q, want empty: values after -- are the sandbox command", got)
	}
}

func TestRawProcessFlagValuesCollectsEveryOccurrence(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "start", "my-box", "--env", "A=1", "--env=B=2", "--", "claude"})
	got := rawProcessFlagValues("start", "env")
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("env = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("env = %v, want %v", got, want)
		}
	}
}

func TestRawProcessFlagValuesStopsAtDoubleDash(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "run", "my-box", "--env", "A=1", "--", "env", "--env", "B=2"})
	got := rawProcessFlagValues("run", "env")
	if len(got) != 1 || got[0] != "A=1" {
		t.Fatalf("env = %v, want [A=1]", got)
	}
}

func TestRawProcessFlagValuesEmptyWhenAbsent(t *testing.T) {
	withArgs(t, []string{"createos", "sandbox", "process", "start", "my-box", "--", "claude"})
	if got := rawProcessFlagValues("start", "env"); len(got) != 0 {
		t.Fatalf("env = %v, want empty", got)
	}
}

func TestSplitOnPTYControlInterrupt(t *testing.T) {
	before, action, after, found := splitOnPTYControl([]byte("abc\x03def"))
	if !found {
		t.Fatal("expected Ctrl-C control byte")
	}
	if string(before) != "abc" || action != "interrupt" || string(after) != "def" {
		t.Fatalf("before/action/after = %q/%q/%q, want abc/interrupt/def", before, action, after)
	}
}

func TestSplitOnPTYControlUsesFirstControlByte(t *testing.T) {
	before, action, after, found := splitOnPTYControl([]byte("abc\x03\x1d"))
	if !found {
		t.Fatal("expected control byte")
	}
	if string(before) != "abc" || action != "interrupt" || string(after) != "\x1d" {
		t.Fatalf("before/action/after = %q/%q/%q, want abc/interrupt/Ctrl-]", before, action, after)
	}

	before, action, after, found = splitOnPTYControl([]byte("abc\x1d\x03"))
	if !found {
		t.Fatal("expected control byte")
	}
	if string(before) != "abc" || action != "detach" || after != nil {
		t.Fatalf("before/action/after = %q/%q/%q, want abc/detach/nil", before, action, after)
	}
}

func TestSplitOnPTYControlNoControlByte(t *testing.T) {
	before, action, after, found := splitOnPTYControl([]byte("abc"))
	if found {
		t.Fatal("did not expect control byte")
	}
	if string(before) != "abc" || action != "" || after != nil {
		t.Fatalf("before/action/after = %q/%q/%q, want abc/empty/nil", before, action, after)
	}
}
