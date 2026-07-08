package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newPushCommand() *cli.Command {
	return &cli.Command{
		Name:      "push",
		Aliases:   []string{"upload", "cp-up"},
		Usage:     "Copy a local file into a sandbox",
		ArgsUsage: "<sandbox> <local-path> <remote-path>",
		Description: `Upload a file from your machine into a sandbox.

Examples:
  # Single file
  createos sandbox push my-box ./main.py /workspace/main.py

  # Stream a tarball from a directory; unpack inside the sandbox afterwards
  tar -c mydir | createos sandbox push my-box - /tmp/bundle.tar

Max 500 MB per file. The remote path must be absolute. Parent
directories are created automatically.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "stats",
				Usage: "Print elapsed time and average throughput after the upload",
			},
		},
		Action: runPush,
	}
}

func runPush(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	if len(args) < 3 {
		return fmt.Errorf("please provide <sandbox> <local-path> <remote-path>\n\n  Example:\n    createos sandbox push my-box ./main.py /workspace/main.py")
	}
	ref, local, remote := strings.TrimSpace(args[0]), args[1], args[2]
	if !strings.HasPrefix(remote, "/") {
		return fmt.Errorf("remote path must be absolute (got %q)\n\n  Example: /workspace/main.py", remote)
	}

	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}

	// Open the source: a real file (we know its size for Content-Length)
	// or stdin ("-") for piped uploads.
	var (
		src interface {
			Read(p []byte) (int, error)
		}
		size   int64
		label  string
		closer func() error
	)
	if local == "-" {
		src = os.Stdin
		size = 0
		label = "(stdin)"
		closer = func() error { return nil }
	} else {
		f, err := os.Open(local) // #nosec G304 -- local is a user-supplied source path
		if err != nil {
			return fmt.Errorf("could not open %s: %w", local, err)
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close() //nolint:errcheck
			return fmt.Errorf("could not stat %s: %w", local, err)
		}
		if info.IsDir() {
			_ = f.Close() //nolint:errcheck
			return fmt.Errorf("%s is a directory — push handles single files. Tar it first:\n  tar -c %s | createos sandbox push %s - /tmp/bundle.tar", local, local, ref)
		}
		src = f
		size = info.Size()
		label = local
		closer = f.Close
	}
	defer func() { _ = closer() }() //nolint:errcheck

	stats := c.Bool("stats")
	counted := &countingReader{r: src}
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Uploading %s → %s:%s", label, refLabel(ref, id), remote)) //nolint:errcheck
	start := time.Now()
	if err := client.UploadFile(c.Context, id, remote, counted, size); err != nil {
		spinner.Fail("Upload failed")
		return err
	}
	elapsed := time.Since(start)
	sent := size
	if sent == 0 {
		sent = counted.n // stdin path: use bytes actually read
	}
	if sent > 0 {
		spinner.Success(fmt.Sprintf("Uploaded %s → %s:%s (%s)", label, refLabel(ref, id), remote, humanBytes(sent)))
	} else {
		spinner.Success(fmt.Sprintf("Uploaded %s → %s:%s", label, refLabel(ref, id), remote))
	}
	if stats {
		pterm.Println(pterm.Gray(fmt.Sprintf("  %s in %s (%s)",
			humanBytes(sent), formatElapsed(elapsed), throughput(sent, elapsed))))
	}
	return nil
}

// countingReader wraps a Reader and counts bytes read — used to get a
// byte total when the source is stdin (unknown size up front).
type countingReader struct {
	r interface {
		Read(p []byte) (int, error)
	}
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Guard: countingReader satisfies io.Reader.
var _ io.Reader = (*countingReader)(nil)

// formatElapsed renders a duration compactly: "182ms", "1.4s", "2m03s".
func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", m, s)
	}
}

// throughput renders bytes/duration as MB/s or kB/s, ready to display.
func throughput(bytes int64, d time.Duration) string {
	if d <= 0 || bytes <= 0 {
		return "n/a"
	}
	bps := float64(bytes) / d.Seconds()
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f kB/s", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// humanBytes renders a size like "4.2 MB" — small, no units library.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
