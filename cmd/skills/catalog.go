package skills

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/ui"
	"github.com/urfave/cli/v2"
)

func newCatalog() *cli.Command {
	return &cli.Command{
		Name:  "catalog",
		Usage: "Browse and purchase skills from the catalog",
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.ApiClient)
			if !ok {
				return fmt.Errorf("not logged in, run 'createos login' first")
			}

			const pageSize = 20
			skills, pagination, err := client.ListAvailableSkillsForPurchase("", 0, pageSize)
			if err != nil {
				return err
			}

			purchasedIds := make(map[string]string)
			if purchased, err := client.ListMyPurchasedSkills(); err == nil {
				for _, item := range purchased {
					purchasedIds[item.SkillId] = item.Id
				}
			}

			return ui.RunCatalogList(skills, pagination, 0, "", purchasedIds, client)
		},
	}
}
