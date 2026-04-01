package env

import (
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// resolveProjectEnv resolves the project ID and environment from flags or .createos.json.
func resolveProjectEnv(c *cli.Context, client *api.APIClient) (string, *api.Environment, error) {
	projectID := c.String("project")
	envID := c.String("environment")

	if projectID == "" || envID == "" {
		cfg, err := config.FindProjectConfig()
		if err != nil {
			return "", nil, err
		}
		if cfg == nil {
			return "", nil, fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify flags:\n    --project <id> --environment <id>")
		}
		if projectID == "" {
			projectID = cfg.ProjectID
		}
		if envID == "" {
			envID = cfg.EnvironmentID
		}
	}

	// If still no environment, resolve it interactively
	if envID == "" {
		envs, err := client.ListEnvironments(projectID)
		if err != nil {
			return "", nil, err
		}
		if len(envs) == 0 {
			return "", nil, fmt.Errorf("no environments found for this project")
		}
		if len(envs) == 1 {
			return projectID, &envs[0], nil
		}
		options := make([]string, len(envs))
		for i, e := range envs {
			options[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.Status)
		}
		if !terminal.IsInteractive() {
			return "", nil, fmt.Errorf("multiple environments found — use --environment <id> to specify one\n\n  To see your environments, run:\n    createos environments list")
		}
		selected, err := pterm.DefaultInteractiveSelect.
			WithOptions(options).
			WithDefaultText("Select an environment").
			Show()
		if err != nil {
			return "", nil, fmt.Errorf("could not read selection: %w", err)
		}
		for i, opt := range options {
			if opt == selected {
				return projectID, &envs[i], nil
			}
		}
		return "", nil, fmt.Errorf("no environment selected")
	}

	// envID was known — fetch the full object so callers have UniqueName etc.
	envs, err := client.ListEnvironments(projectID)
	if err != nil {
		return "", nil, err
	}
	for i := range envs {
		if envs[i].ID == envID {
			return projectID, &envs[i], nil
		}
	}
	return "", nil, fmt.Errorf("environment %q not found", envID)
}

// ensureEnvGitignored adds .env.* to .gitignore if not already covered.
func ensureEnvGitignored() {
	const pattern = ".env.*"
	const gitignore = ".gitignore"

	data, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return
		}
	}

	var content string
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		content = "\n" + pattern + "\n"
	} else {
		content = pattern + "\n"
	}

	f, err := os.OpenFile(gitignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	_, _ = f.WriteString(content)

	pterm.Println(pterm.Gray("  Added .env.* to .gitignore"))
}
