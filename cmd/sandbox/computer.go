package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// `createos sandbox computer` drives the desktop inside a sandbox: look at it,
// point at it, type into it. Pair it with `createos sandbox desktop`, which
// brings the desktop up and hands a human the link to watch.
//
// Coordinates are raw X11 pixels of the target screen. Nothing scales them for
// DPI anywhere along this path, so read the bounds from `computer screen`
// rather than assuming a resolution.

// defaultScreenshotPath is where a capture lands when no -o is given.
const defaultScreenshotPath = "screenshot.png"

func newComputerCommand() *cli.Command {
	return &cli.Command{
		Name:  "computer",
		Usage: "Control the desktop inside a sandbox",
		Description: `Look at and control a sandbox's desktop — take a screenshot, move and
click the pointer, type, press keys, open a page.

Start the desktop first:

    createos sandbox desktop <sandbox>

Take a screenshot before and after anything you click. Nothing here
confirms that a click landed on what you meant, so a screenshot is the
only way to see what actually happened.`,
		Subcommands: []*cli.Command{
			newComputerScreenshotCommand(),
			newComputerInfoCommand("screen", "Show the screen's size in pixels"),
			newComputerInfoCommand("cursor", "Show where the pointer is"),
			newComputerInfoCommand("windows", "List the windows on screen"),
			newComputerMoveCommand(),
			newComputerClickCommand(),
			newComputerTypeCommand(),
			newComputerKeyCommand(),
			newComputerOpenCommand(),
			newComputerRawCommand(),
		},
	}
}

// screenFlag is repeated per subcommand rather than set on the group: urfave
// does not pass a parent group's flags down to its subcommands.
//
// The flag is declared so it shows up in help and parses in the position
// urfave expects; parseComputerArgs is what actually reads it, because urfave
// stops parsing flags at the first positional argument.
func screenFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "screen",
		Aliases: []string{"S"},
		Usage:   "Which screen to act on",
		Value:   api.DefaultComputerScreen,
	}
}

// outFlag names the file a screenshot is written to. It deliberately has no
// short alias: `-o` is already the global output-format flag.
func outFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "out",
		Usage: "Where to save the picture",
		Value: defaultScreenshotPath,
	}
}

// computerArgs is one subcommand's arguments after the flags have been pulled
// out of them, wherever the caller happened to put them.
type computerArgs struct {
	ref    string
	screen string
	out    string
	rest   []string
}

// parseComputerArgs re-scans the raw arguments for this package's flags.
//
// urfave/cli v2 stops parsing flags at the first positional argument, so
// `computer screenshot my-box --out shot.png` silently drops --out and writes
// to the default path. Nobody types the flags first, so the arguments are
// scanned by hand — the same workaround `sandbox edit` already makes for
// --ingress.
func parseComputerArgs(c *cli.Context) computerArgs {
	parsed := computerArgs{screen: c.String("screen"), out: c.String("out")}
	args := c.Args().Slice()

	for i := 0; i < len(args); i++ {
		a := args[i]
		take := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--screen" || a == "-S":
			if v := take(); v != "" {
				parsed.screen = v
			}
		case strings.HasPrefix(a, "--screen="):
			parsed.screen = strings.TrimPrefix(a, "--screen=")
		case strings.HasPrefix(a, "-S="):
			parsed.screen = strings.TrimPrefix(a, "-S=")
		case a == "--out":
			if v := take(); v != "" {
				parsed.out = v
			}
		case strings.HasPrefix(a, "--out="):
			parsed.out = strings.TrimPrefix(a, "--out=")
		default:
			parsed.rest = append(parsed.rest, a)
		}
	}

	if len(parsed.rest) > 0 {
		parsed.ref = strings.TrimSpace(parsed.rest[0])
		parsed.rest = parsed.rest[1:]
	}
	if strings.TrimSpace(parsed.screen) == "" {
		parsed.screen = api.DefaultComputerScreen
	}
	if strings.TrimSpace(parsed.out) == "" {
		parsed.out = defaultScreenshotPath
	}
	return parsed
}

// computerTarget resolves the shared preamble of every op: the client, the
// sandbox the positional ref names, and the arguments left over for the op.
func computerTarget(c *cli.Context) (*api.SandboxClient, string, computerArgs, error) {
	args := parseComputerArgs(c)
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, "", args, fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	if args.ref == "" {
		return nil, "", args, fmt.Errorf("please provide a sandbox ID or name\n\n  To see your sandboxes, run:\n    createos sandbox list")
	}
	id, err := resolveSandboxRef(c.Context, client, args.ref)
	if err != nil {
		return nil, "", args, err
	}
	return client, id, args, nil
}

