package sandbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"

	"github.com/go-resty/resty/v2"
	"github.com/urfave/cli/v2"
)

// Devin Outposts runs Devin's tool calls on machines you supply. Each outpost
// is a queue of sessions; an orchestrator claims a waiting session, gives it a
// machine and starts devin-remote there with the claim's connect token.
// Docs: https://docs.devin.ai/cloud/outposts/orchestration
//
// `createos sandbox setup devin` is that orchestrator. One session is one
// sandbox. Suspend pauses it, since CreateOS pause keeps disk and memory and a
// resumed session expects its files where it left them; terminate deletes it.
// The Devin API token stays on this host. A sandbox only sees the connect
// token of its own session.

const (
	devinDefaultShape  = "s-4vcpu-8gb"
	devinDefaultRootfs = "devbox:1"
	devinDefaultAPI    = "https://api.devin.ai"
	devinPollEvery     = 5 * time.Second
	devinWaitChunk     = 30 * time.Second
	// The session status can lag the remote's exit by a few seconds.
	devinStatusTries = 6
	devinRemoteBin   = "/usr/local/bin/devin-remote"
	devinWorkDir     = "/home/user/work"
)

// A process may only set env keys declared when its sandbox was created.
var devinEnvKeys = []string{
	"DEVIN_OUTPOST_GATEWAY_URL", "DEVIN_OUTPOST_CONNECT_TOKEN",
	"DEVIN_OUTPOST_SESSION_ID", "DEVIN_REMOTE_STATE_DIR",
}

// Both come from Devin's API and reach a shell and a path in the sandbox.
var (
	devinSessionIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	devinSHARE       = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Installs devin-remote and the session's folders. $1 is the binary's git SHA
// (empty for the latest), $2 the session ID. Idempotent, so a resumed box only
// stops the previous remote.
const devinProvision = `set -e
sha="$1" session="$2"
grep -q "$(hostname)" /etc/hosts || echo "127.0.1.1 $(hostname)" >>/etc/hosts
id user >/dev/null 2>&1 || useradd -m -s /bin/bash user
missing=""
for pair in git:git sudo:sudo curl:curl pkill:procps; do
	command -v "${pair%%:*}" >/dev/null || missing="$missing ${pair#*:}"
done
if [ -n "$missing" ]; then
	# A stalled connection is dropped after 10 s and retried, not held for apt's default 120 s.
	APT="-o Acquire::http::Timeout=10 -o Acquire::https::Timeout=10 -o Acquire::Retries=3"
	DEBIAN_FRONTEND=noninteractive apt-get $APT update -qq
	DEBIAN_FRONTEND=noninteractive apt-get $APT install -y -qq --no-install-recommends ca-certificates $missing >/dev/null
fi
# The box serves one session and is deleted with it, which is when Devin's
# docs allow passwordless sudo.
echo "user ALL=(ALL) NOPASSWD: ALL" >/etc/sudoers.d/user
case "$(uname -m)" in
x86_64) arch=x64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "devin-remote has no build for $(uname -m)" >&2 && exit 1 ;;
esac
base=https://static.devin.ai/devin-rs/remote
[ -n "$sha" ] || sha=$(curl -fsSL "$base/latest_linux_$arch")
bin=/usr/local/lib/devin-remote/$sha
if [ ! -x "$bin" ]; then
	mkdir -p /usr/local/lib/devin-remote
	curl -fsSL "$base/devin-remote_${sha}_linux_$arch" -o "$bin.part"
	echo "$(curl -fsSL "$base/devin-remote_${sha}_linux_$arch.sha256" | cut -d' ' -f1)  $bin.part" | sha256sum -c --quiet
	chmod 755 "$bin.part" && mv "$bin.part" "$bin"
fi
ln -sf "$bin" ` + devinRemoteBin + `
# A resumed box still runs the previous remote, connected with an old token.
pkill -f "^` + devinRemoteBin + ` serve" || true
runuser -u user -- mkdir -p ` + devinWorkDir + ` "/home/user/.devin/worker/sessions/$session"
`

func newSetupDevinCommand() *cli.Command {
	return &cli.Command{
		Name:  "devin",
		Usage: "Run Devin Outposts sessions on CreateOS Sandboxes",
		Description: "Serves a Devin outpost so each Devin session gets its own sandbox.\n\n" +
			"   First, create a Linux outpost in Devin (Settings > Environment > Outposts)\n" +
			"   and a v3 API token for a service user with the Outposts write\n" +
			"   permission. Then:\n\n" +
			"     export DEVIN_API_TOKEN=<the token>\n" +
			"     createos sandbox setup devin --outpost <outpost name>\n\n" +
			"   Leave it running, then pick the outpost under Configuration >\n" +
			"   Virtual environment when you start a Devin session.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "outpost",
				Usage:    "Outpost to serve, by name or ID",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "doctor",
				Usage: "Check the prerequisites and report, without serving anything",
			},
			&cli.StringFlag{
				Name:    "shape",
				Usage:   "Sandbox size for each session",
				Value:   devinDefaultShape,
				EnvVars: []string{"CREATEOS_DEVIN_SHAPE"},
			},
			&cli.StringFlag{
				Name:    "rootfs",
				Usage:   "Image or template each session boots from (Debian or Ubuntu based)",
				Value:   devinDefaultRootfs,
				EnvVars: []string{"CREATEOS_DEVIN_ROOTFS"},
			},
			&cli.IntFlag{
				Name:  "max-sessions",
				Usage: "Most sessions to serve at once; keep it within your sandbox quota",
				Value: 2,
			},
			&cli.StringFlag{
				Name:    "api-url",
				Usage:   "Devin API base URL",
				Value:   devinDefaultAPI,
				EnvVars: []string{"DEVIN_API_URL"},
			},
		},
		Action: runDevinSetup,
	}
}

