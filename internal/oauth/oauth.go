package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// ServerMetadata holds the OAuth authorization server metadata (RFC 8414)
type ServerMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// TokenResponse holds the token endpoint response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// PKCEPair holds a PKCE code_verifier and code_challenge
type PKCEPair struct {
	Verifier  string
	Challenge string
}

// FetchServerMetadata fetches OAuth server metadata from {baseURL}/.well-known/openid-configuration
func FetchServerMetadata(baseURL string) (*ServerMetadata, error) {
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + "/.well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("could not reach authorization server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorization server returned status %d", resp.StatusCode)
	}
	var meta ServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("could not parse server metadata: %w", err)
	}
	return &meta, nil
}

// GeneratePKCE generates a PKCE code_verifier and code_challenge (S256)
func GeneratePKCE() (*PKCEPair, error) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("could not generate PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PKCEPair{Verifier: verifier, Challenge: challenge}, nil
}

// GenerateState generates a random state string for CSRF protection
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// BuildAuthURL constructs the authorization URL with PKCE parameters
func BuildAuthURL(authEndpoint, clientID, redirectURI, state, challenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "openid offline_access")
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	return authEndpoint + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for tokens
func ExchangeCode(tokenEndpoint, clientID, code, redirectURI, verifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)

	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(b))
	}
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("could not parse token response: %w", err)
	}
	return &token, nil
}

// RefreshTokens exchanges a refresh token for new tokens
func RefreshTokens(tokenEndpoint, clientID, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh returned status %d: %s", resp.StatusCode, string(b))
	}
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("could not parse token refresh response: %w", err)
	}
	return &token, nil
}

// StartCallbackServer starts a local HTTP server and waits for the OAuth callback.
// Returns the code and state from the redirect.
func StartCallbackServer(port int) (code string, state string, err error) {
	codeCh := make(chan struct {
		code  string
		state string
		err   error
	}, 1)

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", port),
		Handler: mux,
	}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		errParam := q.Get("error")
		if errParam != "" {
			fmt.Fprintf(w, "<html><body><h2>Login failed: %s</h2><p>You can close this tab.</p></body></html>", errParam)
			codeCh <- struct {
				code  string
				state string
				err   error
			}{"", "", fmt.Errorf("authorization error: %s", errParam)}
			return
		}
		fmt.Fprint(w, "<html><body><h2>Login successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>")
		codeCh <- struct {
			code  string
			state string
			err   error
		}{q.Get("code"), q.Get("state"), nil}
	})

	go func() {
		if serveErr := srv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			codeCh <- struct {
				code  string
				state string
				err   error
			}{"", "", fmt.Errorf("callback server error: %w", serveErr)}
		}
	}()

	result := <-codeCh
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)

	return result.code, result.state, result.err
}

// OpenBrowser opens the given URL in the default system browser
func OpenBrowser(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", rawURL}
	default:
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	// Use exec.Command — import os/exec
	return runCmd(cmd, args...)
}

// FindFreePort finds a free TCP port on localhost
func FindFreePort() (int, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("could not find a free port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}
