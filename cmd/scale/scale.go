// Package scale provides the scale command for adjusting replicas and resources.
package scale

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// NewScaleCommand returns the scale command.
func NewScaleCommand() *cli.Command {
	return &cli.Command{
		Name:  "scale",
		Usage: "Adjust replicas and resources for a project environment",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
			&cli.IntFlag{Name: "replicas", Usage: "Number of replicas (1–3)"},
			&cli.IntFlag{Name: "cpu", Usage: "CPU in millicores (200–500)"},
			&cli.IntFlag{Name: "memory", Usage: "Memory in MB (500–1024)"},
			&cli.BoolFlag{Name: "show", Usage: "Show current scale settings without changing"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, envID, err := resolveProjectAndEnv(c, client)
			if err != nil {
				return err
			}

			if c.Bool("show") || (!c.IsSet("replicas") && !c.IsSet("cpu") && !c.IsSet("memory")) {
				return showScale(c, client, projectID, envID)
			}

			return updateScale(c, client, projectID, envID)
		},
	}
}

func resolveProjectAndEnv(c *cli.Context, client *api.APIClient) (string, string, error) {
	projectID := c.String("project")
	envID := c.String("environment")

	if projectID == "" || envID == "" {
		cfg, err := config.FindProjectConfig()
		if err != nil {
			return "", "", err
		}
		if cfg == nil {
			return "", "", fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify flags:\n    --project <id> --environment <id>")
		}
		if projectID == "" {
			projectID = cfg.ProjectID
		}
		if envID == "" {
			envID = cfg.EnvironmentID
		}
	}

	if envID == "" {
		envs, err := client.ListEnvironments(projectID)
		if err != nil {
			return "", "", err
		}
		if len(envs) == 0 {
			return "", "", fmt.Errorf("no environments found for this project")
		}
		if len(envs) == 1 {
			pterm.Println(pterm.Gray(fmt.Sprintf("  Using environment: %s", envs[0].DisplayName)))
			return projectID, envs[0].ID, nil
		}
		options := make([]string, len(envs))
		for i, e := range envs {
			options[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.Status)
		}
		selected, err := pterm.DefaultInteractiveSelect.
			WithOptions(options).
			WithDefaultText("Select an environment").
			Show()
		if err != nil {
			return "", "", fmt.Errorf("could not read selection: %w", err)
		}
		for i, opt := range options {
			if opt == selected {
				return projectID, envs[i].ID, nil
			}
		}
	}

	return projectID, envID, nil
}

func showScale(c *cli.Context, client *api.APIClient, projectID, envID string) error {
	resources, err := client.GetEnvironmentResources(projectID, envID)
	if err != nil {
		return err
	}

	if output.IsJSON(c) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resources)
	}

	cyan := pterm.NewStyle(pterm.FgCyan)
	fmt.Println()
	cyan.Println("  Current scale settings:")
	fmt.Println()
	fmt.Printf("    Replicas:  %d\n", resources.Replicas)
	fmt.Printf("    CPU:       %dm\n", resources.CPU)
	fmt.Printf("    Memory:    %dMB\n", resources.Memory)
	fmt.Println()
	pterm.Println(pterm.Gray("  Adjust with: createos scale --replicas 2 --cpu 300 --memory 512"))
	return nil
}

func updateScale(c *cli.Context, client *api.APIClient, projectID, envID string) error {
	// Get current settings
	current, err := client.GetEnvironmentResources(projectID, envID)
	if err != nil {
		return err
	}

	req := api.ScaleRequest{
		Replicas: current.Replicas,
		CPU:      current.CPU,
		Memory:   current.Memory,
	}

	if c.IsSet("replicas") {
		r := c.Int("replicas")
		if r < 1 || r > 3 {
			return fmt.Errorf("replicas must be between 1 and 3 (requested: %d)", r)
		}
		req.Replicas = r
	}
	if c.IsSet("cpu") {
		cpu := c.Int("cpu")
		if cpu < 200 || cpu > 500 {
			return fmt.Errorf("CPU must be between 200m and 500m (requested: %dm)", cpu)
		}
		req.CPU = cpu
	}
	if c.IsSet("memory") {
		mem := c.Int("memory")
		if mem < 500 || mem > 1024 {
			return fmt.Errorf("memory must be between 500MB and 1024MB (requested: %dMB)", mem)
		}
		req.Memory = mem
	}

	if err := client.UpdateEnvironmentResources(projectID, envID, req); err != nil {
		return err
	}

	if output.IsJSON(c) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(req)
	}

	fmt.Println()
	tableData := pterm.TableData{
		{"", "Before", "After"},
		{"Replicas", fmt.Sprintf("%d", current.Replicas), fmt.Sprintf("%d", req.Replicas)},
		{"CPU", fmt.Sprintf("%dm", current.CPU), fmt.Sprintf("%dm", req.CPU)},
		{"Memory", fmt.Sprintf("%dMB", current.Memory), fmt.Sprintf("%dMB", req.Memory)},
	}
	if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
		return err
	}
	fmt.Println()
	pterm.Success.Println("Scaling complete.")
	return nil
}
