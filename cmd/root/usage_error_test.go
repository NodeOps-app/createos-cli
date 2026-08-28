package root

import (
	"errors"
	"os"
	"strings"
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

// newUnknownCommandTestApp builds a minimal command tree with the same
// shape as the real one (a root with subcommands, one of which is itself a
// group) and wires it through installCommandSuggestions exactly as
// root.NewApp does. It skips root.NewApp itself because that app's Before
// hook requires a signed-in session, which would make these tests depend on
// local auth state instead of on the routing bug being fixed.
func newUnknownCommandTestApp() *cli.App {
	noop := func(_ *cli.Context) error { return nil }
	app := &cli.App{
		Name:   "createos",
		Action: noop, // stands in for root.go's intro action
		Commands: []*cli.Command{
			{
				Name: "sandbox",
				Subcommands: []*cli.Command{
					{Name: "offload", Action: noop},
					{Name: "list", Action: noop},
				},
			},
			{Name: "login", Action: noop},
		},
	}
	installCommandSuggestions(app)
	return app
}

// TestUnknownCommandFailsTheRun pins the routing fix: urfave's own
// CommandNotFoundFunc can print a message but, per ShowCommandHelp in the
// library's help.go, can never make app.Run return an error — so both a
// root-level typo and a nested one used to print a suggestion and still
// exit 0. Every case here must return a non-nil error, since main.go's
// only success/failure signal is whether app.Run returned one.
func TestUnknownCommandFailsTheRun(t *testing.T) {
	t.Run("root typo suggests the real command", func(t *testing.T) {
		err := newUnknownCommandTestApp().Run([]string{"createos", "sandox"})
		if err == nil {
			t.Fatal("want an error for an unknown top-level command")
		}
		if !strings.Contains(err.Error(), "createos sandbox") {
			t.Errorf("error must suggest sandbox, got: %v", err)
		}
	})

	t.Run("nested typo suggests the real subcommand", func(t *testing.T) {
		err := newUnknownCommandTestApp().Run([]string{"createos", "sandbox", "ofload"})
		if err == nil {
			t.Fatal("want an error for an unknown subcommand")
		}
		if !strings.Contains(err.Error(), "createos sandbox offload") {
			t.Errorf("error must suggest sandbox offload, got: %v", err)
		}
	})

	t.Run("gibberish gets an error but no false suggestion", func(t *testing.T) {
		err := newUnknownCommandTestApp().Run([]string{"createos", "zzzzqqq"})
		if err == nil {
			t.Fatal("want an error for an unknown top-level command")
		}
		if strings.Contains(err.Error(), "Did you mean") {
			t.Errorf("must not guess a suggestion for gibberish input, got: %v", err)
		}
	})
}

// TestKnownCommandsStillDispatch guards against the fix being too broad: it
// must fail unresolved names, not every group invocation.
func TestKnownCommandsStillDispatch(t *testing.T) {
	if err := newUnknownCommandTestApp().Run([]string{"createos", "sandbox", "list"}); err != nil {
		t.Errorf("known subcommand must still dispatch normally, got: %v", err)
	}
	if err := newUnknownCommandTestApp().Run([]string{"createos", "sandbox"}); err != nil {
		t.Errorf("a bare group with no subcommand must show help, not fail, got: %v", err)
	}
	if err := newUnknownCommandTestApp().Run([]string{"createos"}); err != nil {
		t.Errorf("no arguments at all must still run the root action, got: %v", err)
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
