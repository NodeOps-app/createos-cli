package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/config"
	internaloauth "github.com/NodeOps-app/createos-cli/internal/oauth"
)

func TestLoginWithAPITokenValidatesBeforeSaving(t *testing.T) {
	for _, tc := range []struct {
		name       string
		token      string
		statusCode int
		wantToken  string
		wantError  string
	}{
		{"valid", " valid-token ", http.StatusOK, "valid-token", ""},
		{"invalid", "abcd", http.StatusUnauthorized, "existing-token", "could not verify your API token"},
		{"server error", "valid-token", http.StatusServiceUnavailable, "existing-token", "could not verify your API token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if err := config.SaveToken("existing-token"); err != nil {
				t.Fatal(err)
			}
			previousSession := config.OAuthSession{ // #nosec G101 -- dummy credentials used only in this test
				AccessToken: "previous-access-token", RefreshToken: "previous-refresh-token",
				ExpiresAt: time.Now().Unix() + 3600, TokenEndpoint: "https://example.com/token",
			}
			if err := config.SaveOAuthSession(previousSession); err != nil {
				t.Fatal(err)
			}
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/v1/users/me" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("X-Api-Key"); got != strings.TrimSpace(tc.token) {
					t.Errorf("API key header = %q, want %q", got, strings.TrimSpace(tc.token))
				}
				if got := r.UserAgent(); got != "createos-cli" {
					t.Errorf("User-Agent = %q, want createos-cli", got)
				}
				w.WriteHeader(tc.statusCode)
				if tc.statusCode == http.StatusOK {
					_, _ = w.Write([]byte(`{"status":"success","data":{"id":"user-1"}}`))
				} else {
					_, _ = w.Write([]byte(`{"status":"fail","data":"invalid api key"}`))
				}
			}))
			defer server.Close()

			app := &cli.App{
				Flags:    []cli.Flag{&cli.StringFlag{Name: "api-url"}},
				Commands: []*cli.Command{NewLoginCommand()},
			}
			err := app.Run([]string{"createos", "--api-url", server.URL, "login", "--token", tc.token})
			if tc.wantError == "" && err != nil {
				t.Fatalf("login failed: %v", err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("login error = %v, want %q", err, tc.wantError)
			}
			if requests != 1 {
				t.Errorf("requests = %d, want 1", requests)
			}
			gotToken, err := config.LoadToken()
			if err != nil {
				t.Fatal(err)
			}
			if gotToken != tc.wantToken {
				t.Errorf("saved token = %q, want %q", gotToken, tc.wantToken)
			}
			session, err := config.LoadOAuthSession()
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError == "" {
				if session != nil {
					t.Error("successful API-token login retained the previous OAuth session")
				}
			} else if session == nil || *session != previousSession {
				t.Error("failed API-token login changed the previous OAuth session")
			}
		})
	}
}

func TestSaveLoginSessionReplacesPreviousCredentials(t *testing.T) {
	for _, saveFails := range []bool{false, true} {
		name := "success"
		if saveFails {
			name = "save failure preserves API token"
		}
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			if err := config.SaveToken("previous-api-token"); err != nil {
				t.Fatal(err)
			}
			if saveFails {
				// A directory at the session path prevents writing the new session.
				if err := os.Mkdir(filepath.Join(homeDir, ".createos", ".oauth"), 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := config.SaveOAuthSession(config.OAuthSession{AccessToken: "previous-access-token"}); err != nil {
				t.Fatal(err)
			}

			token := &internaloauth.TokenResponse{
				AccessToken: "new-access-token", RefreshToken: "new-refresh-token",
				ExpiresIn: 3600, TokenType: "Bearer",
			}
			endpoint := "https://example.com/token"
			started := time.Now().Unix()
			saveErr := saveLoginSession(token, endpoint)
			if saveFails {
				if saveErr == nil || !strings.Contains(saveErr.Error(), "could not save your session") {
					t.Fatalf("save error = %v, want session save failure", saveErr)
				}
				if got, err := config.LoadToken(); err != nil || got != "previous-api-token" {
					t.Fatalf("previous API token was not preserved: token=%q, err=%v", got, err)
				}
				return
			}
			if saveErr != nil {
				t.Fatal(saveErr)
			}
			if _, err := os.Stat(filepath.Join(homeDir, ".createos", ".token")); !os.IsNotExist(err) {
				t.Fatalf("previous API token was not removed: %v", err)
			}
			session, err := config.LoadOAuthSession()
			if err != nil || session == nil {
				t.Fatalf("load session: session=%v, err=%v", session, err)
			}
			if session.AccessToken != token.AccessToken || session.RefreshToken != token.RefreshToken || session.TokenEndpoint != endpoint {
				t.Error("saved session does not match the new login")
			}
			if session.ExpiresAt < started+3600 || session.ExpiresAt > time.Now().Unix()+3600 {
				t.Errorf("unexpected session expiry: %d", session.ExpiresAt)
			}
		})
	}
}