func newComputerScreenshotCommand() *cli.Command {
	return &cli.Command{
		Name:      "screenshot",
		Usage:     "Save a picture of the screen",
		ArgsUsage: "<sandbox>",
		Flags:     []cli.Flag{screenFlag(), outFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			png, err := client.ComputerScreenshot(c.Context, id, args.screen)
			if err != nil {
				return err
			}
			path := args.out
			if err := os.WriteFile(path, png, 0o600); err != nil {
				return fmt.Errorf("couldn't save the picture to %s: %w", path, err)
			}
			output.Render(c, map[string]any{"path": path, "bytes": len(png)}, func() {
				pterm.Success.Printfln("Saved a picture of the screen to %s (%d bytes)", path, len(png))
			})
			return nil
		},
	}
}

// newComputerInfoCommand builds the read-only ops, which differ only in which
// route they read and what they are called.
func newComputerInfoCommand(name, usage string) *cli.Command {
	return &cli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "<sandbox>",
		Flags:     []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			switch name {
			case "screen":
				geom, err := client.ComputerScreen(c.Context, id, args.screen)
				if err != nil {
					return err
				}
				output.Render(c, geom, func() {
					pterm.Printfln("%s %d × %d pixels", pterm.NewStyle(pterm.FgCyan).Sprint("Screen:"), geom.Width, geom.Height)
				})
			case "cursor":
				pos, err := client.ComputerCursor(c.Context, id, args.screen)
				if err != nil {
					return err
				}
				output.Render(c, pos, func() {
					pterm.Printfln("%s %d, %d", pterm.NewStyle(pterm.FgCyan).Sprint("Pointer:"), pos.X, pos.Y)
				})
			case "windows":
				raw, err := client.ComputerWindows(c.Context, id, args.screen)
				if err != nil {
					return err
				}
				printRaw(c, raw)
			}
			return nil
		},
	}
}

func newComputerMoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "move",
		Usage:     "Move the pointer somewhere",
		ArgsUsage: "<sandbox> <x> <y>",
		Flags:     []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			if len(args.rest) < 2 {
				return fmt.Errorf("a move needs two whole numbers — how far across and how far down\n\n  For example:\n    createos sandbox computer move %s 640 400", args.ref)
			}
			x, y, err := coords(args.rest[0], args.rest[1], "move")
			if err != nil {
				return err
			}
			if err := client.ComputerMouseMove(c.Context, id, args.screen, x, y); err != nil {
				return err
			}
			pterm.Success.Printfln("Moved the pointer to %d, %d", x, y)
			return nil
		},
	}
}

func newComputerClickCommand() *cli.Command {
	return &cli.Command{
		Name:      "click",
		Usage:     "Click, optionally somewhere specific",
		ArgsUsage: "<sandbox> [<x> <y>]",
		Description: `With no coordinates this clicks wherever the pointer already is.

Take a screenshot first to see what you are about to click, and
another afterwards to confirm it did what you expected.`,
		Flags: []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			var at *api.ComputerCursorPos
			switch len(args.rest) {
			case 0:
			case 1:
				return fmt.Errorf("a click needs both an across and a down position\n\n  For example:\n    createos sandbox computer click %s 640 400", args.ref)
			default:
				x, y, err := coords(args.rest[0], args.rest[1], "click")
				if err != nil {
					return err
				}
				at = &api.ComputerCursorPos{X: x, Y: y}
			}
			if err := client.ComputerMouseClick(c.Context, id, args.screen, at); err != nil {
				return err
			}
			if at != nil {
				pterm.Success.Printfln("Clicked at %d, %d", at.X, at.Y)
			} else {
				pterm.Success.Println("Clicked where the pointer was")
			}
			return nil
		},
	}
}

