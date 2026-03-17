package auth

import (
	"fmt"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/config"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
)

const (
	oauthClientID       = "fbcaaa58-1e30-43fe-8fba-34382ba4fe7f"
	oauthIssuerURL      = "https://id.nodeops.network"
	oauthCallbackPort = 65341
	oauthCallbackURI  = "http://localhost:65341/callback"
)

// NewLoginCommand creates the login command
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
			// --token flag: API key flow
			if token := c.String("token"); token != "" {
				if err := config.SaveToken(token); err != nil {
					return fmt.Errorf("could not save your token: %w", err)
				}
				pterm.Success.Println("You're now signed in! Run 'createos whoami' to confirm your account.")
				return nil
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
	pterm.Println(pterm.Gray("  You can find your API token in your CreateOS dashboard."))
	fmt.Println()
	token, err := pterm.DefaultInteractiveTextInput.WithMask("*").Show("Paste your API token")
	if err != nil || token == "" {
		return fmt.Errorf("sign in cancelled")
	}
	if err := config.SaveToken(token); err != nil {
		return fmt.Errorf("could not save your token: %w", err)
	}
	pterm.Success.Println("You're now signed in! Run 'createos whoami' to confirm your account.")
	return nil
}

func loginWithBrowser() error {
	pterm.Info.Println("Starting browser login...")

	port := oauthCallbackPort
	redirectURI := oauthCallbackURI

	// 2. Fetch OAuth server metadata
	pterm.Info.Println("Fetching authorization server info...")
	meta, err := internaloauth.FetchServerMetadata(oauthIssuerURL)
	if err != nil {
		return fmt.Errorf("could not reach authorization server: %w", err)
	}

	// 3. Generate PKCE pair
	pkce, err := internaloauth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("could not generate security parameters: %w", err)
	}

	// 4. Generate state for CSRF protection
	state, err := internaloauth.GenerateState()
	if err != nil {
		return fmt.Errorf("could not generate state: %w", err)
	}

	// 5. Build authorization URL
	authURL := internaloauth.BuildAuthURL(
		meta.AuthorizationEndpoint,
		oauthClientID,
		redirectURI,
		state,
		pkce.Challenge,
	)

	// 6. Print fallback URL before opening browser
	fmt.Println()
	pterm.Println(pterm.Gray("  If your browser doesn't open, visit this URL:"))
	pterm.Println(pterm.Gray("  " + authURL))
	fmt.Println()

	// 7. Open browser
	if err := internaloauth.OpenBrowser(authURL); err != nil {
		pterm.Warning.Println("Could not open browser automatically. Please open the URL above.")
	} else {
		pterm.Info.Println("Waiting for you to complete login in your browser...")
	}

	// 8. Wait for callback
	code, returnedState, err := internaloauth.StartCallbackServer(port)
	if err != nil {
		return fmt.Errorf("login was not completed: %w", err)
	}

	// 9. Verify state
	if returnedState != state {
		return fmt.Errorf("invalid state parameter — possible CSRF attack, login aborted")
	}

	// 10. Exchange code for tokens
	pterm.Info.Println("Completing sign in...")
	tokenResp, err := internaloauth.ExchangeCode(
		meta.TokenEndpoint,
		oauthClientID,
		code,
		redirectURI,
		pkce.Verifier,
	)
	if err != nil {
		return fmt.Errorf("could not complete sign in: %w", err)
	}

	// 11. Save OAuth session
	expiresAt := time.Now().Unix() + int64(tokenResp.ExpiresIn)
	if tokenResp.ExpiresIn <= 0 {
		expiresAt = time.Now().Unix() + 3600 // default 1 hour
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
	pterm.Success.Println("You're signed in! Run 'createos whoami' to confirm your account.")
	return nil
}
