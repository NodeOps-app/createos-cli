package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// The guest agent listens on loopback inside every sandbox. Loopback-only
// is the whole security model: nothing outside the sandbox can reach it,
// so it needs no credential — and a sandbox can only ever signal itself.
const (
	selfSignalAddr = "127.0.0.1:1029"
	selfFifoPath   = "/run/self"
	selfDialWait   = 2 * time.Second
)

// selfSignalAddrForTest is the address selfSignalHTTP dials. It is a
// variable purely so a test can point it at a stub agent; nothing else
// ever reassigns it.
var selfSignalAddrForTest = selfSignalAddr

func newSelfCommand() *cli.Command {
	return &cli.Command{
		Name:  "self",
		Usage: "Pause or delete the sandbox this command is running inside",
		Description: `Self-signal lets a workload end its own sandbox from the inside.

Run these INSIDE a sandbox, not on your laptop. There is no sandbox id to
pass and no API key involved: the agent listens on loopback only, so the
only sandbox you can signal is the one you are in.

Use it when a job knows it is finished long before anything outside does —
a batch run, a CI job, a one-shot agent task. The machine is released the
moment the last line executes, with no polling loop and no credential
inside the sandbox.

Examples:
  # Park this sandbox; resume it later from outside
  createos sandbox self pause --reason job-complete

  # Destroy this sandbox. Irreversible.
  createos sandbox self delete

Without this CLI, the same signals are one line each:
  curl -X POST http://127.0.0.1:1029/self/pause
  echo park > /run/self`,
		Subcommands: []*cli.Command{
			{
				Name:   "pause",
				Usage:  "Pause this sandbox, keeping its disk and memory",
				Flags:  selfFlags(),
				Action: runSelf("pause"),
			},
			{
				Name:    "delete",
				Aliases: []string{"destroy", "rm"},
				Usage:   "Destroy this sandbox. Irreversible",
				Flags: append(selfFlags(), &cli.BoolFlag{
					Name:    "force",
					Aliases: []string{"f", "yes", "y"},
					Usage:   "Skip the confirmation prompt",
				}),
				Action: runSelf("delete"),
			},
		},
	}
}

func selfFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "reason",
			Usage: "Free-text label recorded with the signal (truncated to 128 characters)",
		},
	}
}

func runSelf(action string) cli.ActionFunc {
	return func(c *cli.Context) error {
		// Destroying a sandbox cannot be undone, and this command is most
		// often typed inside a shell on a box someone is still using.
		if action == "delete" && !c.Bool("force") {
			if !terminal.IsInteractive() {
				return errors.New(
					"deleting this sandbox is irreversible — pass --force to confirm\n\n  Example:\n    createos sandbox self delete --force")
			}
			ok, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText("Destroy this sandbox? Everything on it is lost").
				Show()
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Cancelled. Nothing changed.")
				return nil
			}
		}

		reason := strings.TrimSpace(c.String("reason"))
		if err := sendSelfSignal(c.Context, action, reason); err != nil {
			return err
		}

		if output.IsJSON(c) {
			output.Render(c, map[string]any{"status": "accepted", "action": action, "reason": reason}, func() {})
			return nil
		}
		switch action {
		case "pause":
			pterm.Success.Println("Pause accepted. This sandbox is being snapshotted.")
			fmt.Println("    Bring it back from outside with: createos sandbox resume <id>")
		default:
			pterm.Success.Println("Delete accepted. This sandbox is going away.")
		}
		return nil
	}
}

// sendSelfSignal delivers one signal to the guest agent, preferring HTTP
// and falling back to the FIFO.
//
// Both surfaces exist because the FIFO works in images with no curl and
// no working loopback HTTP stack. Trying HTTP first keeps the useful part
// of the failure — the agent answers with a status code — and only drops
// to the pipe, which is fire-and-forget, when HTTP is not there at all.
func sendSelfSignal(ctx context.Context, action, reason string) error {
	httpErr := selfSignalHTTP(ctx, action, reason)
	if httpErr == nil {
		return nil
	}
	if fifoErr := selfSignalFIFO(action); fifoErr == nil {
		return nil
	}
	return notInsideSandboxError(action, httpErr)
}

func selfSignalHTTP(ctx context.Context, action, reason string) error {
	endpoint := "http://" + selfSignalAddrForTest + "/self/" + action
	if reason != "" {
		endpoint += "?reason=" + url.QueryEscape(reason)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: selfDialWait}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // status code is what matters here
	// The agent answers 202 Accepted and then acts. Anything else means
	// something is listening on that port that is not the guest agent.
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the agent on %s answered %s", selfSignalAddrForTest, resp.Status)
	}
	return nil
}

// selfSignalFIFO writes one verb to /run/self. The pipe takes no reason,
// so a reason given on the command line is dropped on this path.
func selfSignalFIFO(action string) error {
	verb := "park"
	if action == "delete" {
		verb = "retire"
	}
	f, err := os.OpenFile(selfFifoPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // the write error below is the one that matters
	_, err = f.WriteString(verb + "\n")
	return err
}

// notInsideSandboxError is the message for the most likely mistake:
// running this on a laptop. Naming the alternative matters, because the
// command that does work from outside takes a sandbox id and this one
// does not.
func notInsideSandboxError(action string, cause error) error {
	var opErr *net.OpError
	inside := "no agent is listening"
	if errors.As(cause, &opErr) || strings.Contains(cause.Error(), "connection refused") {
		inside = "nothing answered on " + selfSignalAddr
	}
	outside := "pause"
	if action == "delete" {
		outside = "rm --force"
	}
	return fmt.Errorf(
		"could not signal this sandbox — %s\n\n  'sandbox self' only works INSIDE a sandbox.\n  From your own machine, name the sandbox instead:\n    createos sandbox %s <sandbox>\n\n  Underlying error: %w",
		inside, outside, cause)
}
