package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// These tests cover the failure paths, not the happy ones. Every one of
// them exists because a success path that leaks a billable sandbox looks
// exactly like a success path that does not.

// fakeAPI is a stand-in sandbox control plane. Handlers are matched by
// "METHOD /path" with {id} already substituted, so a test only declares
// the calls it cares about; anything else is a 404 the test can assert on.
type fakeAPI struct {
	t        *testing.T
	mu       sync.Mutex
	seen     []string
	handlers map[string]http.HandlerFunc
	srv      *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, handlers: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.seen = append(f.seen, key)
		h, ok := f.handlers[key]
		f.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":"no handler: `+key+`"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) on(key string, h http.HandlerFunc) *fakeAPI {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[key] = h
	return f
}

func (f *fakeAPI) json(key, body string) *fakeAPI {
	return f.on(key, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

// fails makes an endpoint answer 500. Every caller wants the same thing —
// "this control-plane call is broken right now" — so the status is fixed
// rather than a parameter nobody varies.
func (f *fakeAPI) fails(key string) *fakeAPI {
	return f.on(key, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})
}

func (f *fakeAPI) called(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.seen {
		if s == key {
			return true
		}
	}
	return false
}

func (f *fakeAPI) client() *api.SandboxClient {
	c := api.NewSandboxClient("tok", f.srv.URL, false)
	return &c
}

// shortPoll makes waitForStatus give up quickly. Without it these tests
// would sit through the production 5-minute timeout.
func shortPoll(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestCreateComposeBoxDestroysWhenNeverReady covers the leak Codex found:
// CreateSandbox succeeds, readiness polling does not, and the caller only
// ever sees an error — so if createComposeBox does not destroy the box
// itself, nothing will.
func TestCreateComposeBoxDestroysWhenNeverReady(t *testing.T) {
	f := newFakeAPI(t).
		json("POST /v1/sandboxes", `{"data":{"id":"sb-stuck"}}`).
		json("GET /v1/sandboxes/sb-stuck", `{"data":{"id":"sb-stuck","status":"failed"}}`).
		json("DELETE /v1/sandboxes/sb-stuck", `{"data":{"id":"sb-stuck","status":"destroying"}}`)

	_, err := createComposeBox(shortPoll(t), f.client(), &composeOptions{Shape: "s-1vcpu-1gb"})
	if err == nil {
		t.Fatal("want an error when the sandbox never reaches running")
	}
	if !f.called("DELETE /v1/sandboxes/sb-stuck") {
		t.Error("sandbox was created and never destroyed — it is still billable")
	}
	if !strings.Contains(err.Error(), "sb-stuck") {
		t.Errorf("error must name the sandbox id, got: %v", err)
	}
}

// TestCreateComposeBoxReportsUndestroyableBox is the worse branch: the box
// exists and teardown also failed, so the id must reach the user with a
// command they can run by hand.
func TestCreateComposeBoxReportsUndestroyableBox(t *testing.T) {
	f := newFakeAPI(t).
		json("POST /v1/sandboxes", `{"data":{"id":"sb-orphan"}}`).
		json("GET /v1/sandboxes/sb-orphan", `{"data":{"id":"sb-orphan","status":"failed"}}`).
		fails("DELETE /v1/sandboxes/sb-orphan")

	_, err := createComposeBox(shortPoll(t), f.client(), &composeOptions{Shape: "s-1vcpu-1gb"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"sb-orphan", "still billable", "rm --force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q so the user can clean up, got: %v", want, err)
		}
	}
}

// TestForkNReportsCloneCreatedBeforePollFailed covers the orphan Codex
// found: ForkSandbox returns a real, running clone, then the status poll
// fails. The clone is not in the settled list, so unless the error carries
// its id, nothing can ever clean it up.
func TestForkNReportsCloneCreatedBeforePollFailed(t *testing.T) {
	f := newFakeAPI(t).
		json("GET /v1/sandboxes/sb-golden", `{"data":{"id":"sb-golden","status":"paused"}}`).
		json("POST /v1/sandboxes/sb-golden/fork", `{"data":{"id":"sb-clone-1","status":"forking"}}`).
		json("GET /v1/sandboxes/sb-clone-1", `{"data":{"id":"sb-clone-1","status":"failed"}}`)

	forks, err := forkN(shortPoll(t), f.client(), "sb-golden", api.SandboxForkReq{}, 1, nil)
	if err == nil {
		t.Fatal("want an error when the clone never settles")
	}
	if len(forks) != 0 {
		t.Errorf("settled forks = %d, want 0", len(forks))
	}

	var leak *forkLeak
	if !errors.As(err, &leak) {
		t.Fatalf("error must be a *forkLeak carrying the created id, got %T: %v", err, err)
	}
	if len(leak.IDs) != 1 || leak.IDs[0] != "sb-clone-1" {
		t.Errorf("leak.IDs = %v, want [sb-clone-1]", leak.IDs)
	}
	if !strings.Contains(err.Error(), "sb-clone-1") {
		t.Errorf("message must name the clone, got: %v", err)
	}
}

// TestMatrixRunOneSurfacesDestroyFailure pins the named-return fix. The
// teardown runs in a defer; with an unnamed return Go copies the result
// before defers run, so a failed destroy never reached the caller and the
// matrix exited 0 while a clone kept billing.
func TestMatrixRunOneSurfacesDestroyFailure(t *testing.T) {
	f := newFakeAPI(t).
		json("GET /v1/sandboxes/sb-golden", `{"data":{"id":"sb-golden","status":"paused"}}`).
		json("POST /v1/sandboxes/sb-golden/fork", `{"data":{"id":"sb-clone","status":"running"}}`).
		json("GET /v1/sandboxes/sb-clone", `{"data":{"id":"sb-clone","status":"running"}}`).
		json("PATCH /v1/sandboxes/sb-clone", `{"data":{"id":"sb-clone","status":"running"}}`).
		json("POST /v1/sandboxes/sb-clone/processes", `{"data":{"process_id":"p1","state":"running"}}`).
		on("GET /v1/sandboxes/sb-clone/processes/p1/connect", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintln(w, `{"type":"exit","exit_code":0}`)
		}).
		fails("DELETE /v1/sandboxes/sb-clone")

	res := matrixRunOne(shortPoll(t), f.client(), "sb-golden", 0, "true", t.TempDir(),
		&composeOptions{Shape: "s-1vcpu-1gb", AutoPause: time.Minute})

	if res.ExitCode != 0 {
		t.Fatalf("job exit code = %d, want 0 — the command itself passed", res.ExitCode)
	}
	if res.Error == "" {
		t.Fatal("destroy failed but the job reported no error — matrix would exit 0 having leaked sb-clone")
	}
	if !strings.Contains(res.Error, "sb-clone") {
		t.Errorf("error must name the leaked clone, got: %q", res.Error)
	}
}

