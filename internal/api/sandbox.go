package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/go-resty/resty/v2"
)

// ListSandboxesOpts bounds the query for ListSandboxes.
type ListSandboxesOpts struct {
	// Limit is the page size; 0 = server default (50).
	Limit int
	// Offset is the page offset; 0 = first page.
	Offset int
	// Status, if non-empty, filters server-side BEFORE the limit:
	// "running", "creating", "destroyed", "failed", etc.
	Status string
}

// ListSandboxes returns one page of the caller's sandboxes plus the
// pagination block so callers can compute "more pages?".
func (c *SandboxClient) ListSandboxes(ctx context.Context, opts ListSandboxesOpts) ([]SandboxView, Pagination, error) {
	r := c.Client.R().SetContext(ctx)
	if opts.Limit > 0 {
		r.SetQueryParam("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		r.SetQueryParam("offset", fmt.Sprintf("%d", opts.Offset))
	}
	if opts.Status != "" {
		r.SetQueryParam("status", opts.Status)
	}
	var envelope Response[SandboxList]
	resp, err := r.SetResult(&envelope).Get("/v1/sandboxes")
	if err != nil {
		return nil, Pagination{}, err
	}
	if resp.IsError() {
		return nil, Pagination{}, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, envelope.Data.Pagination, nil
}

// UploadFile streams local data into the sandbox at `remote` (must be
// absolute). The server only inspects the ?path= query, not the body
// shape, so we send raw octets and an explicit Content-Length when we
// know it.
func (c *SandboxClient) UploadFile(ctx context.Context, id, remote string, body io.Reader, size int64) error {
	r := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("path", remote).
		SetHeader("Content-Type", "application/octet-stream").
		SetBody(body)
	if size > 0 {
		r.SetHeader("Content-Length", fmt.Sprintf("%d", size))
	}
	resp, err := r.Put("/v1/sandboxes/{id}/files")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DownloadFile streams the sandbox file at `remote` into dst. Returns
// the number of bytes copied.
func (c *SandboxClient) DownloadFile(ctx context.Context, id, remote string, dst io.Writer) (int64, error) {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("path", remote).
		SetDoNotParseResponse(true).
		Get("/v1/sandboxes/{id}/files")
	if err != nil {
		return 0, err
	}
	body := resp.RawBody()
	defer func() {
		if cerr := body.Close(); cerr != nil {
			_ = cerr
		}
	}()
	if resp.IsError() {
		raw, readErr := io.ReadAll(body)
		if readErr != nil {
			raw = nil
		}
		return 0, ParseAPIError(resp.StatusCode(), raw)
	}
	return io.Copy(dst, body)
}

// ExecSandbox runs a command inside the sandbox and returns the
// buffered result (stdout, stderr, exit code).
func (c *SandboxClient) ExecSandbox(ctx context.Context, id string, req SandboxExecReq) (*SandboxExecResp, error) {
	req.Stream = false
	var envelope Response[SandboxExecResp]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/exec")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ExecSandboxStream runs a command and streams NDJSON events live to
// onEvent. onEvent is invoked per line — the caller decides what to do
// with stdout/stderr chunks. Returns the final exit code (or -1 if the
// stream ended without a terminal exit_code frame).
func (c *SandboxClient) ExecSandboxStream(ctx context.Context, id string, req SandboxExecReq, onEvent func(SandboxExecStreamEvent)) (int, error) {
	req.Stream = true
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("stream", "true").
		SetBody(req).
		SetDoNotParseResponse(true).
		Post("/v1/sandboxes/{id}/exec")
	if err != nil {
		return -1, err
	}
	body := resp.RawBody()
	defer func() {
		if cerr := body.Close(); cerr != nil {
			_ = cerr
		}
	}()

	// Non-2xx bodies are JSend envelopes, not NDJSON — read and parse.
	if resp.IsError() {
		raw, readErr := io.ReadAll(body)
		if readErr != nil {
			raw = nil
		}
		return -1, ParseAPIError(resp.StatusCode(), raw)
	}

	exit := -1
	scanner := bufio.NewScanner(body)
	// Bigger buffer for fat stdout chunks (default is 64 KiB).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev SandboxExecStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Skip malformed lines rather than abort the whole exec —
			// otherwise a server-side log glitch kills user output.
			continue
		}
		if ev.ExitCode != nil {
			exit = *ev.ExitCode
		}
		onEvent(ev)
	}
	if err := scanner.Err(); err != nil {
		return exit, fmt.Errorf("read stream: %w", err)
	}
	return exit, nil
}

// AddSSHPubkeys appends OpenSSH-formatted public keys to a sandbox.
// The server canonicalises and de-duplicates against existing keys.
// Returns the new total count.
func (c *SandboxClient) AddSSHPubkeys(ctx context.Context, id string, keys []string) (int, error) {
	body := map[string]any{"keys": keys}
	var envelope Response[struct {
		Count int `json:"count"`
	}]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetBody(body).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/ssh-pubkeys")
	if err != nil {
		return 0, err
	}
	if resp.IsError() {
		return 0, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Count, nil
}

// SetSandboxIngress flips the public-URL toggle on a running sandbox.
// PATCH /v1/sandboxes/:id with {"ingress_enabled": <bool>}. Returns
// the updated SandboxView (with ingress_url_template when enabled and
// the cluster knows its domain suffix).
func (c *SandboxClient) SetSandboxIngress(ctx context.Context, id string, enabled bool) (*SandboxView, error) {
	return c.patchSandbox(ctx, id, SandboxPatchReq{IngressEnabled: &enabled})
}

// SetAutoPause sets or clears the idle-pause timeout on a sandbox.
// Pass nil to turn auto-pause off; pass a pointer to seconds (60–86400) to enable.
func (c *SandboxClient) SetAutoPause(ctx context.Context, id string, seconds *int) (*SandboxView, error) {
	if seconds == nil {
		return c.patchSandbox(ctx, id, SandboxPatchReq{DisableAutoPause: true})
	}
	return c.patchSandbox(ctx, id, SandboxPatchReq{AutoPauseAfterSeconds: seconds})
}

func (c *SandboxClient) patchSandbox(ctx context.Context, id string, req SandboxPatchReq) (*SandboxView, error) {
	var envelope Response[SandboxView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetBody(req).
		SetResult(&envelope).
		Patch("/v1/sandboxes/{id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ── Disks ─────────────────────────────────────────────────────────

// CreateDisk registers an S3 bucket as a named disk the caller can
// later mount into sandboxes. The server HEAD-probes the bucket
// before persisting, so wrong creds / unreachable endpoints fail
// fast at create time.
func (c *SandboxClient) CreateDisk(ctx context.Context, req DiskCreateReq) (*DiskView, error) {
	var envelope Response[DiskView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/disks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListDisks returns the caller's disks. Paginated server-side; we
// pull one large page to cover anyone but extreme outliers.
func (c *SandboxClient) ListDisks(ctx context.Context) ([]DiskView, error) {
	var envelope Response[DiskList]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetQueryParam("limit", "200").
		SetResult(&envelope).
		Get("/v1/disks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// GetDisk fetches one disk by id OR friendly name.
func (c *SandboxClient) GetDisk(ctx context.Context, ref string) (*DiskView, error) {
	var envelope Response[DiskView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		SetResult(&envelope).
		Get("/v1/disks/{ref}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteDisk soft-deletes a disk by id or name. 409 if attached to a
// non-terminal sandbox — caller must detach (or destroy) first.
func (c *SandboxClient) DeleteDisk(ctx context.Context, ref string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		Delete("/v1/disks/{ref}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListSandboxDisks returns the attachments on one sandbox (with the
// live mount_status reported by the guest agent).
func (c *SandboxClient) ListSandboxDisks(ctx context.Context, sandboxID string) ([]SandboxDiskView, error) {
	var envelope Response[SandboxDiskList]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/disks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// AttachDisk live-attaches a disk to a running sandbox. The agent
// mounts it on its next reconcile tick (~seconds).
func (c *SandboxClient) AttachDisk(ctx context.Context, sandboxID string, req DiskAttachReq) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetBody(req).
		Post("/v1/sandboxes/{id}/disks")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DetachDisk unmounts a disk from a running sandbox. The bucket itself
// is untouched. mountPath is required so a disk mounted at multiple
// paths can detach exactly one.
func (c *SandboxClient) DetachDisk(ctx context.Context, sandboxID, diskRef, mountPath string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("disk", diskRef).
		SetQueryParam("mount_path", mountPath).
		Delete("/v1/sandboxes/{id}/disks/{disk}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// PauseSandbox kicks off the async pause. Returns the row in its
// `pausing` state; callers should poll GetSandbox until status flips
// to `paused` (or `failed`).
func (c *SandboxClient) PauseSandbox(ctx context.Context, id string) (*SandboxView, error) {
	return c.lifecyclePOST(ctx, id, "/v1/sandboxes/{id}/pause")
}

// ResumeSandbox kicks off the async resume. Status flips through
// `resuming` → `running`.
func (c *SandboxClient) ResumeSandbox(ctx context.Context, id string) (*SandboxView, error) {
	return c.lifecyclePOST(ctx, id, "/v1/sandboxes/{id}/resume")
}

// ForkSandbox clones a paused source sandbox into a brand-new id. By
// default the fork auto-resumes; pass StartPaused=true to keep it
// paused. The response is the NEW sandbox's view (in `forking` or
// `paused`/`running` depending on StartPaused + timing).
func (c *SandboxClient) ForkSandbox(ctx context.Context, srcID string, req SandboxForkReq) (*SandboxView, error) {
	var envelope Response[SandboxView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", srcID).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/fork")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// lifecyclePOST is the shared shape of pause/resume — body-less POST
// to /v1/sandboxes/{id}/<action>, returning the updated view.
func (c *SandboxClient) lifecyclePOST(ctx context.Context, id, path string) (*SandboxView, error) {
	var envelope Response[SandboxView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetResult(&envelope).
		Post(path)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DestroySandbox issues DELETE /v1/sandboxes/:id. The server returns
// 200 with {id, status:"destroying"} — actual teardown is async.
// Calling DestroySandbox on a row already destroyed returns 200 too,
// so this is idempotent on the wire.
func (c *SandboxClient) DestroySandbox(ctx context.Context, id string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		Delete("/v1/sandboxes/{id}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// GetSandbox fetches one sandbox by id. Returns a wrapped *APIError on
// non-2xx (404 when the id is wrong or the sandbox belongs to someone
// else — the server returns the same shape both ways).
func (c *SandboxClient) GetSandbox(ctx context.Context, id string) (*SandboxView, error) {
	var envelope Response[SandboxView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListRootfs returns the rootfs catalog (base images + templates the
// cluster has cached). Single-item endpoint — the response carries
// both the list and the cluster default name.
func (c *SandboxClient) ListRootfs(ctx context.Context) (*RootfsCatalog, error) {
	var envelope Response[RootfsCatalog]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetResult(&envelope).
		Get("/v1/rootfs")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// CreateNetwork registers a new private network. Returns the row
// (with empty Members on a fresh network).
func (c *SandboxClient) CreateNetwork(ctx context.Context, name string) (*SandboxNetwork, error) {
	var envelope Response[SandboxNetwork]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetBody(map[string]string{"name": name}).
		SetResult(&envelope).
		Post("/v1/networks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// GetNetwork fetches one network by id OR friendly name. Includes the
// Members list so the caller can show what's attached.
func (c *SandboxClient) GetNetwork(ctx context.Context, ref string) (*SandboxNetwork, error) {
	var envelope Response[SandboxNetwork]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		SetResult(&envelope).
		Get("/v1/networks/{ref}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteNetwork soft-deletes a network by id or name.
func (c *SandboxClient) DeleteNetwork(ctx context.Context, ref string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		Delete("/v1/networks/{ref}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// AttachNetwork live-attaches a sandbox to a network. The body matches
// SandboxNetworkAttach so attach-at-create and attach-at-runtime share
// the same shape on the wire.
func (c *SandboxClient) AttachNetwork(ctx context.Context, sandboxID, netRef string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetBody(SandboxNetworkAttach{ID: netRef}).
		Post("/v1/sandboxes/{id}/networks")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DetachNetwork live-detaches a sandbox from a network.
func (c *SandboxClient) DetachNetwork(ctx context.Context, sandboxID, netRef string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("net", netRef).
		Delete("/v1/sandboxes/{id}/networks/{net}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListNetworks returns the caller's private networks. Paginated on the
// wire; we pull one page (200 max) which covers anyone but pathological
// outliers.
func (c *SandboxClient) ListNetworks(ctx context.Context) ([]SandboxNetwork, error) {
	var envelope Response[NetworkList]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetQueryParam("limit", "200").
		SetResult(&envelope).
		Get("/v1/networks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// ListShapes returns the static shape catalog. Server-side this is a
// paginated endpoint; the full catalog fits in one page in practice.
func (c *SandboxClient) ListShapes(ctx context.Context) ([]Shape, error) {
	var envelope Response[ShapeList]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetResult(&envelope).
		Get("/v1/shapes")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// CreateSandbox creates a sandbox via POST /v1/sandboxes.
// Returns the parsed SandboxCreateResp on 2xx; *APIError otherwise.
func (c *SandboxClient) CreateSandbox(ctx context.Context, req SandboxCreateReq) (*SandboxCreateResp, error) {
	var envelope Response[SandboxCreateResp]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/sandboxes")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// GetBandwidth returns the live quota / used / remaining counters.
func (c *SandboxClient) GetBandwidth(ctx context.Context, id string) (*BandwidthView, error) {
	var envelope Response[BandwidthView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/bandwidth")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// RechargeBandwidth adds bytes to the sandbox's quota. Returns the
// updated bandwidth view.
func (c *SandboxClient) RechargeBandwidth(ctx context.Context, id string, addBytes int64) (*BandwidthView, error) {
	var envelope Response[BandwidthView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetBody(BandwidthRechargeReq{AddBytes: addBytes}).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/bandwidth/recharge")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// GetEgress returns the sandbox's outbound allowlist.
func (c *SandboxClient) GetEgress(ctx context.Context, id string) ([]string, error) {
	var envelope Response[EgressView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/egress")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Egress, nil
}

// SetEgress replaces the sandbox's outbound allowlist. Empty slice
// means allow-all.
func (c *SandboxClient) SetEgress(ctx context.Context, id string, rules []string) ([]string, error) {
	if rules == nil {
		rules = []string{}
	}
	var envelope Response[EgressView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetBody(EgressSetReq{Egress: rules}).
		SetResult(&envelope).
		Put("/v1/sandboxes/{id}/egress")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Egress, nil
}

// ── Templates ─────────────────────────────────────────────────────

// CreateTemplate submits a Dockerfile for build. Returns the
// newly-inserted (pending) template row. Build runs async — caller
// can poll GetTemplate or stream logs to watch progress.
func (c *SandboxClient) CreateTemplate(ctx context.Context, req TemplateCreateReq) (*TemplateView, error) {
	var envelope Response[TemplateView]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/templates")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListTemplates returns the caller's templates. Paginated server-side;
// we pull one max-page which covers anyone but pathological outliers.
func (c *SandboxClient) ListTemplates(ctx context.Context) ([]TemplateView, error) {
	var envelope Response[TemplateList]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetQueryParam("limit", "200").
		SetResult(&envelope).
		Get("/v1/templates")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// GetTemplate fetches one template by id OR friendly name. When
// withDockerfile is true the response includes the source.
func (c *SandboxClient) GetTemplate(ctx context.Context, ref string, withDockerfile bool) (*TemplateView, error) {
	var envelope Response[TemplateView]
	req := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		SetResult(&envelope)
	if withDockerfile {
		req = req.SetQueryParam("include", "dockerfile")
	}
	resp, err := req.Get("/v1/templates/{ref}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteTemplate soft-deletes a template. Paused sandboxes that were
// built from it can still be resumed; new sandbox creates will 404.
func (c *SandboxClient) DeleteTemplate(ctx context.Context, ref string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		Delete("/v1/templates/{ref}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// StreamTemplateLogs opens the NDJSON log stream and returns the raw
// HTTP response — the caller is responsible for scanning lines and
// closing the body. Honours ctx cancellation through resty.
func (c *SandboxClient) StreamTemplateLogs(ctx context.Context, ref string, follow bool) (*resty.Response, error) {
	req := c.Client.R().
		SetContext(ctx).
		SetPathParam("ref", ref).
		SetDoNotParseResponse(true)
	if follow {
		req = req.SetQueryParam("follow", "true")
	}
	resp, err := req.Get("/v1/templates/{ref}/logs")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		body := resp.RawBody()
		defer func() {
			if cerr := body.Close(); cerr != nil {
				_ = cerr
			}
		}()
		return nil, ParseAPIError(resp.StatusCode(), nil)
	}
	return resp, nil
}
