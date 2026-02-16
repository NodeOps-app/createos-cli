package root

import (
	"github.com/NodeOps-app/createos-cli/cmd/auth"
	"github.com/NodeOps-app/createos-cli/cmd/skills"
	"github.com/urfave/cli/v2"
)

// NewApp creates and configures the root CLI application
func NewApp() *cli.App {
	app := &cli.App{
		Name:    "createos",
		Usage:   "CreateOS CLI - Manage your infrastructure",
		Version: "1.0.0",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug mode",
				EnvVars: []string{"CREATEOS_DEBUG"},
			},
		},
		Commands: []*cli.Command{
			auth.NewLoginCommand(),
			auth.NewLogoutCommand(),
			skills.NewSkillsCommand(),
		},
	}

	return app
}
