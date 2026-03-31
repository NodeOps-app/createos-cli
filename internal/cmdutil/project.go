// Package cmdutil provides shared CLI utilities.
package cmdutil

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/config"
)

// ResolveProjectID returns the explicit project ID or falls back to the linked project config.
func ResolveProjectID(projectID string) (string, error) {
	if projectID != "" {
		return projectID, nil
	}

	cfg, err := config.FindProjectConfig()
	if err != nil {
		return "", err
	}
	if cfg == nil || cfg.ProjectID == "" {
		return "", fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    <project-id>")
	}

	return cfg.ProjectID, nil
}

// ResolveProjectScopedArg resolves a command that accepts either:
//   - <project-id> <resource-id>
//   - <resource-id> when the current directory is linked to a project
func ResolveProjectScopedArg(args []string, resourceLabel string) (string, string, error) {
	switch len(args) {
	case 0:
		return "", "", fmt.Errorf("please provide %s", resourceLabel)
	case 1:
		projectID, err := ResolveProjectID("")
		if err != nil {
			return "", "", err
		}
		return projectID, args[0], nil
	default:
		return args[0], args[1], nil
	}
}
