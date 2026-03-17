package oauth

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create an OAuth client",
		Description: "Starts an interactive flow to create an OAuth client.\n\n" +
			"   You'll be asked for the client name, redirect URIs, visibility, and policy URLs.",
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

			name, err := promptRequiredText("Client name", validateClientName)
			if err != nil {
				return err
			}

			redirectURIs, err := promptRedirectURIs()
			if err != nil {
				return err
			}

			public, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText("Should this be a public client? Public clients do not use a client secret").
				WithDefaultValue(false).
				Show()
			if err != nil {
				return fmt.Errorf("could not read client type: %w", err)
			}

			clientURI, err := promptRequiredText("Application URL", validateURI)
			if err != nil {
				return err
			}
			policyURI, err := promptRequiredText("Privacy policy URL", validateURI)
			if err != nil {
				return err
			}
			tosURI, err := promptRequiredText("Terms of service URL", validateURI)
			if err != nil {
				return err
			}
			logoURI, err := promptRequiredText("Logo URL", validateURI)
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
			} else {
				fmt.Println()
				pterm.Println(pterm.Gray("  Hint: To see setup instructions for this client, run:"))
				pterm.Println(pterm.Gray("    createos oauth clients instructions " + clientID))
			}

			return nil
		},
	}
}
