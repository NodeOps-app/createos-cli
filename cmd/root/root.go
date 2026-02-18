package root

import (
	"fmt"

	"github.com/NodeOps-app/createos-cli/cmd/auth"
	"github.com/NodeOps-app/createos-cli/cmd/skills"
	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/intro"
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
			&cli.StringFlag{
				Name:    "api-url",
				Usage:   "Override the API base URL",
				EnvVars: []string{"CREATEOS_API_URL"},
				Value:   api.DefaultBaseURL,
			},
		},
		Before: func(c *cli.Context) error {
			// skip client init for login/logout — no token needed
			cmd := c.Args().First()
			if cmd == "login" || cmd == "logout" {
				return nil
			}

			token, err := config.LoadToken()
			if err != nil {
				return err
			}

			client := api.NewClient(token, c.String("api-url"), c.Bool("debug"))
			c.App.Metadata[api.ClientKey] = &client
			return nil
		},
		Action: func(c *cli.Context) error {
			intro.Show()

			fmt.Println("Available Commands:")
			if config.IsLoggedIn() {
				fmt.Println("  logout         Sign out from CreateOS")
				fmt.Println("  whoami         Show the currently authenticated user")
				fmt.Println("  skills         Manage skills")
			} else {
				fmt.Println("  login          Authenticate with CreateOS")
			}
			fmt.Println()
			fmt.Println("Run 'createos <command> --help' for more information on a command.")

			return nil
		},
		Commands: []*cli.Command{
			auth.NewLoginCommand(),
			auth.NewLogoutCommand(),
			auth.NewWhoamiCommand(),
			skills.NewSkillsCommand(),
		},
	}

	return app
}
