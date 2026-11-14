package sandbox

import (
	"flag"
	"testing"
	"time"

	"github.com/urfave/cli/v2"
)

// newComputerTestContext builds a context the way urfave hands one to an
// Action: flags declared, but parsing stopped at the first positional. Every
// argument after the sandbox reference therefore arrives unparsed.
func newComputerTestContext(args ...string) *cli.Context {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("screen", "screen-0", "")
	set.String("out", defaultScreenshotPath, "")
	set.Duration("wait", 2*time.Minute, "")
	_ = set.Parse(args)
	return cli.NewContext(cli.NewApp(), set, nil)
}

// Nobody types flags before the positional argument, and urfave stops parsing
// at the first one. Without the hand re-scan, `screenshot my-box --out shot.png`
// writes to the default path and reports success, which is worse than an error.
func TestParseComputerArgsReadsFlagsAfterPositional(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantRef    string
		wantScreen string
		wantOut    string
		wantRest   []string
	}{
		{
			name:       "out after the reference",
			args:       []string{"my-box", "--out", "shot.png"},
			wantRef:    "my-box",
			wantScreen: "screen-0",
			wantOut:    "shot.png",
		},
		{
			name:       "equals form",
			args:       []string{"my-box", "--out=shot.png", "--screen=screen-2"},
			wantRef:    "my-box",
			wantScreen: "screen-2",
			wantOut:    "shot.png",
		},
		{
			name:       "short screen alias after the reference",
			args:       []string{"my-box", "-S", "screen-1"},
			wantRef:    "my-box",
			wantScreen: "screen-1",
			wantOut:    defaultScreenshotPath,
		},
		{
			name:       "flags before the reference still work",
			args:       []string{"--screen", "screen-3", "my-box"},
			wantRef:    "my-box",
			wantScreen: "screen-3",
			wantOut:    defaultScreenshotPath,
		},
		{
			name:       "operands survive around a flag",
			args:       []string{"my-box", "640", "--screen", "screen-1", "400"},
			wantRef:    "my-box",
			wantScreen: "screen-1",
			wantOut:    defaultScreenshotPath,
			wantRest:   []string{"640", "400"},
		},
		{
			name:       "no arguments at all",
			args:       nil,
			wantRef:    "",
			wantScreen: "screen-0",
			wantOut:    defaultScreenshotPath,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseComputerArgs(newComputerTestContext(tc.args...))
			if got.ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", got.ref, tc.wantRef)
			}
			if got.screen != tc.wantScreen {
				t.Errorf("screen = %q, want %q", got.screen, tc.wantScreen)
			}
			if got.out != tc.wantOut {
				t.Errorf("out = %q, want %q", got.out, tc.wantOut)
			}
			if len(got.rest) != len(tc.wantRest) {
				t.Fatalf("rest = %v, want %v", got.rest, tc.wantRest)
			}
			for i := range got.rest {
				if got.rest[i] != tc.wantRest[i] {
					t.Errorf("rest[%d] = %q, want %q", i, got.rest[i], tc.wantRest[i])
				}
			}
		})
	}
}

// `sandbox desktop` reads --wait through the same parser, so a timeout written
// after the sandbox reference has to survive.
func TestParseComputerArgsReadsWait(t *testing.T) {
	got := parseComputerArgs(newComputerTestContext("my-box", "--wait", "30s"))
	if got.wait != 30*time.Second {
		t.Errorf("wait = %s, want 30s", got.wait)
	}

	got = parseComputerArgs(newComputerTestContext("my-box", "--wait=90s"))
	if got.wait != 90*time.Second {
		t.Errorf("wait = %s, want 90s", got.wait)
	}

	// An unparseable duration keeps the declared default rather than zeroing
	// the timeout, which would turn the readiness wait into a single attempt.
	got = parseComputerArgs(newComputerTestContext("my-box", "--wait", "soon"))
	if got.wait != 2*time.Minute {
		t.Errorf("wait = %s, want the 2m default", got.wait)
	}
}
