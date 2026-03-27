package domains

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
)

func newDomainsVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:      "verify",
		Usage:     "Check DNS propagation and wait for domain verification",
		ArgsUsage: "[project-id] <domain-id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "no-wait",
				Usage: "Check once and exit instead of polling",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, domainID, err := cmdutil.ResolveProjectScopedArg(c.Args().Slice(), "a domain ID")
			if err != nil {
				return err
			}

			// Trigger a refresh first
			if err := client.RefreshDomain(projectID, domainID); err != nil {
				return err
			}

			// Check status
			domains, err := client.ListDomains(projectID)
			if err != nil {
				return err
			}

			var domain *api.Domain
			for i := range domains {
				if domains[i].ID == domainID {
					domain = &domains[i]
					break
				}
			}
			if domain == nil {
				return fmt.Errorf("domain %s not found", domainID)
			}

			if domain.Status == "verified" || domain.Status == "active" {
				pterm.Success.Printf("Domain %s is verified!\n", domain.Name)
				return nil
			}

			if c.Bool("no-wait") {
				pterm.Warning.Printf("Domain %s status: %s\n", domain.Name, domain.Status)
				return nil
			}

			// Poll until verified
			pterm.Info.Printf("Waiting for DNS verification of %s...\n", domain.Name)

			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			attempts := 0
			maxAttempts := 30 // 5 minutes

			for {
				select {
				case <-sigCh:
					fmt.Println()
					pterm.Info.Println("Verification stopped. You can check again later with:")
					pterm.Println(pterm.Gray("    createos domains verify " + projectID + " " + domainID))
					return nil
				case <-ticker.C:
					attempts++
					if attempts > maxAttempts {
						pterm.Warning.Println("DNS propagation is taking longer than expected.")
						pterm.Println(pterm.Gray("  DNS changes can take up to 48 hours. Try again later:"))
						pterm.Println(pterm.Gray("    createos domains verify " + projectID + " " + domainID))
						return nil
					}

					_ = client.RefreshDomain(projectID, domainID)
					domains, err := client.ListDomains(projectID)
					if err != nil {
						continue
					}

					for _, d := range domains {
						if d.ID == domainID {
							if d.Status == "verified" || d.Status == "active" {
								pterm.Success.Printf("Domain %s is verified!\n", d.Name)
								return nil
							}
							fmt.Printf("  ⏳ Checking DNS... %s\n", d.Status)
							break
						}
					}
				}
			}
		},
	}
}
