// Package auth provides authentication commands.
package auth

import (
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
	"github.com/NodeOps-app/createos-cli/internal/telemetry"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

const (
	oauthCallbackPort = 65341
	oauthCallbackURI  = "http://localhost:65341/callback"
)

// captureLoginFailure emits an auth_event with success=false. Called BEFORE
// identity is rebound, so distinct_id is still the machine_id_hash — that's
// expected and correct.
func captureLoginFailure(method string, err error) {
	if telemetry.Default == nil {
		return
	}
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	telemetry.Default.Capture("auth_event", map[string]any{
		"action":         "login",
		"method":         method,
		"success":        false,
		"failure_reason": reason,
	})
}

// bindIdentityAndCapture runs the post-credential-save identity flow:
// fetch /me, persist Identity (preserving AliasedForUserID for same user),
// rebind telemetry distinct_id to user_id, then emit success auth_event.
// All identity fetching is best-effort — a failure here must NOT fail login.
//
// If /me fails OR SaveIdentity fails, we DELETE any pre-existing .identity
// file and skip RebindIdentity — otherwise stale identity from a previous
// account on this machine would mis-attribute the new login's telemetry.
func bindIdentityAndCapture(apiClient *api.APIClient, method string) {
	identityFresh := false
	var personProps map[string]any
	if apiClient != nil {
		if u, err := apiClient.GetUser(); err == nil && u != nil && u.ID != "" {
			id := config.Identity{UserID: u.ID}
			if existing, _ := config.LoadIdentity(); existing != nil && existing.UserID == u.ID {
				id.AliasedForUserID = existing.AliasedForUserID
			}
			if saveErr := config.SaveIdentity(id); saveErr == nil {
				identityFresh = true
				personProps = userToPersonProps(u)
			}
		}
		// Silent on /me failure — login still succeeds without user_id.
	}

	if !identityFresh {
		// /me or SaveIdentity failed; drop any stale identity so that a later
		// command does not attribute events to a previous user_id.
		_ = config.DeleteIdentity()
	}

	if telemetry.Default != nil {
		if identityFresh {
			telemetry.Default.SetPersonProperties(personProps)
			telemetry.Default.RebindIdentity()
		}
		telemetry.Default.Capture("auth_event", map[string]any{
			"action":  "login",
			"method":  method,
			"success": true,
		})
	}
}

// userToPersonProps maps the API User struct to PostHog Person-level
// properties. These go ONLY to the Person record via Identify; they are
// NOT included in any Capture event payload. Pointer fields are dereferenced
// only when non-nil.
func userToPersonProps(u *api.User) map[string]any {
	p := map[string]any{
		"email": u.Email,
	}
	if u.DisplayName != nil && *u.DisplayName != "" {
		p["name"] = *u.DisplayName
	}
	if u.Username != nil && *u.Username != "" {
		p["username"] = *u.Username
	}
	if u.CreatedAt != "" {
		// signup_date is immutable — Client uses $set_once for this key.
		p["signup_date"] = u.CreatedAt
	}
	return p
}

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
					wrapped := fmt.Errorf("could not save your token: %w", err)
					captureLoginFailure("token", wrapped)
					return wrapped
				}
				client := api.NewClient(token, c.String("api-url"), c.Bool("debug"))
				bindIdentityAndCapture(&client, "token")
				pterm.Success.Println("You're signed in.")
				return nil
			}

			// Non-interactive (CI/script): require --token flag
			if !terminal.IsInteractive() {
				err := fmt.Errorf("non-interactive mode: use --token flag to sign in\n\n  Example:\n    createos login --token <your-api-token>")
				captureLoginFailure("token", err)
				return err
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
				cancel := fmt.Errorf("sign in cancelled")
				// Method unknown at this point — choice was never made. Pick
				// "browser" since that's the default selection in the picker.
				captureLoginFailure("browser", cancel)
				return cancel
			}

			if selected == options[1] {
				return loginWithAPIToken(c)
			}
			return loginWithBrowser(c)
		},
	}
}

func loginWithAPIToken(c *cli.Context) error {
	token, err := pterm.DefaultInteractiveTextInput.WithMask("*").Show("Paste your API token")
	if err != nil || token == "" {
		cancel := fmt.Errorf("sign in cancelled")
		captureLoginFailure("token", cancel)
		return cancel
	}
	if err := config.SaveToken(token); err != nil {
		wrapped := fmt.Errorf("could not save your token: %w", err)
		captureLoginFailure("token", wrapped)
		return wrapped
	}
	client := api.NewClient(token, c.String("api-url"), c.Bool("debug"))
	bindIdentityAndCapture(&client, "token")
	pterm.Success.Println("You're signed in.")
	return nil
}

func loginWithBrowser(c *cli.Context) error {
	pterm.Info.Println("Starting browser login...")

	port := oauthCallbackPort
	redirectURI := oauthCallbackURI

	pterm.Info.Println("Fetching authorization server info...")
	meta, err := internaloauth.FetchServerMetadata(config.OAuthIssuerURL)
	if err != nil {
		wrapped := fmt.Errorf("could not reach authorization server: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
	}

	pkce, err := internaloauth.GeneratePKCE()
	if err != nil {
		wrapped := fmt.Errorf("could not generate security parameters: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
	}

	state, err := internaloauth.GenerateState()
	if err != nil {
		wrapped := fmt.Errorf("could not generate state: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
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
		wrapped := fmt.Errorf("login was not completed: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
	}

	if returnedState != state {
		wrapped := fmt.Errorf("invalid state parameter — possible CSRF attack, login aborted")
		captureLoginFailure("browser", wrapped)
		return wrapped
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
		wrapped := fmt.Errorf("could not complete sign in: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
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
		wrapped := fmt.Errorf("could not save your session: %w", err)
		captureLoginFailure("browser", wrapped)
		return wrapped
	}

	client := api.NewClientWithAccessToken(tokenResp.AccessToken, c.String("api-url"), c.Bool("debug"))
	bindIdentityAndCapture(&client, "browser")

	fmt.Println()
	pterm.Success.Println("You're signed in.")
	return nil
}
