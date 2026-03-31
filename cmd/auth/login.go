// Package auth provides authentication commands.
package auth

import (
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/config"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

const (
	oauthCallbackPort = 65341
	oauthCallbackURI  = "http://localhost:65341/callback"
)

// NewLoginCommand creates the login command.
func NewLoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Sign in to your CreateOS account",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "Your API token (from your CreateOS dashboard) — skips browser login",
			},
		},
		Action: func(c *cli.Context) error {
			// --token flag: API key flow (works in both TTY and non-TTY)
			if token := c.String("token"); token != "" {
				if err := config.SaveToken(token); err != nil {
					return fmt.Errorf("could not save your token: %w", err)
				}
				pterm.Success.Println("You're signed in.")
				return nil
			}

			// Non-interactive (CI/script): require --token flag
			if !terminal.IsInteractive() {
				return fmt.Errorf("non-interactive mode: use --token flag to sign in\n\n  Example:\n    createos login --token <your-api-token>")
			}

			// Interactive: let user choose auth method
			options := []string{
				"Sign in with browser (recommended)",
				"Sign in with API token",
			}
			selected, err := pterm.DefaultInteractiveSelect.
				WithOptions(options).
				Show("How would you like to sign in?")
			if err != nil {
				return fmt.Errorf("sign in cancelled")
			}

			if selected == options[1] {
				return loginWithAPIToken()
			}
			return loginWithBrowser()
		},
	}
}

func loginWithAPIToken() error {
	token, err := pterm.DefaultInteractiveTextInput.WithMask("*").Show("Paste your API token")
	if err != nil || token == "" {
		return fmt.Errorf("sign in cancelled")
	}
	if err := config.SaveToken(token); err != nil {
		return fmt.Errorf("could not save your token: %w", err)
	}
	pterm.Success.Println("You're signed in.")
	return nil
}

func loginWithBrowser() error {
	pterm.Info.Println("Starting browser login...")

	port := oauthCallbackPort
	redirectURI := oauthCallbackURI

	pterm.Info.Println("Fetching authorization server info...")
	meta, err := internaloauth.FetchServerMetadata(config.OAuthIssuerURL)
	if err != nil {
		return fmt.Errorf("could not reach authorization server: %w", err)
	}

	pkce, err := internaloauth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("could not generate security parameters: %w", err)
	}

	state, err := internaloauth.GenerateState()
	if err != nil {
		return fmt.Errorf("could not generate state: %w", err)
	}

	authURL := internaloauth.BuildAuthURL(
		meta.AuthorizationEndpoint,
		config.OAuthClientID,
		redirectURI,
		state,
		pkce.Challenge,
	)

	fmt.Println()
	pterm.Println(pterm.Gray("  If your browser doesn't open, visit this URL:"))
	pterm.Println(pterm.Gray("  " + authURL))
	fmt.Println()

	if err := internaloauth.OpenBrowser(authURL); err != nil {
		pterm.Warning.Println("Could not open browser automatically. Please open the URL above.")
	} else {
		pterm.Info.Println("Waiting for you to complete login in your browser...")
	}

	code, returnedState, err := internaloauth.StartCallbackServer(port)
	if err != nil {
		return fmt.Errorf("login was not completed: %w", err)
	}

	if returnedState != state {
		return fmt.Errorf("invalid state parameter — possible CSRF attack, login aborted")
	}

	pterm.Info.Println("Completing sign in...")
	tokenResp, err := internaloauth.ExchangeCode(
		meta.TokenEndpoint,
		config.OAuthClientID,
		code,
		redirectURI,
		pkce.Verifier,
	)
	if err != nil {
		return fmt.Errorf("could not complete sign in: %w", err)
	}

	expiresAt := time.Now().Unix() + int64(tokenResp.ExpiresIn)
	if tokenResp.ExpiresIn <= 0 {
		expiresAt = time.Now().Unix() + 3600
	}
	session := config.OAuthSession{
		AccessToken:   tokenResp.AccessToken,
		RefreshToken:  tokenResp.RefreshToken,
		ExpiresAt:     expiresAt,
		TokenEndpoint: meta.TokenEndpoint,
	}
	if err := config.SaveOAuthSession(session); err != nil {
		return fmt.Errorf("could not save your session: %w", err)
	}

	fmt.Println()
	pterm.Success.Println("You're signed in.")
	return nil
}
