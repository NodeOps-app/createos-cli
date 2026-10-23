//go:build e2e

package sandbox

// Live end-to-end test of `createos sandbox setup amp --dispatch`, run the way
// Amp's orb provider runs it: the real binary, behind the real wrapper, one
// process per job with the job in env vars. It creates and destroys one real
// sandbox and takes about five minutes, most of it CreateOS pausing and
// resuming. The e2e build tag keeps it out of `go test ./...`.
//
//	CREATEOS_API_KEY=<key> go test -tags e2e -run TestAmpE2E -v -timeout 20m ./cmd/sandbox
//
// CREATEOS_ORB_TEMPLATE=<a ready template> forces the "template ready" path.
// ORB_TOKEN is fake, so Amp rejecting it proves the token reached bootstrap.

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

type ampE2E struct {
	t        *testing.T
	ctx      context.Context
	client   *api.SandboxClient
	wrapper  string
	orbID    string
	name     string
	template string
	id       string
}

func TestAmpE2E(t *testing.T) {
	key := os.Getenv("CREATEOS_API_KEY")
	if key == "" {
		t.Skip("set CREATEOS_API_KEY to run the live Amp orb test")
	}
	ctx := t.Context()
	dir := t.TempDir()
	bin := filepath.Join(dir, "createos")
	// #nosec G204 -- fixed arguments
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "github.com/NodeOps-app/createos-cli").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	e := &ampE2E{
		t:        t,
		ctx:      ctx,
		orbID:    fmt.Sprintf("amp-T-e2e-%d", time.Now().UnixNano()),
		template: envOr("CREATEOS_ORB_TEMPLATE", ampDefaultTemplate),
	}
	client := api.NewSandboxClient(key, os.Getenv("CREATEOS_SANDBOX_URL"), false)
	e.client = &client
	e.name = ampSandboxName(e.orbID)
	e.wrapper = filepath.Join(dir, ampWrapperName)
	// Smaller than the default shape: pause and resume time grows with memory.
	script := ampWrapperScript(bin, envOr("CREATEOS_ORB_SHAPE", "s-2vcpu-4gb"), e.template)
	if err := os.WriteFile(e.wrapper, []byte(script), 0o700); err != nil { // #nosec G306 -- must be executable
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _, _ = e.job(context.Background(), "delete") }) //nolint:errcheck // best effort
	t.Logf("orb %s -> sandbox %s", e.orbID, e.name)

	// Jobs on nothing.
	_, _, err := e.job(ctx, "delete")
	e.check("delete with no sandbox exits 0", err == nil, err)
	_, _, err = e.job(ctx, "delete", "CREATEOS_SANDBOX_URL=https://127.0.0.1:9")
	e.check("delete fails when the API is unreachable", err != nil)
	_, _, err = e.job(ctx, "pause")
	e.check("pause with no sandbox exits 0", err == nil, err)
	_, _, err = e.job(ctx, "bogus")
	e.check("unknown operation exits non-zero", err != nil)

	// Fresh spawn.
	out, took, err := e.job(ctx, "spawn")
	if err != nil {
		t.Fatalf("fresh spawn: %v\n%s", err, out)
	}
	e.check(fmt.Sprintf("fresh spawn within dispatch deadline (%s)", took), took < ampJobDeadline)
	sb, err := ampFind(ctx, e.client, e.name)
	if err != nil || sb == nil {
		t.Fatalf("no live sandbox %s after spawn: %v", e.name, err)
	}
	e.id = sb.ID
	e.checkStatus("sandbox is running", api.SandboxStatusRunning)
	rootfs := ""
	if sb.Rootfs != nil {
		rootfs = *sb.Rootfs
	}
	tpl, tplErr := e.client.GetTemplate(ctx, e.template, false)
	ready := tplErr == nil && tpl.Status == "ready"
	t.Logf("template %s ready=%v; orb booted from %s", e.template, ready, rootfs)
	// A sandbox reports a template rootfs by id, not by name.
	e.check("booted from the template when it is ready", !ready || rootfs == tpl.ID, rootfs)
	e.check("fell back to devbox:1 when it is not",
		ready || (rootfs == ampFallbackRootfs && strings.Contains(out, "booting "+ampFallbackRootfs)), rootfs)
	e.check("submitted the template at most once", strings.Count(out, "building template") <= 1)
	e.checkBox("amp at /usr/local/bin/amp", "/usr/local/bin/amp --version >/dev/null")
	e.checkBox("user has passwordless sudo", "sudo -u user sudo -n true")
	e.checkBox("user home is writable", "sudo -u user test -w /home/user")
	// As user, the account the agent runs as: root can reach tools that user cannot.
	e.checkBox("gh, git, node, tmux, rg, curl usable by user",
		`sudo -u user -H bash -lc 'for b in gh git node tmux rg curl; do command -v $b >/dev/null || exit 1; done'`)
	e.checkBox("apt does not use the slow default Ubuntu mirror",
		`! grep -rqE 'archive\.ubuntu\.com|security\.ubuntu\.com' /etc/apt/sources.list /etc/apt/sources.list.d/ 2>/dev/null`)
	e.checkBox("node is 18 or newer for user",
		`[ "$(sudo -u user -H bash -lc 'node -p "process.versions.node.split(\".\")[0]"')" -ge 18 ]`)
	e.checkBox("hostname resolves", `getent hosts "$(hostname)" >/dev/null`)
	e.checkBox("root shell profile untouched", `! grep -q "Amp CLI" /root/.bashrc`)
	e.check("bootstrap got ORB_TOKEN and reached Amp", strings.Contains(e.bootstrapOutput(out), "ORB_TOKEN was rejected"))

	// Repeat spawn, with a stand-in for a bootstrap left running from before a pause.
	e.box(`sudo -u user setsid bash -c 'exec -a "amp orb bootstrap" sleep 9999' </dev/null >/dev/null 2>&1 &`)
	e.checkBox("stale bootstrap stand-in is running", `pgrep -f "^amp orb bootstrap" >/dev/null`)
	_, took, err = e.job(ctx, "spawn")
	e.check("repeat spawn exits 0", err == nil, err)
	e.check(fmt.Sprintf("repeat spawn is fast (%s)", took), took < time.Minute)
	// The stand-in is a sleep; the fresh bootstrap the spawn started is amp itself.
	e.checkBox("repeat spawn killed the stale bootstrap", "! pgrep -u user -x sleep >/dev/null")
	out, _, err = e.job(ctx, "spawn", "AMP_URL=http://127.0.0.1:9")
	e.check("spawn with AMP_URL exits 0", err == nil, err)
	e.check("bootstrap used AMP_URL", strings.Contains(e.bootstrapOutput(out), "Cannot reach Amp"))

	// Pause and resume keep disk and memory.
	e.box("echo kept >/home/user/marker")
	e.box(`setsid bash -c 'exec -a memtest-sleeper sleep 9999' </dev/null >/dev/null 2>&1 &`)
	_, took, err = e.job(ctx, "pause")
	e.check("pause exits 0", err == nil, err)
	e.check(fmt.Sprintf("sandbox is paused (%s)", took), e.status() == api.SandboxStatusPaused)
	_, _, err = e.job(ctx, "pause")
	e.check("repeat pause exits 0", err == nil, err)
	_, took, err = e.job(ctx, "spawn")
	e.check("spawn resumes paused sandbox", err == nil, err)
	e.check(fmt.Sprintf("resume under 5 min (%s)", took), took < 5*time.Minute)
	e.checkStatus("sandbox is running again", api.SandboxStatusRunning)
	e.checkBox("disk survived pause", `[ "$(cat /home/user/marker)" = kept ]`)
	e.checkBox("memory survived pause (process alive)", "pgrep -f ^memtest-sleeper >/dev/null")

	// Races: Amp may send the next job while the previous one is still settling.
	done := e.jobAsync("pause")
	st := e.waitFor(api.SandboxStatusPausing)
	e.check("status mid-pause is pausing", st == api.SandboxStatusPausing, "status: "+st)
	out, _, err = e.job(ctx, "spawn")
	e.check("spawn during pausing exits 0", err == nil, err, out)
	r := <-done
	e.check("the pause it raced exits 0", r.err == nil, r.err, r.out)
	e.checkStatus("sandbox is running after the race", api.SandboxStatusRunning)

	_, _, _ = e.job(ctx, "pause") //nolint:errcheck // setup for the next race
	done = e.jobAsync("spawn")
	st = e.waitFor(api.SandboxStatusResuming)
	e.check("status mid-resume is resuming", st == api.SandboxStatusResuming, "status: "+st)
	out, _, err = e.job(ctx, "pause")
	e.check("pause during resuming exits 0", err == nil, err, out)
	r = <-done
	e.check("the spawn it raced exits 0", r.err == nil, r.err, r.out)
	// The pause came last, so the sandbox must stay paused, not be woken by the spawn's setup.
	e.checkStatus("sandbox is paused after the race", api.SandboxStatusPaused)

	// Delete.
	_, _, err = e.job(ctx, "delete")
	e.check("delete exits 0", err == nil, err)
	st = e.status()
	e.check("sandbox is destroying or destroyed", strings.HasPrefix(st, "destroy"), st)
	_, _, err = e.job(ctx, "delete")
	e.check("repeat delete exits 0", err == nil, err)
}

