package deployments

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newDeploymentLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Get logs for a deployment",
		ArgsUsage: "<project-id> <deployment-id>",
		Description: "Fetches the latest logs for a running deployment.\n\n" +
			"   To find your deployment ID, run:\n" +
			"     createos deployments list <project-id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "follow",
				Aliases: []string{"f"},
				Usage:   "Continuously poll for new logs",
			},
			&cli.DurationFlag{
				Name:  "interval",
				Value: 2 * time.Second,
				Usage: "Polling interval when using --follow",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("please provide a project ID and deployment ID\n\n  Example:\n    createos deployments logs <project-id> <deployment-id>")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.Args().Get(0)
			deploymentID := c.Args().Get(1)

			logs, err := client.GetDeploymentLogs(projectID, deploymentID)
			if err != nil {
				return err
			}

			if logs == "" {
				fmt.Println("No logs available yet. The deployment may still be starting up.")
			} else {
				fmt.Print(logs)
				if !strings.HasSuffix(logs, "\n") {
					fmt.Println()
				}
			}

			if !c.Bool("follow") {
				fmt.Println()
				pterm.Println(pterm.Gray("  Tip: Use --follow (-f) to tail logs in real-time."))
				return nil
			}

			// Follow mode: poll for new logs
			pterm.Println(pterm.Gray("  Tailing logs (Ctrl+C to stop)..."))
			fmt.Println()

			previousLen := len(logs)
			interval := c.Duration("interval")
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			for {
				select {
				case <-sigCh:
					fmt.Println()
					pterm.Info.Println("Log streaming stopped.")
					return nil
				case <-ticker.C:
					newLogs, err := client.GetDeploymentLogs(projectID, deploymentID)
					if err != nil {
						continue // transient error, keep trying
					}
					if len(newLogs) > previousLen {
						// Print only the new portion
						fmt.Print(newLogs[previousLen:])
						previousLen = len(newLogs)
					}
				}
			}
		},
	}
}
