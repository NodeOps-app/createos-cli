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
)

func newDomainsVerifyCommand() *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "Check DNS propagation and wait for domain verification",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "domain", Usage: "Domain ID"},
			&cli.BoolFlag{Name: "no-wait", Usage: "Check once and exit instead of polling"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, domainID, err := resolveDomain(c, client)
			if err != nil {
				return err
			}

			_ = client.RefreshDomain(projectID, domainID)

			domain, err := findDomain(client, projectID, domainID)
			if err != nil {
				return err
			}

			if domain.Status == "verified" || domain.Status == "active" {
				pterm.Success.Printf("Domain %s is verified!\n", domain.Name)
				return nil
			}

			// Show required DNS records so the user knows what to configure
			printDNSRecords(*domain)

			if c.Bool("no-wait") {
				pterm.Warning.Printf("Domain %s status: %s\n", domain.Name, domain.Status)
				return nil
			}

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
					pterm.Info.Println("Verification stopped.")
					return nil
				case <-ticker.C:
					attempts++
					if attempts > maxAttempts {
						pterm.Warning.Println("DNS propagation is taking longer than expected. Changes can take up to 48 hours.")
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
							fmt.Printf("  ⏳ %s\n", d.Status)
							break
						}
					}
				}
			}
		},
	}
}

func findDomain(client *api.APIClient, projectID, domainID string) (*api.Domain, error) {
	domains, err := client.ListDomains(projectID)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if domains[i].ID == domainID {
			return &domains[i], nil
		}
	}
	return nil, fmt.Errorf("domain %s not found", domainID)
}