// job runs one orb job the way Amp's provider does and returns its output.
func (e *ampE2E) job(ctx context.Context, op string, env ...string) (string, time.Duration, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, e.wrapper) // #nosec G204 -- test-owned path
	cmd.Env = append(os.Environ(), "ORB_OPERATION="+op, "ORB_ID="+e.orbID, "ORB_TOKEN=fake-token", "ORB_IMAGE=ignored")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), time.Since(start), err
}

type ampJobResult struct {
	out string
	err error
}

// jobAsync runs a job in the background; its result arrives on the channel.
func (e *ampE2E) jobAsync(op string) <-chan ampJobResult {
	done := make(chan ampJobResult, 1)
	go func() {
		out, _, err := e.job(e.ctx, op)
		done <- ampJobResult{out, err}
	}()
	return done
}

// waitFor polls until the sandbox reaches want, for up to 20 s, and returns
// the last status seen. Races need the sandbox truly mid-transition.
func (e *ampE2E) waitFor(want string) string {
	st := ""
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if st = e.status(); st == want {
			break
		}
	}
	return st
}

func (e *ampE2E) status() string {
	sb, err := e.client.GetSandbox(e.ctx, e.id)
	if err != nil {
		return "lookup failed: " + err.Error()
	}
	return sb.Status
}

