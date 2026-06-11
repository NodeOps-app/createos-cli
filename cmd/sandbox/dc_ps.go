package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/dclock"
)

// newDCPsCommand wires `createos sandbox dc ps`.
//
// Reads .createos/dc.lock for the sandbox id + locally-bound port map,
// then re-queries `docker compose ps --format json` inside the VM so
// the displayed status (and health) is fresh on every call.
//
// --json dumps the raw compose ps array for scripts.
func newDCPsCommand() *cli.Command {
	return &cli.Command{
		Name:  "ps",
		Usage: "List services running in the sandbox + their local URLs",
		Description: `Shows compose services running inside the sandbox plus the
'http://127.0.0.1:PORT' on your laptop that maps to each published
port (when 'dc up' is holding tunnels open in another terminal).

Examples:
  createos sb dc ps
  createos sb dc ps --json | jq .`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "Path to docker-compose.yml (default: ./docker-compose.yml)",
				Value:   "docker-compose.yml",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Emit raw 'docker compose ps' JSON (one object per service)",
			},
		},
		Action: runDCPs,
	}
}

// dcPsRow is the subset of `docker compose ps --format json` we render.
// Field names match docker's output verbatim — case matters.
type dcPsRow struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	Image   string `json:"Image"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health,omitempty"`
}

func runDCPs(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	_, lock, err := loadDCProject(c.String("file"))
	if err != nil {
		return err
	}
	raw, err := composePsRaw(c.Context, client, lock)
	if err != nil {
		return err
	}
	if c.Bool("json") {
		fmt.Println(raw)
		return nil
	}
	rows, err := parseDCPsRows(raw)
	if err != nil {
		return fmt.Errorf("parse compose ps output: %w (raw: %s)", err, truncate(raw, 200))
	}
	renderDCPs(rows, lock.Ports)
	return nil
}

// composePsRaw executes `docker compose ps --format json` in the VM
// and returns its raw stdout — either an NDJSON stream or a single
// JSON array, depending on the plugin version. parseDCPsRows handles
// both shapes.
func composePsRaw(ctx context.Context, client *api.SandboxClient, lock *dclock.Lock) (string, error) {
	resp, err := client.ExecSandbox(ctx, lock.SandboxID, api.SandboxExecReq{
		Cmd: "docker",
		Args: []string{
			"compose",
			"-p", lock.ProjectName,
			"-f", lock.ComposeFile,
			"ps",
			"--format", "json",
			"--all", // include stopped containers so the user sees crashes
		},
	})
	if err != nil {
		return "", err
	}
	if resp.Result.ExitCode != 0 {
		return "", fmt.Errorf("compose ps exit %d: %s", resp.Result.ExitCode, resp.Result.Stderr)
	}
	return resp.Result.Stdout, nil
}

func parseDCPsRows(raw string) ([]dcPsRow, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var rows []dcPsRow
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	// NDJSON: one JSON object per line.
	var rows []dcPsRow
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r dcPsRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// renderDCPs prints the compose-ps result as a pterm table. The PORTS
// column synthesises a "http://127.0.0.1:N" URL from the lockfile's
// port map (those are the ports `dc up` opened tunnels for; they're
// only LIVE when a `dc up` is holding foreground in another terminal).
func renderDCPs(rows []dcPsRow, ports []dclock.Port) {
	if len(rows) == 0 {
		pterm.Info.Println("No services found for this project.")
		return
	}
	portsByService := map[string][]dclock.Port{}
	for _, p := range ports {
		portsByService[p.Service] = append(portsByService[p.Service], p)
	}

	header := []string{"SERVICE", "STATE", "STATUS", "PORTS"}
	table := [][]string{header}
	for _, r := range rows {
		state := r.State
		if r.Health != "" {
			state = state + " (" + r.Health + ")"
		}
		table = append(table, []string{
			r.Service,
			state,
			truncate(r.Status, 40),
			formatPorts(portsByService[r.Service]),
		})
	}
	_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
	if len(ports) > 0 {
		pterm.Println()
		pterm.Info.Println("Tunnels are only live while a 'sb dc up' is holding foreground in another terminal.")
	}
}

func formatPorts(ps []dclock.Port) string {
	if len(ps) == 0 {
		return ""
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d → :%d", p.LocalPort, p.ContainerPort))
	}
	return strings.Join(out, ", ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// (time import kept for potential future use — uptime calculation off
// the State field when compose exposes a StartedAt timestamp.)
var _ = time.Now
