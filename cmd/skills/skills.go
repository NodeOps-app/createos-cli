package skills

import (
	"github.com/urfave/cli/v2"
)

// NewSkillsCommand creates the skills command with subcommands
func NewSkillsCommand() *cli.Command {
	return &cli.Command{
		Name:  "skills",
		Usage: "Manage skills",
		Subcommands: []*cli.Command{
			newPurchasedCommand(),
		},
	}
}
