package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// The same session must always map to the same sandbox, or resume after
// suspend would boot an empty one.
func TestDevinSandboxNameIsShortAndStable(t *testing.T) {
	// printf %s devin-T-0001 | shasum -a 256 | cut -c1-18. Pinned: changing the
	// scheme would orphan every paused session that already exists.
	if name := devinSandboxName("devin-T-0001"); name != "dvn-eb6c41717d735aea23" {
		t.Fatalf("devinSandboxName = %q", name)
	}
	if devinSandboxName("devin-T-0001") == devinSandboxName("devin-T-0002") {
		t.Fatal("different sessions share a sandbox name")
	}
	if n := len(devinSandboxName(strings.Repeat("x", 200))); n != 22 {
		t.Fatalf("long session ID gave %d chars", n)
	}
}

// Session IDs and SHAs come from Devin's API and reach a path and a URL in the sandbox.
func TestDevinInputPatternsRejectShellSyntax(t *testing.T) {
	for _, bad := range []string{"", "a b", "$(id)", "x;id", "../x", "a/b", strings.Repeat("a", 129)} {
		if devinSessionIDRE.MatchString(bad) {
			t.Errorf("session ID pattern accepted %q", bad)
		}
	}
	if !devinSessionIDRE.MatchString("devin-0123abcd_EF") {
		t.Error("session ID pattern rejected a plain ID")
	}
	for _, bad := range []string{"F62A3AADB1", "f62a3", "f62a3aadb1/../x", "f62a3aadb1 x"} {
		if devinSHARE.MatchString(bad) {
			t.Errorf("SHA pattern accepted %q", bad)
		}
	}
	if !devinSHARE.MatchString("f62a3aadb1") {
		t.Error("SHA pattern rejected a short SHA")
	}
}

// fakeDevin serves the queue for session s1 and answers 409 for s2.
type fakeDevin struct {
	mu       sync.Mutex
	status   string // session_status reported for s1
	released []string
	claims   []string
}

func (f *fakeDevin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := func(id string, withToken bool) map[string]any {
		st := map[string]any{"phase": "claimed", "session_status": f.status}
		if withToken {
			st["connect_token"], st["gateway_url"] = "tok-"+id, "wss://gw.example"
		}
		return map[string]any{
			"metadata": map[string]any{"session_id": id, "outpost_id": "outpost_env-1"},
			"spec":     map[string]any{"kind": "new", "remote_binary_sha": nil, "network_policy": map[string]any{"enabled": true}},
			"status":   st,
		}
	}
	switch p := strings.TrimPrefix(r.URL.Path, "/opbeta/outposts"); {
	case r.Header.Get("Authorization") != "Bearer devin-secret":
		w.WriteHeader(http.StatusUnauthorized)
	case p == "/devins/s2/claim":
		f.claims = append(f.claims, "s2")
		w.WriteHeader(http.StatusConflict)
	case p == "/devins/s1/claim":
		f.claims = append(f.claims, "s1")
		_ = json.NewEncoder(w).Encode(entry("s1", true)) //nolint:errcheck
	case p == "/devins/s1/release":
		f.released = append(f.released, "s1")
	case p == "/devins/s1":
		_ = json.NewEncoder(w).Encode(entry("s1", false)) //nolint:errcheck
	default:
		http.NotFound(w, r)
	}
}

// fakeCreateOS holds one sandbox, sb1, which starts absent.
type fakeCreateOS struct {
	mu          sync.Mutex
	devin       *fakeDevin
	finalStatus string
	created     map[string]any
	exec        map[string]any
	proc        map[string]any
	waits       int
	calls       []string
}

