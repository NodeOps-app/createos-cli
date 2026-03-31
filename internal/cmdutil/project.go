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
		return "", fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    --project <project-id>")
	}

	return cfg.ProjectID, nil
}
