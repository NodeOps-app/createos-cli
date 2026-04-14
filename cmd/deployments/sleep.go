package deployments

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newDeploymentSleepCommand() *cli.Command {
	return &cli.Command{
		Name:  "sleep",
		Usage: "Put a running deployment to sleep",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "deployment", Usage: "Deployment ID"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip confirmation prompt"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, deploymentID, err := resolveDeployment(c, client, []string{"deployed", "deploying", "crashing"})
			if err != nil {
				return err
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("confirmation required — use --force to sleep without a prompt")
				}

				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to put deployment %q to sleep?", deploymentID)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}

				if !confirm {
					fmt.Println("Cancelled. Your deployment was not put to sleep.")
					return nil
				}
			}

			if err := client.SleepDeployment(projectID, deploymentID); err != nil {
				return err
			}

			pterm.Success.Println("Deployment is being put to sleep.")
			return nil
		},
	}
}
