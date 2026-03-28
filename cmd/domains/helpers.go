package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

// resolveDomain resolves projectID and domainID from flags, args, or interactively.
func resolveDomain(c *cli.Context, client *api.APIClient) (string, string, error) {
	args := c.Args().Slice()

	// --domain flag takes priority
	if domainID := c.String("domain"); domainID != "" {
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		return projectID, domainID, nil
	}

	switch len(args) {
	case 0:
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		domainID, err := pickDomain(client, projectID)
		if err != nil {
			return "", "", err
		}
		return projectID, domainID, nil
	case 1:
		projectID, err := cmdutil.ResolveProjectID(c.String("project"))
		if err != nil {
			return "", "", err
		}
		return projectID, args[0], nil
	default:
		return args[0], args[1], nil
	}
}

func pickDomain(client *api.APIClient, projectID string) (string, error) {
	domains, err := client.ListDomains(projectID)
	if err != nil {
		return "", err
	}
	if len(domains) == 0 {
		return "", fmt.Errorf("no domains found for this project")
	}
	if len(domains) == 1 {
		pterm.Println(pterm.Gray(fmt.Sprintf("  Using domain: %s (%s)", domains[0].Name, domains[0].Status)))
		return domains[0].ID, nil
	}

	options := make([]string, len(domains))
	for i, d := range domains {
		options[i] = fmt.Sprintf("%s  %s", d.Name, d.Status)
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select a domain").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return domains[i].ID, nil
		}
	}
	return "", fmt.Errorf("no domain selected")
}

// resolveEnvironmentForDomain returns an environment ID from flag or interactive select.
func resolveEnvironmentForDomain(c *cli.Context, client *api.APIClient, projectID string) (string, error) {
	if envID := c.String("environment"); envID != "" {
		return envID, nil
	}
	return pickEnvironment(client, projectID)
}

// pickEnvironment shows a required interactive environment selector.
func pickEnvironment(client *api.APIClient, projectID string) (string, error) {
	envs, err := client.ListEnvironments(projectID)
	if err != nil {
		return "", err
	}
	if len(envs) == 0 {
		return "", fmt.Errorf("no environments found — deploy your project first before adding a domain")
	}
	if len(envs) == 1 {
		pterm.Println(pterm.Gray(fmt.Sprintf("  Linking to environment: %s", envs[0].DisplayName)))
		return envs[0].ID, nil
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
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return envs[i].ID, nil
		}
	}
	return "", fmt.Errorf("no environment selected")
}
