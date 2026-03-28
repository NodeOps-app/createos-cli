package deployments

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

func newDeploymentBuildLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "build-logs",
		Usage:     "Get build logs for a deployment",
		ArgsUsage: "[project-id] <deployment-id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "follow",
				Aliases: []string{"f"},
				Usage:   "Continuously poll for new build logs",
			},
			&cli.DurationFlag{
				Name:  "interval",
				Value: 2 * time.Second,
				Usage: "Polling interval when using --follow",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c.Args().Slice(), client)
			if err != nil {
				return err
			}

			entries, err := client.GetDeploymentBuildLogs(projectID, deploymentID)
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				fmt.Println("No build logs available yet.")
			} else {
				for _, e := range entries {
					fmt.Println(e.Log)
				}
			}

			if !c.Bool("follow") {
				return nil
			}

			pterm.Println(pterm.Gray("  Tailing build logs (Ctrl+C to stop)..."))
			fmt.Println()

			lastLineNumber := 0
			for _, e := range entries {
				if e.LineNumber > lastLineNumber {
					lastLineNumber = e.LineNumber
				}
			}

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
					newEntries, err := client.GetDeploymentBuildLogs(projectID, deploymentID)
					if err != nil {
						continue
					}
					for _, e := range newEntries {
						if e.LineNumber > lastLineNumber {
							fmt.Println(e.Log)
							lastLineNumber = e.LineNumber
						}
					}
				}
			}
		},
	}
}
