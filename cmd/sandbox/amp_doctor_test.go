package sandbox

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func TestAmpDoctorTemplate(t *testing.T) {
	for _, tc := range []struct {
		name           string
		code           int
		state          string
		ready, wantErr bool
		contains       string
	}{
		{"missing", 404, "", false, false, "--build-template --template custom-orb"},
		{"ready", 200, "ready", true, false, "is ready"},
		{"building", 200, "building", false, false, "template logs custom-orb --follow"},
		{"failed", 200, "failed", false, false, "is failed"},
		{"unauthorized", 401, "", false, true, ""},
		{"unavailable", 503, "", false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/templates/custom-orb" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "tpl-test", "status": tc.state}})
			}))
			defer srv.Close()
			client := api.NewSandboxClient("test", srv.URL, false)
			var out bytes.Buffer
			ready, err := ampDoctorTemplate(t.Context(), &client, "custom-orb", &out)
			if ready != tc.ready || (err != nil) != tc.wantErr {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
			if !strings.Contains(out.String(), tc.contains) {
				t.Fatalf("output: %s", out.String())
			}
			if tc.wantErr && strings.Contains(out.String(), "missing") {
				t.Fatal("lookup error reported as missing")
			}
		})
	}
}

func TestAmpBuildTemplateUsesEmbeddedDockerfile(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "GET":
			if r.URL.Path != "/v1/templates/custom-orb" {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"fail","data":"not found"}`))
		case "POST":
			posted = true
			if r.URL.Path != "/v1/templates" {
				t.Errorf("path = %s", r.URL.Path)
			}
			var req api.TemplateCreateReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Name != "custom-orb" || req.Dockerfile != ampDockerfile {
				t.Errorf("wrong template payload")
			}
			_, _ = w.Write([]byte(`{"data":{"id":"tpl-test","name":"custom-orb","status":"ready"}}`))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()
	client := api.NewSandboxClient("test", srv.URL, false)
	app := &cli.App{Metadata: map[string]interface{}{api.SandboxClientKey: &client}, Commands: []*cli.Command{newSetupAmpCommand()}}
	if err := app.Run([]string{"test", "amp", "--build-template", "--template", "custom-orb"}); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("template was not submitted")
	}
}