func runDevinSetup(c *cli.Context) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("setup devin runs on macOS and Linux only")
	}
	if c.Int("max-sessions") < 1 {
		return fmt.Errorf("--max-sessions must be at least 1")
	}
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}
	if _, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{Limit: 1}); err != nil {
		return fmt.Errorf("your session is not usable — run 'createos login' again: %w", err)
	}
	fmt.Println("signed in to CreateOS")

	token := os.Getenv("DEVIN_API_TOKEN")
	if token == "" {
		return fmt.Errorf("DEVIN_API_TOKEN is not set\n\n" +
			"  Create a v3 API token for a service user whose role has Outposts write, then:\n" +
			"    export DEVIN_API_TOKEN=<the token>")
	}
	devin := newDevinAPI(c.String("api-url"), token)
	outpost, err := devin.findOutpost(c.Context, c.String("outpost"))
	if err != nil {
		return err
	}
	if p := outpost.Spec.Platform; p != "" && p != "linux" {
		return fmt.Errorf("outpost %s serves %s machines; CreateOS sandboxes are Linux", outpost.Spec.Name, p)
	}
	fmt.Printf("outpost %s found (%s)\n", outpost.Spec.Name, outpost.Metadata.OutpostID)

	acceptor, err := devinAcceptorID()
	if err != nil {
		return err
	}
	fmt.Println("worker identity", acceptor)
	if c.Bool("doctor") {
		return nil
	}

	// Two orchestrators with one identity would both adopt the same sessions.
	lockCtx, cancelLock := context.WithTimeout(c.Context, time.Second)
	unlock, err := ampLock(lockCtx, "devin-orchestrator")
	cancelLock()
	if err != nil {
		return fmt.Errorf("another 'setup devin' is already running on this machine")
	}
	defer unlock()

	// Connect tokens travel in request bodies, which debug logging would print.
	client.Client.SetDebug(false)
	fmt.Printf("\nServing outpost %s. Leave this running.\n"+
		"Each Devin session on it gets its own %s sandbox, %d at a time.\n"+
		"Run it under systemd or launchd to keep it up after a reboot.\n\n",
		outpost.Spec.Name, c.String("shape"), c.Int("max-sessions"))

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	o := &devinOrch{
		devin: devin, client: client, outpost: outpost.Metadata.OutpostID, acceptor: acceptor,
		shape: c.String("shape"), rootfs: c.String("rootfs"), busy: map[string]bool{},
		recordDir: filepath.Join(filepath.Dir(acceptorPath()), "devin-sessions"),
	}
	return o.run(ctx, c.Int("max-sessions"))
}