// box runs a script in the sandbox as root and returns its exit code.
func (e *ampE2E) box(script string) int {
	resp, err := e.client.ExecSandbox(e.ctx, e.id, api.SandboxExecReq{Cmd: "bash", Args: []string{"-c", script}})
	if err != nil {
		return -1
	}
	return resp.Result.ExitCode
}

func (e *ampE2E) checkStatus(name, want string) {
	e.t.Helper()
	got := e.status()
	e.check(name, got == want, "status: "+got)
}

func (e *ampE2E) checkBox(name, script string) { e.check(name, e.box(script) == 0) }

// bootstrapOutput replays what the bootstrap started by a spawn printed. With a
// fake token it exits within seconds, so the replay ends on its own.
func (e *ampE2E) bootstrapOutput(spawnOutput string) string {
	pid := regexp.MustCompile(`proc_[A-Za-z0-9_-]+`).FindString(spawnOutput)
	if pid == "" {
		return ""
	}
	time.Sleep(5 * time.Second)
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	var b strings.Builder
	_ = e.client.ConnectProcess(ctx, e.id, pid, 0, func(ev api.ProcessOutputEvent) { //nolint:errcheck // partial output is still evidence
		if data, err := base64.StdEncoding.DecodeString(ev.DataBase64); err == nil {
			b.Write(data)
		}
	})
	return b.String()
}

func (e *ampE2E) check(name string, ok bool, detail ...any) {
	e.t.Helper()
	if ok {
		e.t.Logf("ok   %s", name)
		return
	}
	e.t.Errorf("FAIL %s %v", name, detail)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
