package oauth

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func getClient(c *cli.Context) (*api.APIClient, error) {
	client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
	if !ok {
		return nil, fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	return client, nil
}

// resolveOAuthClientID resolves a client ID from flag, arg, or interactive select.
func resolveOAuthClientID(c *cli.Context, apiClient *api.APIClient) (string, error) {
	if id := c.String("client"); id != "" {
		return id, nil
	}
	if c.NArg() > 0 {
		return c.Args().First(), nil
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("please provide a client ID\n\n  Example:\n    createos oauth-clients %s --client <client-id>", c.Command.Name)
	}
	return pickOAuthClient(apiClient)
}

func pickOAuthClient(apiClient *api.APIClient) (string, error) {
	clients, err := apiClient.ListOAuthClients()
	if err != nil {
		return "", err
	}
	if len(clients) == 0 {
		return "", fmt.Errorf("you don't have any OAuth clients yet — run 'createos oauth-clients create' to create one")
	}
	if len(clients) == 1 {
		return clients[0].ID, nil
	}
	options := make([]string, len(clients))
	for i, cl := range clients {
		options[i] = cl.Name
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText("Select an OAuth client").
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read selection: %w", err)
	}
	for i, opt := range options {
		if opt == selected {
			return clients[i].ID, nil
		}
	}
	return "", fmt.Errorf("no client selected")
}

func promptRequiredText(prompt string, validate func(string) error) (string, error) {
	for {
		value, err := pterm.DefaultInteractiveTextInput.Show(prompt)
		if err != nil {
			return "", fmt.Errorf("could not read input: %w", err)
		}
		value = strings.TrimSpace(value)
		if err := validate(value); err != nil {
			pterm.Error.Println(err.Error())
			continue
		}
		return value, nil
	}
}

func validateClientName(value string) error {
	if value == "" {
		return fmt.Errorf("name is required")
	}
	if len(value) < 4 {
		return fmt.Errorf("name must be at least 4 characters long")
	}
	if len(value) > 255 {
		return fmt.Errorf("name must be 255 characters or fewer")
	}
	return nil
}

func validateURI(value string) error {
	if value == "" {
		return fmt.Errorf("this value is required")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("please enter a valid absolute URI")
	}
	return nil
}

func promptRedirectURIs() ([]string, error) {
	var uris []string
	for i := 0; i < 5; i++ {
		label := fmt.Sprintf("Redirect URI #%d", i+1)
		if i > 0 {
			label += " (leave blank to finish)"
		}

		value, err := pterm.DefaultInteractiveTextInput.Show(label)
		if err != nil {
			return nil, fmt.Errorf("could not read redirect URI: %w", err)
		}
		value = strings.TrimSpace(value)

		if value == "" {
			if i == 0 {
				pterm.Error.Println("at least one redirect URI is required")
				i--
				continue
			}
			break
		}

		if err := validateURI(value); err != nil {
			pterm.Error.Println(err.Error())
			i--
			continue
		}

		uris = append(uris, value)
	}

	return uris, nil
}

func printInstructions(apiURL string, client *api.OAuthClientDetail) {
	authBaseURL := "https://id.nodeops.network"
	authURL := authBaseURL + "/oauth2/auth"
	tokenURL := authBaseURL + "/oauth2/token"
	redirectURI := ""
	if len(client.RedirectUris) > 0 {
		redirectURI = client.RedirectUris[0]
	}

	fmt.Println()
	pterm.NewStyle(pterm.FgLightCyan, pterm.Bold).Printfln("  OAuth Client Instructions")
	fmt.Println()
	pterm.Printfln("  %s  %s", pterm.Gray("Client ID       "), client.ClientID)
	pterm.Printfln("  %s  %s", pterm.Gray("Client Type     "), map[bool]string{true: "Public", false: "Confidential"}[client.Public])
	if client.ClientSecret != nil && *client.ClientSecret != "" {
		pterm.Printfln("  %s  %s", pterm.Gray("Client Secret   "), *client.ClientSecret)
	}
	fmt.Println()
	fmt.Println("Use these settings in your OAuth app or local test client:")
	fmt.Println("  Authorization URL:", authURL)
	fmt.Println("  Token URL:", tokenURL)
	fmt.Println("  Grant types: authorization_code, refresh_token")
	fmt.Println("  Response types: code, id_token")
	fmt.Println("  User info endpoint:", apiURL+"/v1/-/oauth2/userinfo")
	if len(client.RedirectUris) > 0 {
		fmt.Println("  Redirect URIs:")
		for _, redirectURI := range client.RedirectUris {
			fmt.Println("   ", redirectURI)
		}
	}
	fmt.Println()
	fmt.Println("PKCE requirements:")
	fmt.Println("  Enable PKCE with S256.")
	fmt.Println("  Send code_challenge and code_challenge_method=S256 on the authorization request.")
	fmt.Println("  Send the matching code_verifier when redeeming the authorization code.")
	fmt.Println()
	fmt.Println("Client type behavior:")
	if client.Public {
		fmt.Println("  Public client: do not send a client secret.")
		fmt.Println("  Redeem the code with form fields including client_id, code, redirect_uri, and code_verifier.")
	} else {
		fmt.Println("  Confidential client: still use PKCE, and also authenticate at the token endpoint with client_secret_basic.")
		fmt.Println("  Send client_id:client_secret via HTTP Basic auth, plus code, redirect_uri, and code_verifier in the form body.")
	}
	fmt.Println()
	if redirectURI == "" {
		redirectURI = "http://localhost:8080/callback"
	}
	fmt.Println("Authorization request example:")
	fmt.Printf("  %s?response_type=code&client_id=%s&redirect_uri=%s&scope=openid&state=<state>&code_challenge=<code_challenge>&code_challenge_method=S256\n",
		authURL,
		url.QueryEscape(client.ClientID),
		url.QueryEscape(redirectURI),
	)
	fmt.Println()
	fmt.Println("Token exchange notes:")
	fmt.Println("  grant_type=authorization_code")
	fmt.Println("  redirect_uri must exactly match the registered redirect URI")
	fmt.Println("  Include code_verifier from the same login attempt")
	if client.Public {
		fmt.Println("  Public client: include client_id in the form body")
	} else {
		fmt.Println("  Confidential client: use HTTP Basic auth with client_id and client_secret")
	}
	fmt.Println()
	fmt.Println("For the local oauth-client-test app, set:")
	fmt.Printf("  OAUTH_CLIENT_ID=%q\n", client.ClientID)
	if client.ClientSecret != nil && *client.ClientSecret != "" {
		fmt.Printf("  OAUTH_CLIENT_SECRET=%q\n", *client.ClientSecret)
	}
	fmt.Printf("  OAUTH_AUTH_URL=%q\n", authURL)
	fmt.Printf("  OAUTH_TOKEN_URL=%q\n", tokenURL)
	fmt.Printf("  OAUTH_REDIRECT_URL=%q\n", redirectURI)
	fmt.Println("  OAUTH_SCOPES=\"openid\"")
	fmt.Println()
	fmt.Println("This CLI can fetch your client details, but the CreateOS API does not currently return the auth/token endpoints directly.")
	fmt.Println("The URLs above match the tested oauth-client-test setup. If your deployment uses a different identity host, replace them there.")
	fmt.Println()
}
