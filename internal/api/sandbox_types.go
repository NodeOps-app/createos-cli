package api

import "time"

// ── Sandbox wire types ────────────────────────────────────────────
//
// Mirrors fc-spawn's user-facing API. Field names match the JSON the
// server emits so we round-trip cleanly through Resty's SetResult.
// Pointer types are used for fields that may be null (optional name,
// optional ssh keys, etc.).

// SandboxCreateReq is the body of POST /v1/sandboxes.
// `host_id` is deliberately absent — pinning was removed from the API.
type SandboxCreateReq struct {
	Name           string                 `json:"name,omitempty"`
	Shape          string                 `json:"shape"`
	Rootfs         string                 `json:"rootfs,omitempty"`
	DiskMib        int64                  `json:"disk_mib,omitempty"`
	SSHPubkeys     []string               `json:"ssh_pubkeys,omitempty"`
	Egress         []string               `json:"egress,omitempty"`
	Envs           map[string]string      `json:"envs,omitempty"`
	IngressEnabled bool                   `json:"ingress_enabled,omitempty"`
	Networks       []SandboxNetworkAttach `json:"networks,omitempty"`
	Disks          []SandboxDiskAttach    `json:"disks,omitempty"`
}

// SandboxNetworkAttach binds a sandbox to a private network at create time.
type SandboxNetworkAttach struct {
	ID string `json:"id"`
}

// SandboxDiskAttach mounts an S3 disk at create time.
type SandboxDiskAttach struct {
	DiskID    string `json:"disk_id"`
	MountPath string `json:"mount_path"`
}

