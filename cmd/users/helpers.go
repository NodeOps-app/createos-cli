// Package users provides user account commands.
package users

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func getClient(c *cli.Context) (*api.APIClient, error) {
	client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
	if !ok {
		return nil, fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	return client, nil
}
