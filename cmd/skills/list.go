package skills

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List all available skills",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "filter",
				Aliases: []string{"f"},
				Usage:   "Filter skills by name or tag",
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"l"},
				Usage:   "Limit number of results",
				Value:   100,
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Show detailed information",
			},
		},
		Action: func(c *cli.Context) error {
			filter := c.String("filter")
			limit := c.Int("limit")
			verbose := c.Bool("verbose")

			fmt.Printf("Listing skills (limit: %d)...\n", limit)
			if filter != "" {
				fmt.Printf("Filter: %s\n", filter)
			}

			// TODO: Implement actual listing logic
			if verbose {
				fmt.Println("\nDetailed skill information:")
			}
			fmt.Println("- skill-1")
			fmt.Println("- skill-2")
			fmt.Println("- skill-3")

			return nil
		},
	}
}
