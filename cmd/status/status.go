// Package status provides the status command for project health overview.
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// NewStatusCommand returns the status command.
func NewStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show a project's health and deployment status at a glance",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "project",
				Usage: "Project ID (auto-detected from .createos.json if not set)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.String("project")
			if projectID == "" {
				cfg, err := config.FindProjectConfig()
				if err != nil {
					return err
				}
				if cfg == nil {
					return fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    createos status --project <id>")
				}
				projectID = cfg.ProjectID
			}

			project, err := client.GetProject(projectID)
			if err != nil {
				return err
			}

			deployments, deplErr := client.ListDeployments(projectID)
			domains, domErr := client.ListDomains(projectID)
			environments, envErr := client.ListEnvironments(projectID)

			if output.IsJSON(c) {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"project":      project,
					"deployments":  deployments,
					"domains":      domains,
					"environments": environments,
				})
			}

			// Project header
			cyan := pterm.NewStyle(pterm.FgCyan)
			fmt.Println()
			pterm.DefaultHeader.WithFullWidth(false).Println(project.DisplayName)
			fmt.Println()

			cyan.Printf("  Status:       ")
			fmt.Println(statusIcon(project.Status) + " " + project.Status)

			// Show first environment URL
			if len(environments) > 0 && environments[0].Extra.Endpoint != "" {
				cyan.Printf("  URL:          ")
				fmt.Println(environments[0].Extra.Endpoint)
			}

			cyan.Printf("  Type:         ")
			fmt.Println(project.Type)

			cyan.Printf("  Created:      ")
			fmt.Println(relativeTime(project.CreatedAt))

			// Warn about any fetch failures
			if envErr != nil {
				pterm.Println(pterm.Yellow("  ⚠ Could not fetch environments"))
			}
			if deplErr != nil {
				pterm.Println(pterm.Yellow("  ⚠ Could not fetch deployments"))
			}
			if domErr != nil {
				pterm.Println(pterm.Yellow("  ⚠ Could not fetch domains"))
			}

			// Environments
			if len(environments) > 0 {
				fmt.Println()
				cyan.Println("  Environments:")
				for _, env := range environments {
					branch := ""
					if env.Branch != nil && *env.Branch != "" {
						branch = " (branch: " + *env.Branch + ")"
					}
					fmt.Printf("    %s %s — %s%s\n", statusIcon(env.Status), env.DisplayName, env.Status, branch)
					if env.Extra.Endpoint != "" {
						pterm.Println(pterm.Gray("      " + env.Extra.Endpoint))
					}
				}
			}

			// Active domains — paired with their environment URL
			domainEnvEndpoint := map[string]string{}
			for _, env := range environments {
				for _, d := range env.Extra.CustomDomains {
					domainEnvEndpoint[d] = env.Extra.Endpoint
				}
			}
			var activeDomains []api.Domain
			for _, d := range domains {
				if d.Status == "active" {
					activeDomains = append(activeDomains, d)
				}
			}
			if len(activeDomains) > 0 {
				fmt.Println()
				cyan.Println("  Domains:")
				for _, d := range activeDomains {
					if envURL := domainEnvEndpoint[d.Name]; envURL != "" {
						fmt.Printf("    %s %s | %s\n", pterm.Green("✓"), envURL, d.Name)
					} else {
						fmt.Printf("    %s %s\n", pterm.Green("✓"), d.Name)
					}
				}
			}

			// Recent deployments
			if len(deployments) > 0 {
				fmt.Println()
				cyan.Println("  Recent deploys:")
				limit := len(deployments)
				if limit > 5 {
					limit = 5
				}
				for _, d := range deployments[:limit] {
					icon := deployStatusIcon(d.Status)
					fmt.Printf("    %s  v%d  %s  %s\n", icon, d.VersionNumber, relativeTime(d.CreatedAt), d.Status)
				}
			}

			fmt.Println()
			return nil
		},
	}
}

func statusIcon(status string) string {
	switch status {
	case "active", "running", "healthy":
		return pterm.Green("●")
	case "sleeping", "paused", "pending":
		return pterm.Yellow("●")
	case "failed", "error", "stopped":
		return pterm.Red("●")
	default:
		return pterm.Gray("●")
	}
}


func deployStatusIcon(status string) string {
	switch status {
	case "successful", "running", "active", "deployed":
		return pterm.Green("✓")
	case "building", "deploying", "pending":
		return pterm.Yellow("●")
	default:
		return pterm.Red("✗")
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
