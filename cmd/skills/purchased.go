package skills

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/ui"
)

func newPurchasedCommand() *cli.Command {
	return &cli.Command{
		Name:  "purchased",
		Usage: "List all purchased skills",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			items, err := client.ListMyPurchasedSkills()
			if err != nil {
				return err
			}

			if len(items) == 0 {
				fmt.Println("You haven't purchased any skills yet. Browse the catalog with 'createos skills catalog'.")
				return nil
			}

			return ui.RunSkillsList(items, client)
		},
	}
}