// devinAcceptorID is this machine's worker identity, kept across restarts so
// it can pick its claimed sessions back up. Devin's docs forbid sharing one
// across machines, so it is random rather than derived from anything copied.
func devinAcceptorID() (string, error) {
	path := acceptorPath()
	if path == "" {
		return "", fmt.Errorf("could not find your home folder")
	}
	if b, err := os.ReadFile(path); err == nil { // #nosec G304 -- fixed path under the user's home
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := "createos-" + hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return id, os.WriteFile(path, []byte(id+"\n"), 0o600)
}

func acceptorPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".createos", "devin-acceptor-id")
}

// devinSandboxName maps a session to its sandbox. Names cap at 22 characters.
func devinSandboxName(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return "dvn-" + hex.EncodeToString(sum[:])[:18]
}

// ----------------------------------------------------------------- Devin API

type devinEntry struct {
	Metadata struct {
		SessionID string `json:"session_id"`
		OutpostID string `json:"outpost_id"`
	} `json:"metadata"`
	Spec struct {
		Kind            string `json:"kind"`
		RemoteBinarySHA string `json:"remote_binary_sha"`
		NetworkPolicy   struct {
			Enabled bool `json:"enabled"`
		} `json:"network_policy"`
	} `json:"spec"`
	Status struct {
		Phase         string `json:"phase"`
		SessionStatus string `json:"session_status"`
		ConnectToken  string `json:"connect_token"`
		GatewayURL    string `json:"gateway_url"`
	} `json:"status"`
}

type devinOutpost struct {
	Metadata struct {
		OutpostID string `json:"outpost_id"`
	} `json:"metadata"`
	Spec struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	} `json:"spec"`
}

type devinPage[T any] struct {
	Items       []T    `json:"items"`
	Cursor      string `json:"cursor"`
	HasNextPage bool   `json:"has_next_page"`
}

var errDevinTaken = errors.New("claimed by another worker")

type devinAPI struct{ r *resty.Client }

func newDevinAPI(base, token string) *devinAPI {
	return &devinAPI{r: resty.New().
		SetBaseURL(strings.TrimRight(base, "/") + "/opbeta/outposts").
		SetAuthToken(token).
		SetTimeout(30 * time.Second)}
}

// do returns the HTTP status alongside any error, so callers can read 404 and 409.
func (d *devinAPI) do(ctx context.Context, method, path string, query url.Values, body, out any) (int, error) {
	// Decode as JSON whatever Content-Type the reply carries.
	req := d.r.R().SetContext(ctx).SetQueryParamsFromValues(query).ForceContentType("application/json")
	if body != nil {
		req.SetBody(body)
	}
	if out != nil {
		req.SetResult(out)
	}
	resp, err := req.Execute(method, path)
	if err != nil {
		return 0, fmt.Errorf("devin %s %s: %w", method, path, err)
	}
	if resp.IsError() {
		return resp.StatusCode(), fmt.Errorf("devin %s %s: %s %s", method, path, resp.Status(), truncateProcessLabel(resp.String(), 300))
	}
	return resp.StatusCode(), nil
}

func devinList[T any](ctx context.Context, d *devinAPI, path string, query url.Values) ([]T, error) {
	var all []T
	q := url.Values{"first": {"200"}}
	for k, v := range query {
		q[k] = v
	}
	for {
		var page devinPage[T]
		if _, err := d.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if !page.HasNextPage || page.Cursor == "" {
			return all, nil
		}
		q.Set("cursor", page.Cursor)
	}
}