// SandboxExecReq is the body of POST /v1/sandboxes/:id/exec.
type SandboxExecReq struct {
	Cmd    string            `json:"cmd"`
	Args   []string          `json:"args,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Stdin  string            `json:"stdin,omitempty"`
	Stream bool              `json:"stream,omitempty"`
}

// SandboxExecResult is the inner ExecResponse the agent returns.
type SandboxExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// SandboxExecResp is the buffered (non-streaming) response shape.
type SandboxExecResp struct {
	Result SandboxExecResult `json:"result"`
	ExecMs float64           `json:"exec_ms,omitempty"`
}

// SandboxExecStreamEvent is one NDJSON frame from the streaming endpoint.
// Exactly one of (Stdout, Stderr, ExitCode, Error) is meaningful per
// event; HB heartbeats arrive every ~5 s on silent commands.
type SandboxExecStreamEvent struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
	HB       bool   `json:"hb,omitempty"`
}

// SandboxForkReq is the body of POST /v1/sandboxes/:src/fork. All
// fields are optional overrides; omit any to inherit from the source
// sandbox's snapshot.
type SandboxForkReq struct {
	SSHPubkeys     []string          `json:"ssh_pubkeys,omitempty"`
	Egress         []string          `json:"egress,omitempty"`
	IngressEnabled *bool             `json:"ingress_enabled,omitempty"`
	Envs           map[string]string `json:"envs,omitempty"`
	// StartPaused = true leaves the fork in the `paused` state; otherwise
	// the server auto-resumes it after fork.
	StartPaused bool `json:"start_paused,omitempty"`
}

// SandboxCreateResp is the response body for POST /v1/sandboxes.
// `mode` is deliberately absent — it's an internal boot-path detail.
type SandboxCreateResp struct {
	ID                  string   `json:"id"`
	Name                *string  `json:"name,omitempty"`
	IP                  string   `json:"ip"`
	Shape               string   `json:"shape"`
	Rootfs              *string  `json:"rootfs,omitempty"`
	VCPU                int      `json:"vcpu"`
	MemMib              int      `json:"mem_mib"`
	DiskMib             int64    `json:"disk_mib"`
	SpawnMs             float64  `json:"spawn_ms,omitempty"`
	Egress              []string `json:"egress,omitempty"`
	BandwidthQuotaBytes int64    `json:"bandwidth_quota_bytes,omitempty"`
	IngressURLTemplate  string   `json:"ingress_url_template,omitempty"`
}

// SandboxView is the projection returned by GET /v1/sandboxes and
// GET /v1/sandboxes/:id. The shape matches fc-spawn's SandboxView.
type SandboxView struct {
	ID                    string     `json:"id"`
	Name                  *string    `json:"name,omitempty"`
	Status                string     `json:"status"`
	IP                    *string    `json:"ip,omitempty"`
	VCPU                  int        `json:"vcpu"`
	MemMib                int        `json:"mem_mib"`
	DiskMib               int64      `json:"disk_mib"`
	CreatedAt             time.Time  `json:"created_at"`
	RunningAt             *time.Time `json:"running_at,omitempty"`
	DestroyedAt           *time.Time `json:"destroyed_at,omitempty"`
	SpawnMs               float64    `json:"spawn_ms,omitempty"`
	Shape                 string     `json:"shape,omitempty"`
	Rootfs                *string    `json:"rootfs,omitempty"`
	Egress                []string   `json:"egress,omitempty"`
	SSHPubkeys            []string   `json:"ssh_pubkeys,omitempty"`
	CreatedBy             string     `json:"created_by,omitempty"`
	Region                string     `json:"region"`
	IngressEnabled        bool       `json:"ingress_enabled"`
	IngressURLTemplate    string     `json:"ingress_url_template,omitempty"`
	BandwidthIngressBytes int64      `json:"bandwidth_ingress_bytes,omitempty"`
	Envs                  []string   `json:"envs,omitempty"`
	PausedAt              *time.Time `json:"paused_at,omitempty"`
	LastResumedAt         *time.Time `json:"last_resumed_at,omitempty"`
	ForkedFrom            *string    `json:"forked_from,omitempty"`
}

// ── List shape ────────────────────────────────────────────────────
//
// fc-spawn paginated lists wrap items under data.data[] with a
// pagination block. Matches the createos PaginatedResponse[T] shape.

// SandboxList is the inner shape under data for GET /v1/sandboxes.
type SandboxList struct {
	Data       []SandboxView `json:"data"`
	Pagination Pagination    `json:"pagination"`
}

// ── Catalog ───────────────────────────────────────────────────────

// Shape describes one row of GET /v1/shapes.
type Shape struct {
	ID             string `json:"id"`
	VCPU           int    `json:"vcpu"`
	MemMib         int    `json:"mem_mib"`
	DefaultDiskMib int64  `json:"default_disk_mib"`
}

// ShapeList is the inner shape under data for GET /v1/shapes.
type ShapeList struct {
	Data       []Shape    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// RootfsEntry describes one row of GET /v1/rootfs.
type RootfsEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
	Successor   string `json:"successor,omitempty"`
}

// RootfsCatalog is the response shape of GET /v1/rootfs. The endpoint
// is single-item (not paginated) because it carries both the list and
// the default in one object.
type RootfsCatalog struct {
	Rootfs  []string      `json:"rootfs"`
	Default string        `json:"default"`
	Entries []RootfsEntry `json:"entries"`
}

// ── Disks ─────────────────────────────────────────────────────────

// DiskCreateReq is the body of POST /v1/disks.
type DiskCreateReq struct {
	Name        string           `json:"name"`
	Kind        string           `json:"kind"` // "s3" today
	Config      DiskConfig       `json:"config"`
	Credentials DiskCredentials  `json:"credentials"`
}

// DiskConfig is the non-secret S3 endpoint description.
type DiskConfig struct {
	Bucket       string `json:"bucket"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region,omitempty"`
	UsePathStyle bool   `json:"use_path_style,omitempty"`
}

// DiskCredentials is the bucket creds. AES-encrypted at rest by the
// control plane; never returned in any GET response.
type DiskCredentials struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// DiskView is the user-facing projection returned by all read endpoints.
type DiskView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Config    DiskConfig `json:"config"`
	CreatedAt time.Time `json:"created_at"`
}