func newComputerTypeCommand() *cli.Command {
	return &cli.Command{
		Name:      "type",
		Usage:     "Type text into whatever has focus",
		ArgsUsage: "<sandbox> <text>",
		Description: `Quote the text to keep it as one piece:

    createos sandbox computer type my-box "hello world"

Unquoted words are joined with single spaces.`,
		Flags: []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			// Unquoted multi-word text arrives as separate arguments; join it
			// back up so `type my-box hello world` types the space too.
			text := strings.Join(args.rest, " ")
			if text == "" {
				return fmt.Errorf("please provide the text to type\n\n  For example:\n    createos sandbox computer type %s \"hello world\"", args.ref)
			}
			if err := client.ComputerType(c.Context, id, args.screen, text); err != nil {
				return err
			}
			pterm.Success.Printfln("Typed %d characters", len([]rune(text)))
			return nil
		},
	}
}

func newComputerKeyCommand() *cli.Command {
	return &cli.Command{
		Name:      "key",
		Usage:     "Press keys together",
		ArgsUsage: "<sandbox> <key>...",
		Description: `Every key is pressed at the same time, so this is how you send a
shortcut:

    createos sandbox computer key my-box ctrl l`,
		Flags: []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			keys := args.rest
			if len(keys) == 0 {
				return fmt.Errorf("please provide at least one key to press\n\n  For example:\n    createos sandbox computer key %s ctrl l", args.ref)
			}
			if err := client.ComputerPress(c.Context, id, args.screen, keys); err != nil {
				return err
			}
			pterm.Success.Printfln("Pressed %s", strings.Join(keys, "+"))
			return nil
		},
	}
}

func newComputerOpenCommand() *cli.Command {
	return &cli.Command{
		Name:      "open",
		Usage:     "Open a web page or file on the desktop",
		ArgsUsage: "<sandbox> <url-or-path>",
		Flags:     []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			target := ""
			if len(args.rest) > 0 {
				target = strings.TrimSpace(args.rest[0])
			}
			if target == "" {
				return fmt.Errorf("please provide something to open\n\n  For example:\n    createos sandbox computer open %s https://example.com", args.ref)
			}
			if err := client.ComputerOpen(c.Context, id, args.screen, target); err != nil {
				return err
			}
			pterm.Success.Printfln("Opened %s", target)
			return nil
		},
	}
}

func newComputerRawCommand() *cli.Command {
	return &cli.Command{
		Name:      "raw",
		Usage:     "Call a desktop endpoint this CLI doesn't wrap",
		ArgsUsage: "<sandbox> <method> <path> [json-body]",
		Description: `An escape hatch for the parts of the desktop API without their own
command. The path is relative to the sandbox's computer routes:

    createos sandbox computer raw my-box GET screen`,
		Hidden: true,
		Flags:  []cli.Flag{screenFlag()},
		Action: func(c *cli.Context) error {
			client, id, args, err := computerTarget(c)
			if err != nil {
				return err
			}
			method, path := "", ""
			if len(args.rest) > 0 {
				method = strings.TrimSpace(args.rest[0])
			}
			if len(args.rest) > 1 {
				path = strings.TrimSpace(args.rest[1])
			}
			if method == "" || path == "" {
				return fmt.Errorf("please provide a method and a path\n\n  For example:\n    createos sandbox computer raw %s GET screen", args.ref)
			}
			var body json.RawMessage
			if raw := func() string {
				if len(args.rest) > 2 {
					return strings.TrimSpace(args.rest[2])
				}
				return ""
			}(); raw != "" {
				if !json.Valid([]byte(raw)) {
					return fmt.Errorf("the body isn't valid JSON")
				}
				body = json.RawMessage(raw)
			}
			out, err := client.ComputerRaw(c.Context, id, args.screen, method, path, body)
			if err != nil {
				return err
			}
			printRaw(c, out)
			return nil
		},
	}
}

// coords parses an x/y pair, naming the op in the error so the fix is obvious.
func coords(xs, ys, op string) (int, int, error) {
	x, errX := strconv.Atoi(strings.TrimSpace(xs))
	y, errY := strconv.Atoi(strings.TrimSpace(ys))
	if errX != nil || errY != nil {
		return 0, 0, fmt.Errorf("a %s needs two whole numbers — how far across and how far down\n\n  To see the screen's size, run:\n    createos sandbox computer screen <sandbox>", op)
	}
	return x, y, nil
}

// printRaw emits a passthrough payload: as-is under -o json, pretty otherwise.
func printRaw(c *cli.Context, raw json.RawMessage) {
	if output.IsJSON(c) {
		fmt.Println(string(raw))
		return
	}
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			fmt.Println(string(formatted))
			return
		}
	}
	fmt.Println(string(raw))
}
