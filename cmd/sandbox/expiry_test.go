package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func TestExpiryCreate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		flags         []string
		minutes       float64
		warn, invalid bool
	}{
		{"omitted", nil, 0, false, false},
		{"minimum", []string{"--timeout-minutes", "2"}, 2, false, false},
		{"maximum warning", []string{"--timeout-minutes", "1440", "--sigterm-before-expiry"}, 1440, true, false},
		{"too short", []string{"--timeout-minutes", "1"}, 0, false, true},
		{"too long", []string{"--timeout-minutes", "1441"}, 0, false, true},
		{"fraction", []string{"--timeout-minutes", "2.5"}, 0, false, true},
		{"overflow", []string{"--timeout-minutes", "4294967298"}, 0, false, true},
		{"warning requires timeout", []string{"--sigterm-before-expiry"}, 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if tc.minutes == 0 {
					if _, ok := body["timeout_minutes"]; ok {
						t.Error("omitted lifetime was sent")
					}
				} else if body["timeout_minutes"] != tc.minutes {
					t.Errorf("body = %v", body)
				}
				if tc.warn && body["sigterm_before_expiry"] != true {
					t.Error("missing warning")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"success","data":{"id":"sb-test","expires_at":"2026-11-23T12:00:00Z","sigterm_before_expiry":true}}`))
			}))
			defer server.Close()
			client := api.NewSandboxClient("key", server.URL, false)
			app := &cli.App{Commands: []*cli.Command{newCreateCommand()}, Metadata: map[string]any{api.SandboxClientKey: &client, output.FormatKey: "json"}}
			args := append([]string{"test", "create", "--shape", "s-1vcpu-256mb", "--rootfs", "alpine:3.20"}, tc.flags...)
			err := app.Run(args)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v", err)
			}
			want := 1
			if tc.invalid {
				want = 0
			}
			if calls != want {
				t.Errorf("calls=%d want=%d", calls, want)
			}
		})
	}
}

func TestExpiryResponses(t *testing.T) {
	body := []byte(`{"expires_at":"2026-11-23T12:00:00Z","sigterm_before_expiry":true}`)
	var created api.SandboxCreateResp
	var view api.SandboxView
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if created.ExpiresAt == nil || view.ExpiresAt == nil || !created.SigtermBeforeExpiry || !view.SigtermBeforeExpiry {
		t.Fatal("expiry metadata lost")
	}
	table := sandboxListTable([]api.SandboxView{view}, 200, true)
	found := false
	for i, col := range table[0] {
		if col == "Expires" {
			found = true
			if table[1][i] == "-" {
				t.Fatal("expiry missing")
			}
		}
	}
	if !found {
		t.Fatal("expiry column missing")
	}
}

func TestRunExpiryFlags(t *testing.T) {
	for _, args := range [][]string{
		{"test", "run", "--timeout-minutes", "30", "--sigterm-before-expiry", "nginx"},
		{"test", "run", "nginx", "--timeout-minutes=30", "--sigterm-before-expiry"},
	} {
		cmd := newRunCommand()
		cmd.Action = func(c *cli.Context) error {
			opts, err := parseRunArgs(c)
			if err != nil {
				return err
			}
			minutes, err := parseSandboxTimeout(opts.timeoutRaw, opts.sigtermBeforeExpiry)
			if err != nil {
				return err
			}
			if minutes == nil || *minutes != 30 || !opts.sigtermBeforeExpiry || opts.image != "nginx" {
				t.Fatalf("unexpected expiry options: %+v", opts)
			}
			return nil
		}
		app := &cli.App{Commands: []*cli.Command{cmd}}
		if err := app.Run(args); err != nil {
			t.Fatal(err)
		}
	}
}
