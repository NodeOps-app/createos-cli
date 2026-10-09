package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/config"
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
		})
	}
}
