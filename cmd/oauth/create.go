// Package oauth provides OAuth client management commands.
package oauth

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create an OAuth client",
		Description: "Creates an OAuth client interactively, or non-interactively via flags.\n\n" +
			"   Non-interactive example:\n" +
			"     createos oauth-clients create \\\n" +
			"       --name \"My App\" \\\n" +
			"       --redirect-uri https://myapp.com/callback \\\n" +
			"       --app-url https://myapp.com \\\n" +
			"       --policy-url https://myapp.com/privacy \\\n" +
			"       --tos-url https://myapp.com/tos \\\n" +
			"       --logo-url https://myapp.com/logo.png",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "Client name"},
			&cli.StringSliceFlag{Name: "redirect-uri", Usage: "Redirect URI (repeatable)"},
			&cli.BoolFlag{Name: "public", Usage: "Create a public client (no client secret)"},
			&cli.StringFlag{Name: "app-url", Usage: "Application URL"},
			&cli.StringFlag{Name: "policy-url", Usage: "Privacy policy URL"},
			&cli.StringFlag{Name: "tos-url", Usage: "Terms of service URL"},
			&cli.StringFlag{Name: "logo-url", Usage: "Logo URL"},
		},
		Action: func(c *cli.Context) error {
			client, err := getClient(c)
			if err != nil {
				return err
			}

			existingClients, err := client.ListOAuthClients()
			if err != nil {
				return err
			}
			if len(existingClients) >= 4 {
				return fmt.Errorf("you already have the maximum number of OAuth clients (4)\n\n  To continue, delete one through the dashboard or reuse an existing client")
			}

			isInteractive := terminal.IsInteractive()
			hasFlags := c.IsSet("name") || c.IsSet("redirect-uri")

			var name string
			var redirectURIs []string
			var public bool
			var clientURI, policyURI, tosURI, logoURI string

			if isInteractive && !hasFlags {
				name, err = promptRequiredText("Client name", validateClientName)
				if err != nil {
					return err
				}

				redirectURIs, err = promptRedirectURIs()
				if err != nil {
					return err
				}

				public, err = pterm.DefaultInteractiveConfirm.
					WithDefaultText("Should this be a public client? Public clients do not use a client secret").
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read client type: %w", err)
				}

				clientURI, err = promptRequiredText("Application URL", validateURI)
				if err != nil {
					return err
				}
				policyURI, err = promptRequiredText("Privacy policy URL", validateURI)
				if err != nil {
					return err
				}
				tosURI, err = promptRequiredText("Terms of service URL", validateURI)
				if err != nil {
					return err
				}
				logoURI, err = promptRequiredText("Logo URL", validateURI)
				if err != nil {
					return err
				}

				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Create this OAuth client now?").
					WithDefaultValue(true).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your OAuth client was not created.")
					return nil
				}
			} else {
				name = strings.TrimSpace(c.String("name"))
				if err := validateClientName(name); err != nil {
					return fmt.Errorf("--name: %w", err)
				}

				redirectURIs = c.StringSlice("redirect-uri")
				if len(redirectURIs) == 0 {
					return fmt.Errorf("at least one --redirect-uri is required")
				}
				for _, u := range redirectURIs {
					if err := validateURI(u); err != nil {
						return fmt.Errorf("--redirect-uri %q: %w", u, err)
					}
				}

				public = c.Bool("public")

				clientURI = c.String("app-url")
				if err := validateURI(clientURI); err != nil {
					return fmt.Errorf("--app-url: %w", err)
				}
				policyURI = c.String("policy-url")
				if err := validateURI(policyURI); err != nil {
					return fmt.Errorf("--policy-url: %w", err)
				}
				tosURI = c.String("tos-url")
				if err := validateURI(tosURI); err != nil {
					return fmt.Errorf("--tos-url: %w", err)
				}
				logoURI = c.String("logo-url")
				if err := validateURI(logoURI); err != nil {
					return fmt.Errorf("--logo-url: %w", err)
				}
			}

			clientID, err := client.CreateOAuthClient(api.CreateOAuthClientInput{
				Name:         name,
				RedirectUris: redirectURIs,
				Public:       public,
				URI:          clientURI,
				PolicyURI:    policyURI,
				TOSURI:       tosURI,
				LogoURI:      logoURI,
			})
			if err != nil {
				return err
			}

			pterm.Success.Printf("OAuth client %q has been created.\n", name)

			detail, err := client.GetOAuthClient(clientID)
			if err == nil && detail != nil {
				printInstructions(c.String("api-url"), detail)
			}

			return nil
		},
	}
}