// TestUntarIntoRefusesSymlinkAncestor is the extraction-boundary
// regression. A lexical prefix check passes here, because the pathname
// this code builds stays under root; the escape happens when the
// filesystem follows a symlink that was already on disk.
func TestUntarIntoRefusesSymlinkAncestor(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "report.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The trap: a symlink that already exists inside the extraction root.
	if err := os.Symlink(outside, filepath.Join(root, "coverage")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("owned")
	if err := tw.WriteHeader(&tar.Header{
		Name: "coverage/report.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	err := untarInto(&buf, root)

	got, readErr := os.ReadFile(victim) // #nosec G304 -- path built from t.TempDir()
	if readErr != nil {
		t.Fatalf("read victim: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("archive wrote through the symlink and overwrote %s (untarInto err=%v)", victim, err)
	}
	if err == nil {
		t.Error("want an error for an entry whose parent escapes the root")
	}
}

// TestForkRefusesToPauseARunningSandbox is the guard against the worst
// version of this command: `createos sandbox fork my-live-server` pausing
// a service somebody is using, as a side effect of asking for a clone.
// Fork must never stop a running workload on its own.
func TestForkRefusesToPauseARunningSandbox(t *testing.T) {
	f := newFakeAPI(t).
		json("GET /v1/sandboxes/sb-live", `{"data":{"id":"sb-live","status":"running"}}`)

	err := ensureForkable(shortPoll(t), f.client(), "sb-live")
	if err == nil {
		t.Fatal("want a refusal — forking must not pause a running sandbox")
	}
	if f.called("POST /v1/sandboxes/sb-live/pause") {
		t.Error("fork paused a running sandbox on its own; whatever it was serving just stopped")
	}
	for _, want := range []string{"running", "createos sandbox pause sb-live"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q so the user can act, got: %v", want, err)
		}
	}
}

// pauseForFork is the opposite case: matrix built the golden sandbox, so
// it is allowed to pause it.
func TestPauseForForkPausesASandboxTheCallerOwns(t *testing.T) {
	pauses := 0
	f := newFakeAPI(t).
		on("GET /v1/sandboxes/sb-golden", func(w http.ResponseWriter, _ *http.Request) {
			status := "running"
			if pauses > 0 {
				status = "paused"
			}
			_, _ = fmt.Fprintf(w, `{"data":{"id":"sb-golden","status":%q}}`, status)
		}).
		on("POST /v1/sandboxes/sb-golden/pause", func(w http.ResponseWriter, _ *http.Request) {
			pauses++
			_, _ = w.Write([]byte(`{"data":{"id":"sb-golden","status":"pausing"}}`))
		})

	if err := pauseForFork(shortPoll(t), f.client(), "sb-golden"); err != nil {
		t.Fatalf("pauseForFork: %v", err)
	}
	if pauses != 1 {
		t.Errorf("pause called %d times, want 1", pauses)
	}
}

// TestForkRejectsBadCountBeforeAnyAPICall covers the bug where --count was
// only checked after resolving the source ref, so `fork --count 0 missing`
// spent an API round trip on "missing" before ever complaining about the
// count. The count is knowable from the flags alone, so it must fail before
// any request goes out.
func TestForkRejectsBadCountBeforeAnyAPICall(t *testing.T) {
	f := newFakeAPI(t)
	app := &cli.App{
		Commands: []*cli.Command{newForkCommand()},
		Metadata: map[string]any{api.SandboxClientKey: f.client()},
	}

	err := app.RunContext(shortPoll(t), []string{"createos", "fork", "--count", "0", "missing"})
	if err == nil {
		t.Fatal("want an error for --count 0")
	}
	if !strings.Contains(err.Error(), "--count must be at least 1 (got 0)") {
		t.Errorf("error = %q, want it to name the bad count", err)
	}
	if len(f.seen) != 0 {
		t.Errorf("count was invalid but the CLI still called the API: %v", f.seen)
	}
}

// TestOffloadFailsWhenTeardownFails covers the leak that looks like a
// clean run: the workload passes, DestroySandbox fails, and a CI job
// reading only the exit status would never learn that a billable sandbox
// was left behind. `offload` promises a throwaway sandbox, so a teardown
// failure has to reach the exit code.
func TestOffloadFailsWhenTeardownFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := newFakeAPI(t).
		json("POST /v1/sandboxes", `{"data":{"id":"sb-off"}}`).
		json("GET /v1/sandboxes/sb-off", `{"data":{"id":"sb-off","status":"running"}}`).
		json("PUT /v1/sandboxes/sb-off/files", `{"data":{}}`).
		json("POST /v1/sandboxes/sb-off/exec", `{"data":{"result":{"exit_code":0}}}`).
		json("POST /v1/sandboxes/sb-off/processes", `{"data":{"process_id":"p1","state":"running"}}`).
		on("GET /v1/sandboxes/sb-off/processes/p1/connect", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintln(w, `{"type":"exit","exit_code":0}`)
		}).
		fails("DELETE /v1/sandboxes/sb-off")

	app := &cli.App{
		Commands: []*cli.Command{newOffloadCommand()},
		Metadata: map[string]any{api.SandboxClientKey: f.client()},
	}
	err := app.RunContext(shortPoll(t), []string{"createos", "offload", dir, "--", "true"})

	if err == nil {
		t.Fatal("workload passed but the sandbox leaked, and offload reported success")
	}
	for _, want := range []string{"sb-off", "not destroyed", "rm --force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q, got: %v", want, err)
		}
	}
}
