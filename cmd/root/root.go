// Package root wires together the CLI application.
package root

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/cmd/ask"
	"github.com/NodeOps-app/createos-cli/cmd/auth"
	"github.com/NodeOps-app/createos-cli/cmd/cronjobs"
	"github.com/NodeOps-app/createos-cli/cmd/deploy"
	"github.com/NodeOps-app/createos-cli/cmd/deployments"
	"github.com/NodeOps-app/createos-cli/cmd/domains"
	"github.com/NodeOps-app/createos-cli/cmd/env"
	"github.com/NodeOps-app/createos-cli/cmd/environments"
	initcmd "github.com/NodeOps-app/createos-cli/cmd/init"
	"github.com/NodeOps-app/createos-cli/cmd/oauth"
	"github.com/NodeOps-app/createos-cli/cmd/open"
	"github.com/NodeOps-app/createos-cli/cmd/projects"
	"github.com/NodeOps-app/createos-cli/cmd/sandbox"
	"github.com/NodeOps-app/createos-cli/cmd/scale"
	"github.com/NodeOps-app/createos-cli/cmd/skills"
	"github.com/NodeOps-app/createos-cli/cmd/status"
	"github.com/NodeOps-app/createos-cli/cmd/templates"
	"github.com/NodeOps-app/createos-cli/cmd/upgrade"
	"github.com/NodeOps-app/createos-cli/cmd/users"
	versioncmd "github.com/NodeOps-app/createos-cli/cmd/version"
	"github.com/NodeOps-app/createos-cli/cmd/vms"
	"github.com/NodeOps-app/createos-cli/cmd/webhooks"
	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/intro"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// NewApp creates and configures the root CLI application.
