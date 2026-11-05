package sandbox

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func TestCheckpointCommands(t *testing.T) {
	const cp = "cp_AAAAAAAAAAAAAAAAAAAAAA"
	for _, tc := range []struct {
		name                 string
		args                 []string
		method, suffix, body string
		status               int
		wantError            bool
	}{
		{"create", []string{"create", "--name", "before", "sb-test", "/workspace/project"}, "POST", "", `{"id":"` + cp + `","path":"/workspace/project"}`, 200, false},
		{"list", []string{"list", "sb-test"}, "GET", "", `[{"id":"` + cp + `","path":"/workspace/project"}]`, 200, false},
		{"restore", []string{"restore", "--force", "sb-test", cp}, "POST", "/" + cp + "/restore", `{"checkpoint_id":"` + cp + `","path":"/workspace/project"}`, 200, false},
		{"delete", []string{"rm", "--force", "sb-test", cp}, "DELETE", "/" + cp, `{}`, 200, false},
		{"busy", []string{"create", "sb-test", "/workspace/project"}, "POST", "", `"directory busy"`, 409, true},
		{"not found", []string{"restore", "--force", "sb-test", cp}, "POST", "/" + cp + "/restore", `"not found"`, 404, true},
		{"relative path", []string{"create", "sb-test", "workspace"}, "", "", "", 0, true},
		{"bad id", []string{"restore", "--force", "sb-test", "name"}, "", "", "", 0, true},
		{"restore requires confirmation", []string{"restore", "sb-test", cp}, "", "", "", 0, true},
		{"delete requires confirmation", []string{"rm", "sb-test", cp}, "", "", "", 0, true},
		{"extra argument", []string{"list", "sb-test", "extra"}, "", "", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != "/v1/sandboxes/sb-test/checkpoints"+tc.suffix {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if r.URL.RawQuery != "" {
					t.Error("query parameters must be absent")
				}
				if r.Header.Get("X-Api-Key") != "test-key" {
					t.Error("missing auth header")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if tc.method == "POST" && tc.suffix == "" {
					var req map[string]any
					if err := json.Unmarshal(body, &req); err != nil {
						t.Fatal(err)
					}
					if req["path"] != "/workspace/project" {
						t.Errorf("body = %s", body)
					}
					if tc.name == "create" && req["name"] != "before" {
						t.Error("name missing")
					}
				} else if len(body) != 0 {
					t.Errorf("unexpected request body %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				status := "success"
				if tc.status >= 400 {
					status = "fail"
				}
				_, _ = io.WriteString(w, `{"status":"`+status+`","data":`+tc.body+`}`)
			}))
			defer server.Close()
			client := api.NewSandboxClient("test-key", server.URL, false)
			app := &cli.App{Commands: []*cli.Command{NewSandboxCommand()}, Metadata: map[string]any{api.SandboxClientKey: &client, output.FormatKey: "json"}}
			err := app.Run(append([]string{"createos", "sandbox", "checkpoint"}, tc.args...))
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			want := 1
			if tc.method == "" {
				want = 0
			}
			if calls != want {
				t.Errorf("calls = %d, want %d", calls, want)
			}
			if tc.name == "busy" && !strings.Contains(err.Error(), "busy") {
				t.Errorf("lost API error: %v", err)
			}
		})
	}
}
