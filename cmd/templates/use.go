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
)

func newTemplatesUseCommand() *cli.Command {
	return &cli.Command{
		Name:      "use",
		Usage:     "Download and scaffold a project from a template",
		ArgsUsage: "<template-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Usage: "Target directory (defaults to template name)"},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a template ID\n\n  To see available templates, run:\n    createos templates list")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			templateID := c.Args().First()

			tmpl, err := client.GetTemplate(templateID)
			if err != nil {
				return err
			}

			downloadURL, err := client.GetTemplateDownloadURL(templateID)
			if err != nil {
				return err
			}

			dir := c.String("dir")
			if dir == "" {
				dir = tmpl.Name
			}

			// Create target directory
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(absDir, 0755); err != nil {
				return fmt.Errorf("could not create directory %s: %w", dir, err)
			}

			// Download the template
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
				// Clean up partial file on failure
				os.Remove(zipPath) //nolint:errcheck
				return err
			}

			pterm.Success.Printf("Template %q downloaded to %s\n", tmpl.Name, dir)
			fmt.Println()
			pterm.Println(pterm.Gray("  Next steps:"))
			pterm.Println(pterm.Gray(fmt.Sprintf("    cd %s", dir)))
			pterm.Println(pterm.Gray("    unzip template.zip && rm template.zip"))
			pterm.Println(pterm.Gray("    createos init"))
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
		out.Close() //nolint:errcheck
		return fmt.Errorf("could not write template: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("could not finalize file: %w", err)
	}
	return nil
}
