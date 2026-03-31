package templates

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newTemplatesUseCommand() *cli.Command {
	return &cli.Command{
		Name:      "use",
		Usage:     "Download and scaffold a project from a template",
		ArgsUsage: "[template-id]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "template", Usage: "Template ID"},
			&cli.StringFlag{Name: "dir", Usage: "Target directory (defaults to template name)"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Skip purchase confirmation (required in non-interactive mode)"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			templateID, err := resolveTemplate(c, client)
			if err != nil {
				return err
			}

			tmpl, err := client.GetTemplate(templateID)
			if err != nil {
				return err
			}

			// Check if already owned
			purchases, err := client.ListTemplatePurchases()
			if err != nil {
				return fmt.Errorf("could not retrieve your purchases: %w", err)
			}

			var purchaseID string
			for _, p := range purchases {
				if p.ProjectTemplateID == templateID {
					purchaseID = p.ID
					break
				}
			}

			// Not owned yet — confirm before purchasing
			if purchaseID == "" {
				if !c.Bool("yes") {
					if !terminal.IsInteractive() {
						return fmt.Errorf("use --yes to confirm purchase in non-interactive mode\n\n  Example:\n    createos templates use --template %s --yes", templateID)
					}
					var confirmText string
					if tmpl.Amount > 0 {
						confirmText = fmt.Sprintf("Purchase %q for %.2f credits?", tmpl.Name, tmpl.Amount)
					} else {
						confirmText = fmt.Sprintf("Download %q (free)?", tmpl.Name)
					}
					confirm, err := pterm.DefaultInteractiveConfirm.
						WithDefaultText(confirmText).
						WithDefaultValue(true).
						Show()
					if err != nil {
						return fmt.Errorf("could not read confirmation: %w", err)
					}
					if !confirm {
						fmt.Println("Cancelled.")
						return nil
					}
				}

				newPurchaseID, err := client.BuyTemplate(templateID)
				if err != nil {
					return err
				}
				purchaseID = newPurchaseID
			}

			downloadURL, err := client.GetTemplatePurchaseDownloadURL(purchaseID)
			if err != nil {
				return err
			}

			dir := c.String("dir")
			if dir == "" {
				dir = filepath.Base(tmpl.Name)
				if dir == "." || dir == ".." {
					return fmt.Errorf("template name %q is not safe as a directory name — use --dir to specify output directory", tmpl.Name)
				}
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(absDir, 0750); err != nil { //nolint:gosec
				return fmt.Errorf("could not create directory %s: %w", dir, err)
			}

			resp, err := http.Get(downloadURL) //nolint:gosec,noctx
			if err != nil {
				return fmt.Errorf("could not download template: %w", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("template download returned status %d", resp.StatusCode)
			}

			zipPath := filepath.Join(absDir, "template.zip")
			if err := downloadToFile(zipPath, resp.Body); err != nil {
				_ = os.Remove(zipPath)
				return err
			}

			pterm.Success.Printf("Template %q downloaded to %s/template.zip\n", tmpl.Name, dir)
			return nil
		},
	}
}

func downloadToFile(path string, src io.Reader) error {
	out, err := os.Create(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return fmt.Errorf("could not write template: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("could not finalize file: %w", err)
	}
	return nil
}
