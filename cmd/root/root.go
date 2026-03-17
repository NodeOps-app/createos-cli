// Package root wires together the CLI application.
package root

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/cmd/auth"
	"github.com/NodeOps-app/createos-cli/cmd/deployments"
	"github.com/NodeOps-app/createos-cli/cmd/domains"
	"github.com/NodeOps-app/createos-cli/cmd/environments"
	"github.com/NodeOps-app/createos-cli/cmd/oauth"
	"github.com/NodeOps-app/createos-cli/cmd/projects"
	"github.com/NodeOps-app/createos-cli/cmd/skills"
	"github.com/NodeOps-app/createos-cli/cmd/users"
	versioncmd "github.com/NodeOps-app/createos-cli/cmd/version"
	"github.com/NodeOps-app/createos-cli/cmd/vms"
	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/intro"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
)

// NewApp creates and configures the root CLI application.
func NewApp() *cli.App {
	app := &cli.App{
		Name:    "createos",
		Usage:   "CreateOS CLI - Manage your infrastructure",
		Version: version.Version,
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
			cmd := c.Args().First()
			if cmd == "" || cmd == "login" || cmd == "logout" || cmd == "version" {
				return nil
			}

			// Try OAuth session first
			if config.HasOAuthSession() {
				session, err := config.LoadOAuthSession()
				if err != nil {
					return fmt.Errorf("could not load your session: %w", err)
				}
				if session != nil {
					// Auto-refresh if expired
					if config.IsTokenExpired(session) {
						tokenEndpoint := session.TokenEndpoint
						if tokenEndpoint == "" {
							tokenEndpoint = config.OAuthIssuerURL + "/oauth2/token"
						}
						refreshed, err := internaloauth.RefreshTokens(
							tokenEndpoint,
							config.OAuthClientID,
							session.RefreshToken,
						)
						if err != nil {
							return fmt.Errorf("your session has expired and could not be renewed — run 'createos login' to sign in again")
						}
						session.AccessToken = refreshed.AccessToken
						if refreshed.RefreshToken != "" {
							session.RefreshToken = refreshed.RefreshToken
						}
						if refreshed.ExpiresIn > 0 {
							session.ExpiresAt = time.Now().Unix() + int64(refreshed.ExpiresIn)
						}
						if err := config.SaveOAuthSession(*session); err != nil {
							return fmt.Errorf("could not save refreshed session: %w", err)
						}
					}
					client := api.NewClientWithAccessToken(session.AccessToken, c.String("api-url"), c.Bool("debug"))
					c.App.Metadata[api.ClientKey] = &client
					return nil
				}
			}

			// Fall back to API key
			token, err := config.LoadToken()
			if err != nil {
				return err
			}
			client := api.NewClient(token, c.String("api-url"), c.Bool("debug"))
			c.App.Metadata[api.ClientKey] = &client
			return nil
		},
		Action: func(_ *cli.Context) error {
			intro.Show()

			fmt.Println("Available Commands:")
			if config.IsLoggedIn() {
				fmt.Println("  deployments    Manage project deployments")
				fmt.Println("  domains        Manage custom domains")
				fmt.Println("  environments   Manage project environments")
				fmt.Println("  logout         Sign out from CreateOS")
				fmt.Println("  oauth          Manage OAuth clients")
				fmt.Println("  projects       Manage projects")
				fmt.Println("  skills         Manage skills")
				fmt.Println("  users          Manage your user account")
				fmt.Println("  vms            Manage VM terminal instances")
				fmt.Println("  whoami         Show the currently authenticated user")
			} else {
				fmt.Println("  login          Authenticate with CreateOS")
			}
			fmt.Println("  version        Print the current version")
			fmt.Println()
			fmt.Println("Run 'createos <command> --help' for more information on a command.")

			return nil
		},
		Commands: []*cli.Command{
			auth.NewLoginCommand(),
			auth.NewLogoutCommand(),
			deployments.NewDeploymentsCommand(),
			domains.NewDomainsCommand(),
			environments.NewEnvironmentsCommand(),
			oauth.NewOAuthCommand(),
			projects.NewProjectsCommand(),
			skills.NewSkillsCommand(),
			users.NewUsersCommand(),
			vms.NewVMsCommand(),
			auth.NewWhoamiCommand(),
			versioncmd.NewVersionCommand(),
		},
	}

	return app
}
