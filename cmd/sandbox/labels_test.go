package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/urfave/cli/v2"
)

func TestLabelCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		method string
		calls  int
	}{
		{"single label list", []string{"list", "--all", "--quiet", "--label", "team=backend"}, "GET", 1},
		{"create", []string{"create", "--shape", "s-1vcpu-1gb", "--label", "team=backend", "--label", "value=a,b=c"}, "POST", 1},
		{"active list", []string{"list", "--quiet", "--label", "team=backend", "--label", "value=a,b=c"}, "GET", 5},
		{"all list", []string{"list", "--all", "--quiet", "--label", "team=backend", "--label", "value=a,b=c"}, "GET", 1},
		{"status list", []string{"list", "--status", "paused", "--quiet", "--label", "team=backend", "--label", "value=a,b=c"}, "GET", 1},
		{"edit", []string{"edit", "--label", "team=backend", "sb-test", "--label=value=a,b=c"}, "PATCH", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// Name resolution performs an unfiltered list before editing.
				if tc.method == "PATCH" && r.Method == "GET" {
					_, _ = w.Write([]byte(`{"data":{"data":[{"id":"sb-test"}]}}`))
					return
				}
				calls++
				if r.Method != tc.method {
					t.Errorf("method = %s", r.Method)
				}
				var labels map[string]string
				if r.Method == "GET" {
					if err := json.Unmarshal([]byte(r.URL.Query().Get("labels")), &labels); err != nil {
						t.Error(err)
					}
					_, _ = w.Write([]byte(`{"data":{"data":[]}}`))
				} else {
					var body struct {
						Labels map[string]string `json:"labels"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					labels = body.Labels
					_, _ = w.Write([]byte(`{"data":{"id":"sb-test","labels":{"team":"backend"}}}`))
				}
				want := map[string]string{"team": "backend", "value": "a,b=c"}
				if tc.name == "single label list" {
					delete(want, "value")
				}
				if !reflect.DeepEqual(labels, want) {
					t.Errorf("labels = %#v", labels)
				}
			}))
			defer srv.Close()
			client := api.NewSandboxClient("test", srv.URL, false)
			app := &cli.App{Metadata: map[string]interface{}{api.SandboxClientKey: &client}, Commands: []*cli.Command{newCreateCommand(), newListCommand(), newEditCommand()}}
			if err := app.Run(append([]string{"test"}, tc.args...)); err != nil {
				t.Fatal(err)
			}
			if calls != tc.calls {
				t.Errorf("calls = %d, want %d", calls, tc.calls)
			}
		})
	}
}

func TestParseLabelFlags(t *testing.T) {
	for _, raw := range [][]string{{"missing"}, {"=value"}, {"  =value"}, {strings.Repeat("k", 129) + "=v"}, {"key=" + strings.Repeat("v", 1025)}, {"key=\x00"}} {
		if _, err := parseLabelFlags(raw); err == nil {
			t.Errorf("accepted %#v", raw)
		}
	}
	got, err := parseLabelFlags([]string{"team=old", "team=new", "empty="})
	if err != nil || !reflect.DeepEqual(got, map[string]string{"team": "new", "empty": ""}) {
		t.Fatalf("got %#v, %v", got, err)
	}
	if got := formatLabels(map[string]string{"z": "last", "a": "first"}); got != "a=first, z=last" {
		t.Fatal(got)
	}
}