func (d *devinAPI) findOutpost(ctx context.Context, ref string) (*devinOutpost, error) {
	outposts, err := devinList[devinOutpost](ctx, d, "", nil)
	if err != nil {
		return nil, fmt.Errorf("list outposts (is DEVIN_API_TOKEN an Outposts token?): %w", err)
	}
	for _, o := range outposts {
		if o.Spec.Name == ref || o.Metadata.OutpostID == ref {
			return &o, nil
		}
	}
	return nil, fmt.Errorf("no outpost named %q; create it in Devin under Settings > Environment > Outposts", ref)
}

// get returns nil, nil once the session has left the queue.
func (d *devinAPI) get(ctx context.Context, sessionID string) (*devinEntry, error) {
	var e devinEntry
	status, err := d.do(ctx, http.MethodGet, "/devins/"+url.PathEscape(sessionID), nil, nil, &e)
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (d *devinAPI) claim(ctx context.Context, sessionID, acceptor string) (*devinEntry, error) {
	var e devinEntry
	status, err := d.do(ctx, http.MethodPost, "/devins/"+url.PathEscape(sessionID)+"/claim", nil,
		map[string]string{"acceptor_id": acceptor}, &e)
	if status == http.StatusConflict {
		return nil, errDevinTaken
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (d *devinAPI) release(ctx context.Context, sessionID, acceptor string) error {
	status, err := d.do(ctx, http.MethodPost, "/devins/"+url.PathEscape(sessionID)+"/release", nil,
		map[string]string{"acceptor_id": acceptor}, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// -------------------------------------------------------------- orchestrator

type devinOrch struct {
	devin             *devinAPI
	client            *api.SandboxClient
	outpost, acceptor string
	shape, rootfs     string
	recordDir         string // one file per sandbox this machine owns, named after it
	mu                sync.Mutex
	busy              map[string]bool
	wg                sync.WaitGroup
}

// run claims pending sessions until ctx ends. On stop, sandboxes and their
// remotes keep running; the next run adopts them through their claims.
//
// shortcut: polls the queue every 5 s instead of the SSE watch; switch to the
// watch if many orchestrators share one outpost.
func (o *devinOrch) run(ctx context.Context, maxSessions int) error {
	mine, err := devinList[devinEntry](ctx, o.devin, "/devins", url.Values{
		"outpost": {o.outpost}, "phase": {"claimed"}, "acceptor_id": {o.acceptor}})
	if err != nil {
		return err
	}
	for _, e := range mine {
		o.start(ctx, e, o.adopt)
	}
	// A session that ended while this was stopped has left the queue, so only
	// the local record still points at its sandbox.
	for _, id := range o.records() {
		var e devinEntry
		e.Metadata.SessionID = id
		o.start(ctx, e, o.adopt)
	}
	for {
		if o.count() < maxSessions {
			pending, err := devinList[devinEntry](ctx, o.devin, "/devins", url.Values{
				"outpost": {o.outpost}, "phase": {"pending"}})
			if err != nil && ctx.Err() == nil {
				devinLog("list the queue: %v", err)
			}
			for _, e := range pending {
				if o.count() >= maxSessions {
					break
				}
				o.claimAndServe(ctx, e.Metadata.SessionID)
			}
		}
		if ampSleep(ctx, devinPollEvery) != nil {
			o.wg.Wait()
			return nil
		}
	}
}

func (o *devinOrch) claimAndServe(ctx context.Context, sessionID string) {
	if o.isBusy(sessionID) {
		return
	}
	if !devinSessionIDRE.MatchString(sessionID) {
		devinLog("skipping session with unexpected ID %q", sessionID)
		return
	}
	e, err := o.devin.claim(ctx, sessionID, o.acceptor)
	if errors.Is(err, errDevinTaken) {
		return // another worker won the race, which is normal
	}
	if err != nil {
		devinLog("claim %s: %v", sessionID, err)
		return
	}
	e.Metadata.SessionID = sessionID
	o.start(ctx, *e, o.serve)
}

// start runs fn for one session in the background, at most once at a time.
func (o *devinOrch) start(ctx context.Context, e devinEntry, fn func(context.Context, devinEntry)) {
	id := e.Metadata.SessionID
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.busy[id] {
		return
	}
	o.busy[id] = true
	o.wg.Go(func() {
		defer func() { o.mu.Lock(); delete(o.busy, id); o.mu.Unlock() }()
		fn(ctx, e)
	})
}

func (o *devinOrch) count() int            { o.mu.Lock(); defer o.mu.Unlock(); return len(o.busy) }
func (o *devinOrch) isBusy(id string) bool { o.mu.Lock(); defer o.mu.Unlock(); return o.busy[id] }

func (o *devinOrch) serve(ctx context.Context, e devinEntry) {
	id := e.Metadata.SessionID
	if e.Spec.NetworkPolicy.Enabled {
		// CreateOS enforces IP and CIDR egress rules but not hostnames, so the
		// session's policy is not applied. See the README.
		devinLog("%s: its network policy is NOT enforced; the sandbox has open egress", id)
	}
	sbID, procID, err := o.launch(ctx, e)
	if err != nil {
		if ctx.Err() == nil {
			devinLog("%s: %v; returning it to the queue", id, err)
			o.release(ctx, id)
		}
		return
	}
	o.record(id, true)
	devinLog("%s: serving in %s", id, devinSandboxName(id))
	o.watch(ctx, id, sbID, procID)
}

// launch readies the session's sandbox and starts devin-remote in it.
func (o *devinOrch) launch(ctx context.Context, e devinEntry) (_, _ string, err error) {
	id := e.Metadata.SessionID
	name := devinSandboxName(id)
	sha := e.Spec.RemoteBinarySHA
	if sha != "" && !devinSHARE.MatchString(sha) {
		return "", "", fmt.Errorf("unexpected remote_binary_sha %q", sha)
	}
	if e.Status.ConnectToken == "" || e.Status.GatewayURL == "" {
		return "", "", fmt.Errorf("the claim did not return a connect token and gateway URL")
	}

	sb, err := ampSettle(ctx, o.client, name)
	if err != nil {
		return "", "", err
	}
	if sb != nil && sb.Status == api.SandboxStatusError {
		devinLog("%s: %s is in an error state; replacing it", id, name)
		if err = o.client.DestroySandbox(ctx, sb.ID); err != nil && !api.IsNotFound(err) {
			return "", "", fmt.Errorf("delete %s: %w", name, err)
		}
		sb = nil
	}
	var sbID string
	fresh := false
	// On failure, a sandbox made for this attempt holds no work yet and is
	// deleted. An older one keeps its work, but a remote that may have started
	// (a lost CreateProcess reply) is stopped before the claim is released.
	defer func() {
		if err == nil || sbID == "" {
			return
		}
		bg := context.WithoutCancel(ctx)
		if fresh {
			_ = o.client.DestroySandbox(bg, sbID) //nolint:errcheck // best effort
			return
		}
		_, _ = o.client.ExecSandbox(bg, sbID, api.SandboxExecReq{ //nolint:errcheck // best effort
			Cmd: "pkill", Args: []string{"-f", "^" + devinRemoteBin + " serve"}})
	}()
	switch {
	case sb == nil:
		if e.Spec.Kind == "resume" {
			devinLog("%s: no sandbox left to resume; starting an empty one", id)
		}
		envs := map[string]string{}
		for _, k := range devinEnvKeys {
			envs[k] = ""
		}
		var created *api.SandboxCreateResp
		if created, err = o.client.CreateSandbox(ctx, api.SandboxCreateReq{
			Name: name, Shape: o.shape, Rootfs: o.rootfs, Envs: envs,
		}); err != nil {
			return "", "", fmt.Errorf("create %s: %w", name, err)
		}
		sbID, fresh = created.ID, true
	case sb.Status == api.SandboxStatusPaused:
		sbID = sb.ID
		if _, err = o.client.ResumeSandbox(ctx, sbID); err != nil {
			return "", "", fmt.Errorf("resume %s: %w", name, err)
		}
	case sb.Status == api.SandboxStatusRunning:
		sbID = sb.ID
	default:
		return "", "", fmt.Errorf("sandbox %s is %s; refusing to guess", name, sb.Status)
	}
	if err = orcaWaitRunning(ctx, o.client, sbID, ampSettleWait); err != nil {
		return "", "", err
	}

	var out strings.Builder
	exit, err := o.client.ExecSandboxStream(ctx, sbID,
		api.SandboxExecReq{Cmd: "bash", Args: []string{"-c", devinProvision, "provision", sha, id}},
		func(ev api.SandboxExecStreamEvent) { out.WriteString(ev.Stdout + ev.Stderr + ev.Error) })
	if err == nil && exit != 0 {
		err = fmt.Errorf("exited with %d: %s", exit, truncateProcessLabel(out.String(), 2000))
	}
	if err != nil {
		return "", "", fmt.Errorf("set up %s: %w", name, err)
	}

	// sudo resets the environment, so the remote gets only these keys plus
	// PATH, HOME, USER and LOGNAME, as Devin's spawn contract asks.
	proc, err := o.client.CreateProcess(ctx, sbID, api.ProcessCreateRequest{
		Cmd:  "sudo",
		Args: []string{"-u", "user", "-H", "--preserve-env=" + strings.Join(devinEnvKeys, ","), devinRemoteBin, "serve"},
		Cwd:  devinWorkDir,
		Env: map[string]string{
			"DEVIN_OUTPOST_GATEWAY_URL":   e.Status.GatewayURL,
			"DEVIN_OUTPOST_CONNECT_TOKEN": e.Status.ConnectToken,
			"DEVIN_OUTPOST_SESSION_ID":    id,
			"DEVIN_REMOTE_STATE_DIR":      "/home/user/.devin/worker/sessions/" + id,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("start devin-remote in %s: %w", name, err)
	}
	return sbID, proc.ProcessID, nil
}

// watch waits for the remote to exit. Devin tells the remote when the session
// ends; as a fallback it is killed once the queue says the session is over.
func (o *devinOrch) watch(ctx context.Context, id, sbID, procID string) {
	for {
		p, err := o.client.WaitProcess(ctx, sbID, procID, false, devinWaitChunk.Milliseconds())
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if p.ExitCode != nil {
				devinLog("%s: devin-remote exited with %d", id, *p.ExitCode)
			}
			break
		}
		if api.IsNotFound(err) {
			break
		}
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusRequestTimeout {
			devinLog("%s: wait for devin-remote: %v", id, err)
			if ampSleep(ctx, devinPollEvery) != nil {
				return
			}
			continue
		}
		if e, err := o.devin.get(ctx, id); err == nil && (e == nil || e.Status.SessionStatus == "terminated") {
			_, _ = o.client.TerminateProcess(ctx, sbID, procID, 5000) //nolint:errcheck // the box is deleted next
			break
		}
	}
	o.finish(ctx, id, sbID)
}

// finish acts on how the session ended, then hands the claim back.
func (o *devinOrch) finish(ctx context.Context, id, sbID string) {
	status := ""
	for range devinStatusTries {
		e, err := o.devin.get(ctx, id)
		if err == nil && e == nil {
			status = "terminated"
			break
		}
		if e != nil {
			if status = e.Status.SessionStatus; status == "suspended" || status == "terminated" {
				break
			}
		}
		if ampSleep(ctx, 3*time.Second) != nil {
			return
		}
	}
	name := devinSandboxName(id)
	switch status {
	case "suspended":
		if o.retry(ctx, func() error { return o.pause(ctx, sbID) }) {
			devinLog("%s: suspended; sandbox paused", id)
		} else {
			devinLog("%s: suspended, but %s could not be paused; pause it with 'createos sandbox pause %s'", id, name, name)
		}
	case "terminated":
		if o.retry(ctx, func() error {
			if err := o.client.DestroySandbox(ctx, sbID); err != nil && !api.IsNotFound(err) {
				return err
			}
			return nil
		}) {
			o.record(id, false)
			devinLog("%s: ended; sandbox deleted", id)
		} else {
			devinLog("%s: ended, but %s could not be deleted; delete it with 'createos sandbox rm %s'", id, name, name)
		}
	default:
		// The next claim finds the sandbox still running and starts a new remote.
		devinLog("%s: devin-remote stopped while the session is %q; returning it to the queue", id, status)
	}
	o.release(ctx, id)
}

// adopt picks up a session this machine claimed before a restart.
func (o *devinOrch) adopt(ctx context.Context, e devinEntry) {
	id := e.Metadata.SessionID
	sb, err := ampSettle(ctx, o.client, devinSandboxName(id))
	if err != nil {
		devinLog("%s: %v", id, err)
		return
	}
	if sb == nil {
		o.record(id, false)
		o.release(ctx, id)
		return
	}
	if sb.Status == api.SandboxStatusRunning {
		procs, err := o.client.ListProcesses(ctx, sb.ID)
		if err != nil {
			devinLog("%s: %v", id, err)
			return
		}
		for _, p := range procs {
			if !p.LeaderExited && slices.Contains(p.Args, devinRemoteBin) {
				devinLog("%s: picked up again in %s", id, devinSandboxName(id))
				o.watch(ctx, id, sb.ID, p.ProcessID)
				return
			}
		}
	}
	o.finish(ctx, id, sb.ID)
}

func (o *devinOrch) pause(ctx context.Context, sbID string) error {
	sb, err := o.client.GetSandbox(ctx, sbID)
	if err != nil || sb.Status == api.SandboxStatusPaused {
		return err
	}
	if sb.Status == api.SandboxStatusRunning {
		if _, err = o.client.PauseSandbox(ctx, sbID); err != nil {
			return err
		}
	}
	return ampWaitStatus(ctx, o.client, sbID, api.SandboxStatusPaused, ampSettleWait)
}

// retry runs fn up to three times, so one API hiccup does not leave an idle
// sandbox billing. It reports whether fn succeeded.
func (o *devinOrch) retry(ctx context.Context, fn func() error) bool {
	for try := 1; ; try++ {
		err := fn()
		if err == nil {
			return true
		}
		devinLog("attempt %d: %v", try, err)
		if try == 3 || ampSleep(ctx, devinPollEvery) != nil {
			return false
		}
	}
}

// record marks (or, with keep false, unmarks) a session's sandbox as owned by
// this machine. A failed write only costs restart recovery, so it is logged.
func (o *devinOrch) record(id string, keep bool) {
	path := filepath.Join(o.recordDir, devinSandboxName(id))
	var err error
	if keep {
		if err = os.MkdirAll(o.recordDir, 0o700); err == nil {
			err = os.WriteFile(path, []byte(id+"\n"), 0o600)
		}
	} else if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		devinLog("%s: %v", id, err)
	}
}

// records returns the session IDs of every sandbox this machine owns.
func (o *devinOrch) records() []string {
	files, _ := os.ReadDir(o.recordDir) //nolint:errcheck // no folder means no records
	var ids []string
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(o.recordDir, f.Name())) // #nosec G304 -- our own folder
		if id := strings.TrimSpace(string(b)); err == nil && devinSessionIDRE.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (o *devinOrch) release(ctx context.Context, id string) {
	if err := o.devin.release(ctx, id, o.acceptor); err != nil {
		devinLog("%s: release the claim: %v", id, err)
	}
}

func devinLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05")+" "+format+"\n", args...)
}