func NewApp() *cli.App {
	app := &cli.App{
		Name:                 "createos",
		Usage:                "CreateOS CLI - Manage your infrastructure",
		Version:              version.Version,
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug mode",
				EnvVars: []string{"CREATEOS_DEBUG"},
			},
			&cli.StringFlag{
				Name:  "api-url",
				Usage: "Override the API base URL",
				EnvVars: []string{"CREATEOS_API_URL",
					"CREATEOS_PLAN_API_URL",
					"CREATEOS_PROJECT_API_URL"},
				Value: api.DefaultBaseURL,
			},
			&cli.StringFlag{
				Name:    "sandbox-api-url",
				Usage:   "Override the sandbox (fc-spawn) base URL",
				EnvVars: []string{"CREATEOS_SANDBOX_URL"},
				Value:   api.DefaultSandboxBaseURL,
			},
			&cli.StringFlag{
				Name:    "sandbox-gateway",
				Usage:   "SSH gateway address (<host:port>) used by `sandbox shell`",
				EnvVars: []string{"CREATEOS_SANDBOX_GATEWAY"},
				Value:   "gateway.sb.createos.sh:2222",
			},
			&cli.StringFlag{
				Name:  "api-key",
				Usage: "API key for authentication (overrides stored token)",
				EnvVars: []string{"CREATEOS_API_KEY",
					"CREATEOS_PLAN_API_KEY",
					"CREATEOS_PROJECT_API_KEY"},
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Output format: table or json",
				EnvVars: []string{"CREATEOS_OUTPUT"},
			},
		},
		Before: func(c *cli.Context) error {
			// Store the output format in metadata
			c.App.Metadata[output.FormatKey] = output.DetectFormat(c)
			c.App.Metadata[output.FormatExplicitKey] = c.String("output") != ""

			// One choke point for colour: a pipe, a CI log, or NO_COLOR
			// must never receive ANSI escapes. pterm styles every helper
			// through this global, so disabling it here covers every
			// command at once.
			if os.Getenv("NO_COLOR") != "" || !terminal.IsInteractive() {
				pterm.DisableStyling()
			}

			// In JSON mode stdout carries one machine-readable document and
			// nothing else. Sending every pterm print to stderr keeps that
			// true even for commands that still narrate progress, so a
			// consumer never has to strip prose out of the stream.
			if output.IsJSON(c) {
				pterm.SetDefaultOutput(os.Stderr)
			}

			// Skip auth for --help / -h on any command
			for _, a := range c.Args().Slice() {
				if a == "--help" || a == "-h" || a == "help" {
					return nil
				}
			}

			apiURL := c.String("api-url")
			if apiURL != "" && apiURL != api.DefaultBaseURL {
				parsed, err := url.Parse(apiURL)
				if err != nil || (parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") {
					return fmt.Errorf("--api-url must use HTTPS (got %q)\n\n  Exception: localhost and 127.0.0.1 are allowed for development", apiURL)
				}
			}

			cmd := c.Args().First()
			if cmd == "" || cmd == "login" || cmd == "logout" || cmd == "version" || cmd == "ask" || cmd == "upgrade" {
				return nil
			}

			// CREATEOS_API_KEY env var (or --api-key flag) — injected by Stripe Projects
			if apiKey := c.String("api-key"); apiKey != "" {
				client := api.NewClient(apiKey, c.String("api-url"), c.Bool("debug"))
				c.App.Metadata[api.ClientKey] = &client
				sandboxClient := api.NewSandboxClient(apiKey, c.String("sandbox-api-url"), c.Bool("debug"))
				c.App.Metadata[api.SandboxClientKey] = &sandboxClient
				return nil
			}

			// Try OAuth session
			if config.HasOAuthSession() {
				session, err := config.LoadOAuthSession()
				if err != nil {
					return fmt.Errorf("could not load your session: %w", err)
				}
				if session != nil {
					// Pre-flight refresh when the token is expired (or
					// about to be). A hard failure here means the refresh
					// token is dead — the user must sign in again.
					if config.IsTokenExpired(session) {
						if _, err := refreshOAuthSession(session); err != nil {
							return fmt.Errorf("your session has expired and could not be renewed — run 'createos login' to sign in again")
						}
					}
					// Reactive refresher: if the server rejects the token
					// mid-command with a 401 (revoked server-side, or our
					// clock was wrong), the clients refresh and retry once.
					refresher := func() (string, error) {
						return refreshOAuthSession(session)
					}
					client := api.NewClientWithAccessToken(session.AccessToken, c.String("api-url"), c.Bool("debug"), refresher)
					c.App.Metadata[api.ClientKey] = &client
					// Sandbox API (fc-spawn) reuses the same OAuth access
					// token, but it must go in the X-Access-Token header —
					// fc-spawn rejects a JWT sent as X-Api-Key with
					// "invalid api key".
					sandboxClient := api.NewSandboxClientWithAccessToken(session.AccessToken, c.String("sandbox-api-url"), c.Bool("debug"), refresher)
					c.App.Metadata[api.SandboxClientKey] = &sandboxClient
					return nil
				}
			}

			// Fall back to stored token (~/.createos/.token)
			token, err := config.LoadToken()
			if err != nil {
				return err
			}
			client := api.NewClient(token, c.String("api-url"), c.Bool("debug"))
			c.App.Metadata[api.ClientKey] = &client
			sandboxClient := api.NewSandboxClient(token, c.String("sandbox-api-url"), c.Bool("debug"))
			c.App.Metadata[api.SandboxClientKey] = &sandboxClient
			return nil
		},
		Action: func(_ *cli.Context) error {
			intro.Show()

			fmt.Println("Available Commands:")
			if config.IsLoggedIn() {
				fmt.Println("  cronjobs       Manage cron jobs for a project")
				fmt.Println("  deploy         Deploy your project to CreateOS")
				fmt.Println("  deployments    Manage project deployments")
				fmt.Println("  domains        Manage custom domains")
				fmt.Println("  env            Manage environment variables")
				fmt.Println("  environments   Manage project environments")
				fmt.Println("  init           Link this directory to a CreateOS project")
				fmt.Println("  logout         Sign out from CreateOS")
				fmt.Println("  me             Manage your account and OAuth consents")
				fmt.Println("  oauth-clients  Manage OAuth clients")
				fmt.Println("  open           Open project URL or dashboard in browser")
				fmt.Println("  projects       Manage projects")
				fmt.Println("  sandbox        Manage sandboxes")
				fmt.Println("  scale          Adjust replicas and resources")
				fmt.Println("  skills         Manage skills")
				fmt.Println("  status         Show project health and deployment status")
				fmt.Println("  templates      Browse and scaffold from project templates")
				fmt.Println("  vms            Manage VM terminal instances")
				fmt.Println("  webhooks       Manage webhook endpoints")
				fmt.Println("  whoami         Show the currently authenticated user")
			} else {
				fmt.Println("  login          Authenticate with CreateOS")
			}
			fmt.Println("  ask            Ask the AI assistant to help manage your infrastructure")
			fmt.Println("  upgrade        Upgrade createos to the latest version")
			fmt.Println("  version        Print the current version")
			fmt.Println()
			fmt.Println("Global Flags:")
			fmt.Println("  --output, -o   Output format: table (default) or json")
			fmt.Println("  --debug, -d    Enable debug mode")
			fmt.Println()
			fmt.Println("Run 'createos <command> --help' for more information on a command.")

			return nil
		},
		Commands: []*cli.Command{
			auth.NewLoginCommand(),
			auth.NewLogoutCommand(),
			cronjobs.NewCronjobsCommand(),
			deploy.NewDeployCommand(),
			deployments.NewDeploymentsCommand(),
			ask.NewAskCommand(),
			domains.NewDomainsCommand(),
			env.NewEnvCommand(),
			environments.NewEnvironmentsCommand(),
			initcmd.NewInitCommand(),
			oauth.NewOAuthCommand(),
			open.NewOpenCommand(),
			projects.NewProjectsCommand(),
			sandbox.NewSandboxCommand(),
			scale.NewScaleCommand(),
			skills.NewSkillsCommand(),
			status.NewStatusCommand(),
			templates.NewTemplatesCommand(),
			upgrade.NewUpgradeCommand(),
			users.NewUsersCommand(),
			vms.NewVMsCommand(),
			webhooks.NewWebhooksCommand(),
			auth.NewWhoamiCommand(),
			versioncmd.NewVersionCommand(),
		},
	}

	return app
}

// refreshOAuthSession exchanges the session's refresh token for a new
// access token, updates the session in place, and persists it to
// ~/.createos/.oauth. It returns the new access token. This is shared by
// the pre-flight (expiry-based) refresh in the Before hook and the
// reactive on-401 retry wired into the API clients, so both paths rotate
// and store tokens identically.
func refreshOAuthSession(session *config.OAuthSession) (string, error) {
	tokenEndpoint := session.TokenEndpoint
	if tokenEndpoint == "" {
		tokenEndpoint = config.OAuthIssuerURL + "/oauth2/token"
	}
	refreshed, err := internaloauth.RefreshTokens(
		tokenEndpoint,
		config.OAuthClientID,
		session.RefreshToken,
	)
	if err != nil {
		return "", err
	}
	session.AccessToken = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		session.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.ExpiresIn > 0 {
		session.ExpiresAt = time.Now().Unix() + int64(refreshed.ExpiresIn)
	}
	if err := config.SaveOAuthSession(*session); err != nil {
		return "", err
	}
	return session.AccessToken, nil
}
