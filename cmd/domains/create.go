package domains

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newDomainsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Add a custom domain to a project",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "name", Usage: "Domain name to add (e.g. example.com)"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID to link the domain to"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, err := cmdutil.ResolveProjectID(c.String("project"))
			if err != nil {
				return err
			}
			name := c.String("name")
			if name == "" {
				return fmt.Errorf("please provide a domain name with --name (e.g. --name example.com)")
			}

			environmentID, err := resolveEnvironmentForDomain(c, client, projectID)
			if err != nil {
				return err
			}

			id, err := client.AddDomain(projectID, name, environmentID)
			if err != nil {
				return err
			}

			pterm.Success.Printf("Domain %q added successfully.\n", name)
			fmt.Println()

			// Fetch domain to get DNS records
			domains, err := client.ListDomains(projectID)
			if err == nil {
				for _, d := range domains {
					if d.ID == id {
						printDNSRecords(d)
						return nil
					}
				}
			}

			// Fallback if records not yet available
			fmt.Println("  DNS records are being generated. Run 'createos domains verify' to check status.")
			return nil
		},
	}
}

func printDNSRecords(d api.Domain) {
	if d.Records == nil || (len(d.Records.ARecords) == 0 && len(d.Records.TXTRecords) == 0) {
		fmt.Println("  DNS records are being generated. Run verify to check status.")
		return
	}

	fmt.Println("  Configure your DNS with the following records:")
	fmt.Println()

	tableData := pterm.TableData{{"Type", "Name", "Value"}}
	for _, a := range d.Records.ARecords {
		tableData = append(tableData, []string{"A", d.Name, a})
	}
	for _, txt := range d.Records.TXTRecords {
		tableData = append(tableData, []string{"TXT", txt.Name + "." + d.Name, txt.Value})
	}

	_ = pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
	fmt.Println()
}