// DiskList is the paginated list shape.
type DiskList struct {
	Data       []DiskView `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// SandboxDiskView is one attachment of a disk to a running sandbox,
// shape of GET /v1/sandboxes/:id/disks rows.
type SandboxDiskView struct {
	DiskID      string `json:"disk_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Config      DiskConfig `json:"config"`
	MountPath   string `json:"mount_path"`
	SubPath     string `json:"sub_path,omitempty"`
	MountStatus string `json:"mount_status"`
	MountError  string `json:"mount_error,omitempty"`
}

// SandboxDiskList is the paginated list shape under data.
type SandboxDiskList struct {
	Data       []SandboxDiskView `json:"data"`
	Pagination Pagination        `json:"pagination"`
}

// DiskAttachReq is the body of POST /v1/sandboxes/:id/disks.
type DiskAttachReq struct {
	DiskID    string `json:"disk_id"`
	MountPath string `json:"mount_path"`
	SubPath   string `json:"sub_path,omitempty"`
}

// SandboxNetwork describes one row of GET /v1/networks. Members is
// populated only by the per-id GET, not by the list endpoint.
type SandboxNetwork struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	CreatedAt   time.Time              `json:"created_at"`
	MemberCount int                    `json:"member_count,omitempty"`
	Members     []SandboxNetworkMember `json:"members,omitempty"`
}

// SandboxNetworkMember is one sandbox attached to a network.
type SandboxNetworkMember struct {
	SandboxID string `json:"sandbox_id"`
	Status    string `json:"status"`
	IP        string `json:"ip,omitempty"`
	Name      string `json:"name,omitempty"`
}

// NetworkList is the inner shape under data for GET /v1/networks.
type NetworkList struct {
	Data       []SandboxNetwork `json:"data"`
	Pagination Pagination       `json:"pagination"`
}

// BandwidthView is the response of GET /v1/sandboxes/:id/bandwidth.
// `Capped` flips true once usage hits the quota; while capped, the
// sandbox's egress is throttled hard at the host. `RemainingBytes`
// can read negative when used > quota due to in-flight accounting.
type BandwidthView struct {
	ID             string `json:"id"`
	QuotaBytes     int64  `json:"quota_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	IngressBytes   int64  `json:"ingress_bytes"`
	RemainingBytes int64  `json:"remaining_bytes"`
	Capped         bool   `json:"capped"`
}

// BandwidthRechargeReq is the body of POST /v1/sandboxes/:id/bandwidth/recharge.
type BandwidthRechargeReq struct {
	AddBytes int64 `json:"add_bytes"`
}

// EgressView is the response of GET /v1/sandboxes/:id/egress.
type EgressView struct {
	Egress []string `json:"egress"`
}

// EgressSetReq is the body of PUT /v1/sandboxes/:id/egress.
type EgressSetReq struct {
	Egress []string `json:"egress"`
}

// ── Templates ─────────────────────────────────────────────────────

// TemplateView projects a templates row to the user-facing API.
// Mirrors fc-spawn's types.TemplateView.
type TemplateView struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Base          string     `json:"base"`
	Status        string     `json:"status"`
	Ext4SizeBytes int64      `json:"ext4_size_bytes"`
	CreatedAt     time.Time  `json:"created_at"`
	BuiltAt       *time.Time `json:"built_at,omitempty"`
	// Only populated by GET /v1/templates/:id?include=dockerfile.
	Dockerfile string `json:"dockerfile,omitempty"`
}

// TemplateList is the inner shape under data for GET /v1/templates.
type TemplateList struct {
	Data       []TemplateView `json:"data"`
	Pagination Pagination     `json:"pagination"`
}

// TemplateCreateReq is the body of POST /v1/templates.
type TemplateCreateReq struct {
	Name       string `json:"name"`
	Dockerfile string `json:"dockerfile"`
	Base       string `json:"base,omitempty"`
}

// TemplateLogEvent is one NDJSON frame from
// GET /v1/templates/:id/logs?follow=true. Either `Line` carries a
// build output line, or `Final` is true with `Status` set to
// ready/failed/cancelled.
type TemplateLogEvent struct {
	Line   string `json:"line,omitempty"`
	Final  bool   `json:"final,omitempty"`
	Status string `json:"status,omitempty"`
}
