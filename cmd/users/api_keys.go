package users

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// NewAPIKeysCommand provides API key management commands.
func NewAPIKeysCommand() *cli.Command {
	return &cli.Command{
		Name:  "api-keys",
		Usage: "Manage your API keys",
		Subcommands: []*cli.Command{
			{
				Name:        "create",
				Usage:       "Create an API key and print its secret",
				Description: "Prompts for missing values in a terminal. In scripts, provide --name and --expires-at.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Usage: "Unique key name (4–48 characters)"},
					&cli.StringFlag{Name: "expires-at", Usage: "Future expiration time in RFC3339 format"},
					&cli.StringFlag{Name: "description", Usage: "Key description (4–2048 characters)"},
				},
				Action: createAPIKey,
			},
			{Name: "list", Aliases: []string{"ls"}, Usage: "List your API keys", Action: listAPIKeys},
			{
				Name: "revoke", Usage: "Revoke an API key", ArgsUsage: "[key-id]",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip confirmation"},
				},
				Action: revokeAPIKey,
			},
		},
	}
}

func createAPIKey(c *cli.Context) error {
	name := strings.TrimSpace(c.String("name"))
	expiresAt := c.String("expires-at")
	description := c.String("description")
	if name == "" || expiresAt == "" {
		if !canPromptForAPIKey() {
			return fmt.Errorf("provide --name and --expires-at in non-interactive mode")
		}
		var err error
		if name == "" {
			name, err = pterm.DefaultInteractiveTextInput.Show("API key name (4–48 characters)")
			if err != nil {
				return fmt.Errorf("could not read name: %w", err)
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("API key name is required")
			}
		}
		if expiresAt == "" {
			expiresAt, err = pterm.DefaultInteractiveTextInput.Show("Expiration (RFC3339, press Enter for 30 days)")
			if err != nil {
				return fmt.Errorf("could not read expiration: %w", err)
			}
			if expiresAt == "" {
				expiresAt = time.Now().UTC().AddDate(0, 0, 30).Format(time.RFC3339)
			}
		}
		if !c.IsSet("description") {
			description, err = pterm.DefaultInteractiveTextInput.Show("Description (optional)")
			if err != nil {
				return fmt.Errorf("could not read description: %w", err)
			}
		}
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || !expiry.After(time.Now()) {
		return fmt.Errorf("--expires-at must be a future RFC3339 timestamp, such as 2030-01-01T00:00:00Z")
	}
	client, err := getClient(c)
	if err != nil {
		return err
	}
	key, err := client.CreateAPIKey(c.Context, api.CreateAPIKeyInput{
		Name: name, Description: description, ExpiryAt: expiry,
	})
	if err != nil {
		return err
	}
	output.Render(c, key, func() {
		fmt.Printf("API key ID: %s\nSecret: %s\n", key.ID, key.Secret)
	})
	return nil
}

func listAPIKeys(c *cli.Context) error {
	client, err := getClient(c)
	if err != nil {
		return err
	}
	keys, err := client.ListAPIKeys(c.Context)
	if err != nil {
		return err
	}
	if output.IsJSON(c) {
		output.Render(c, keys, nil)
		return nil
	}
	if len(keys) == 0 {
		fmt.Println("You have no API keys.")
		return nil
	}
	rows := pterm.TableData{{"ID", "Name", "Key preview", "Expires", "Status"}}
	for _, key := range keys {
		name, expiry, status := "-", "-", "active"
		if key.Name != nil {
			name = *key.Name
		}
		if key.ExpiredAt != nil {
			expiry = key.ExpiredAt.Format(time.RFC3339)
			if !key.ExpiredAt.After(time.Now()) {
				status = "expired"
			}
		}
		if key.RevokedAt != nil {
			status = "revoked"
		}
		preview := key.MaskedSecret
		if preview == "" {
			preview = "-"
		}
		rows = append(rows, []string{key.ID, name, preview, expiry, status})
	}
	const previewColumn = 2
	return output.RenderTable(rows, previewColumn)
}

func canPromptForAPIKey() bool {
	return terminal.IsInteractive() && term.IsTerminal(int(os.Stdin.Fd())) // #nosec G115 -- stdin is a standard file descriptor
}

func selectAPIKey(c *cli.Context, client *api.APIClient) (string, error) {
	keys, err := client.ListAPIKeys(c.Context)
	if err != nil {
		return "", err
	}
	options := make([]string, 0, len(keys))
	ids := make(map[string]string, len(keys))
	now := time.Now()
	for _, key := range keys {
		if key.RevokedAt != nil || (key.ExpiredAt != nil && !key.ExpiredAt.After(now)) {
			continue
		}
		label := key.ID
		if key.MaskedSecret != "" {
			label = key.MaskedSecret + "  " + label
		}
		if key.Name != nil {
			label = *key.Name + "  " + label
		}
		options = append(options, label)
		ids[label] = key.ID
	}
	if len(options) == 0 {
		return "", fmt.Errorf("you have no active API keys to revoke")
	}
	selected, err := pterm.DefaultInteractiveSelect.WithOptions(options).
		WithDefaultText("Select an API key to revoke").Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	id, ok := ids[selected]
	if !ok {
		return "", fmt.Errorf("no API key selected")
	}
	return id, nil
}

func revokeAPIKey(c *cli.Context) error {
	id := strings.TrimSpace(c.Args().First())
	if c.NArg() > 1 || (id == "" && (c.NArg() == 1 || !canPromptForAPIKey())) {
		return fmt.Errorf("provide one API key ID from 'createos api-keys list'")
	}
	client, err := getClient(c)
	if err != nil {
		return err
	}
	if id == "" {
		id, err = selectAPIKey(c, client)
		if err != nil {
			return err
		}
	}
	if !c.Bool("force") {
		if !canPromptForAPIKey() {
			return fmt.Errorf("use --force to confirm revocation in non-interactive mode")
		}
		confirmed, err := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).
			WithDefaultText(fmt.Sprintf("Revoke API key %q?", id)).Show()
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if err := client.RevokeAPIKey(c.Context, id); err != nil {
		return err
	}
	output.Render(c, map[string]string{"id": id, "status": "revoked"}, func() {
		fmt.Printf("Revoked API key %s.\n", id)
	})
	return nil
}
