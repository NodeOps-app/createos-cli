package root

import (
	"errors"
	"os"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestUndefinedFlagName(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("flag provided but not defined: -o"), "-o"},
		{errors.New("flag provided but not defined: --output"), "--output"},
		{errors.New("something else entirely"), ""},
	} {
		if got := undefinedFlagName(tc.err); got != tc.want {
			t.Errorf("undefinedFlagName(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// TestCorrectedCommandLine covers the payoff: the message has to hand back
// a line the user can paste, which means moving the flag AND its value.
func TestCorrectedCommandLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		flag string
		want string
	}{
		{
			"separate value moves with the flag",
			[]string{"sandbox", "shapes", "-o", "json"},
			"-o",
			"createos -o json sandbox shapes",
		},
		{
			"joined value",
			[]string{"sandbox", "shapes", "--output=json"},
			"--output",
			"createos --output=json sandbox shapes",
		},
		{
			"boolean flag has no value to move",
			[]string{"sandbox", "ls", "--debug"},
			"--debug",
			"createos --debug sandbox ls",
		},
		{
			"flag already first is left alone",
			[]string{"-o", "json", "sandbox", "shapes"},
			"-o",
			"createos -o json sandbox shapes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := os.Args
			os.Args = append([]string{"createos"}, tc.args...)
			t.Cleanup(func() { os.Args = original })

			if got := correctedCommandLine(tc.flag); got != tc.want {
				t.Errorf("correctedCommandLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNearestCommand pins both halves: a near miss gets a suggestion, and
// a wild guess gets silence. Suggesting something unrelated is worse than
// suggesting nothing.
func TestNearestCommand(t *testing.T) {
	commands := []*cli.Command{
		{Name: "shell", Aliases: []string{"sh"}},
		{Name: "fork"},
		{Name: "exec"},
		{Name: "hidden", Hidden: true},
	}
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"ssh", "shell"},  // one edit from the "sh" alias
		{"forkk", "fork"}, // one extra character
		{"exe", "exec"},   // one missing character
		{"zzzzqqq", ""},   // nothing close — stay quiet
		{"hidden", ""},    // hidden commands are not suggested
	} {
		if got := nearestCommand(commands, tc.input); got != tc.want {
			t.Errorf("nearestCommand(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"fork", "fork", 0},
		{"forkk", "fork", 1},
		{"ssh", "sh", 1},
		{"exec", "", 4},
	} {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
