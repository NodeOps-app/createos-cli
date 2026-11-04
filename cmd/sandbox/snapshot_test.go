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

func TestSnapshotCommands(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		method, path string
		status       int
		body         string
		wantError    bool
	}{
		{"capture", []string{"snapshot", "create", "--type", "full", "--name", "Checkpoint", "sb-test"}, "POST", "/v1/sandboxes/sb-test/snapshots", 202, `{"status":"success","data":{"id":"snap_test","status":"creating","type":"full"}}`, false},
		{"list", []string{"snapshot", "list", "--limit", "2", "--offset", "3"}, "GET", "/v1/snapshots", 200, `{"status":"success","data":{"data":[{"id":"snap_test"}],"pagination":{"total":4}}}`, false},
		{"get", []string{"snapshot", "get", "snap_test"}, "GET", "/v1/snapshots/snap_test", 200, `{"status":"success","data":{"id":"snap_test","status":"ready"}}`, false},
		{"delete", []string{"snapshot", "rm", "--force", "snap_test"}, "DELETE", "/v1/snapshots/snap_test", 202, `{"status":"success","data":{"id":"snap_test","status":"deleted"}}`, false},
		{"restore inherits sizing", []string{"create", "--snapshot", "snap_test"}, "POST", "/v1/sandboxes", 200, `{"status":"success","data":{"id":"sb-restored"}}`, false},
		{"not ready", []string{"snapshot", "get", "snap_missing"}, "GET", "/v1/snapshots/snap_missing", 404, `{"status":"fail","data":"snapshot not found"}`, true},
		{"invalid type", []string{"snapshot", "create", "--type", "bad", "sb-test"}, "", "", 0, "", true},
		{"conflicting source", []string{"create", "--snapshot", "snap_test", "--rootfs", "devbox:1"}, "", "", 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "capture" && r.Method == "GET" && r.URL.Path == "/v1/sandboxes" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"status":"success","data":{"data":[{"id":"sb-test"}],"pagination":{}}}`))
					return
				}
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if tc.name == "list" && (r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("offset") != "3") {
					t.Error("pagination missing")
				}
				if r.Method == "POST" {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if tc.name == "capture" && (body["type"] != "full" || body["name"] != "Checkpoint") {
						t.Errorf("capture body = %v", body)
					}
					if tc.name == "restore inherits sizing" && (body["snapshot_id"] != "snap_test" || body["shape"] != "" || body["rootfs"] != nil || body["disk_mib"] != nil) {
						t.Errorf("restore body = %v", body)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := api.NewSandboxClient("test-key", server.URL, false)
			app := &cli.App{Commands: []*cli.Command{NewSandboxCommand()}, Metadata: map[string]any{api.SandboxClientKey: &client, output.FormatKey: "json"}}
			args := append([]string{"createos", "sandbox"}, tc.args...)
			err := app.Run(args)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tc.wantError)
			}
			wantCalls := 1
			if tc.method == "" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}