func (f *fakeCreateOS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
	} //nolint:errcheck
	route := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, route)
	switch route {
	case "GET /v1/sandboxes":
		ok(map[string]any{"data": []any{}, "pagination": map[string]any{"total": 0}})
	case "POST /v1/sandboxes":
		_ = json.NewDecoder(r.Body).Decode(&f.created) //nolint:errcheck
		ok(map[string]any{"id": "sb1"})
	case "GET /v1/sandboxes/sb1":
		ok(map[string]any{"id": "sb1", "status": "running"})
	case "POST /v1/sandboxes/sb1/exec":
		_ = json.NewDecoder(r.Body).Decode(&f.exec) //nolint:errcheck
		fmt.Fprintln(w, `{"stdout":"ok\n"}`)
		fmt.Fprintln(w, `{"exit_code":0}`)
	case "POST /v1/sandboxes/sb1/processes":
		_ = json.NewDecoder(r.Body).Decode(&f.proc) //nolint:errcheck
		ok(map[string]any{"process_id": "p1"})
	case "GET /v1/sandboxes/sb1/processes/p1/wait":
		// Still running on the first wait; then Devin suspends or ends the session.
		if f.waits++; f.waits == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"status":"fail","data":"timeout"}`)) //nolint:errcheck
			return
		}
		f.devin.mu.Lock()
		f.devin.status = f.finalStatus
		f.devin.mu.Unlock()
		ok(map[string]any{"process_id": "p1", "exit_code": 0, "leader_exited": true})
	case "POST /v1/sandboxes/sb1/pause", "DELETE /v1/sandboxes/sb1":
		ok(map[string]any{"id": "sb1"})
	default:
		http.NotFound(w, r)
	}
}

func TestDevinSessionLifecycle(t *testing.T) {
	for _, tc := range []struct{ final, wantCall string }{
		{"suspended", "POST /v1/sandboxes/sb1/pause"},
		{"terminated", "DELETE /v1/sandboxes/sb1"},
	} {
		t.Run(tc.final, func(t *testing.T) {
			devin := &fakeDevin{status: "running"}
			cos := &fakeCreateOS{devin: devin, finalStatus: tc.final}
			devinSrv, cosSrv := httptest.NewServer(devin), httptest.NewServer(cos)
			defer devinSrv.Close()
			defer cosSrv.Close()
			client := api.NewSandboxClient("cos-key", cosSrv.URL, false)
			o := &devinOrch{
				devin: newDevinAPI(devinSrv.URL, "devin-secret"), client: &client,
				outpost: "outpost_env-1", acceptor: "createos-test",
				shape: devinDefaultShape, rootfs: devinDefaultRootfs, busy: map[string]bool{},
			}

			ctx := context.Background()
			o.claimAndServe(ctx, "s2") // lost race: no sandbox
			o.claimAndServe(ctx, "s1")
			o.wg.Wait()

			if !slices.Equal(devin.claims, []string{"s2", "s1"}) {
				t.Errorf("claims = %v, want [s2 s1]", devin.claims)
			}
			if !slices.Equal(devin.released, []string{"s1"}) {
				t.Errorf("released = %v, want [s1]", devin.released)
			}
			if cos.created["name"] != devinSandboxName("s1") {
				t.Errorf("created %v, want name %s", cos.created["name"], devinSandboxName("s1"))
			}
			if args, _ := cos.exec["args"].([]any); len(args) != 5 || args[3] != "" || args[4] != "s1" {
				t.Errorf("provision args = %v, want [-c <script> provision \"\" s1]", cos.exec["args"])
			}
			env, _ := cos.proc["env"].(map[string]any)
			if env["DEVIN_OUTPOST_CONNECT_TOKEN"] != "tok-s1" || env["DEVIN_OUTPOST_GATEWAY_URL"] != "wss://gw.example" {
				t.Errorf("remote env = %v", env)
			}
			// The account-wide Devin token must never reach the sandbox.
			body, _ := json.Marshal(cos.proc) //nolint:errcheck
			if strings.Contains(string(body), "devin-secret") || strings.Contains(fmt.Sprint(cos.created), "devin-secret") {
				t.Error("the Devin API token was sent to the sandbox")
			}
			if !slices.Contains(cos.calls, tc.wantCall) {
				t.Errorf("calls %v lack %s", cos.calls, tc.wantCall)
			}
			if o.count() != 0 {
				t.Errorf("%d sessions still marked busy", o.count())
			}
		})
	}
}
