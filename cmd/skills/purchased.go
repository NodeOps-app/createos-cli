package skills

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/ui"
	"github.com/urfave/cli/v2"
)

func newPurchasedCommand() *cli.Command {
	return &cli.Command{
		Name:  "purchased",
		Usage: "List all purchased skills",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.ApiClient)
			if !ok {
				return fmt.Errorf("not logged in, run 'createos login' first")
			}

			items, err := client.ListMyPurchasedSkills()
			if err != nil {
				return err
			}

			if len(items) == 0 {
				fmt.Println("No purchased skills found.")
				return nil
			}

			return ui.RunSkillsList(items, client)
		},
	}
}
