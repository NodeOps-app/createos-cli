// Package skills provides skills management commands.
package skills

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/ui"
)

func newCatalog() *cli.Command {
	return &cli.Command{
		Name:  "catalog",
		Usage: "Browse and purchase skills from the catalog",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			const pageSize = 20
			skills, pagination, err := client.ListAvailableSkillsForPurchase("", 0, pageSize)
			if err != nil {
				return err
			}

			purchasedIDs := make(map[string]string)
			if purchased, err := client.ListMyPurchasedSkills(); err == nil {
				for _, item := range purchased {
					purchasedIDs[item.SkillID] = item.ID
				}
			}

			return ui.RunCatalogList(skills, pagination, 0, "", purchasedIDs, client)
		},
	}
}
