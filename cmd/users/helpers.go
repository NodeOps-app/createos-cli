package users

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/urfave/cli/v2"
)

func getClient(c *cli.Context) (*api.ApiClient, error) {
	client, ok := c.App.Metadata[api.ClientKey].(*api.ApiClient)
	if !ok {
		return nil, fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	return client, nil
}
