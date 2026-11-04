package api

import (
	"context"
	"strconv"
	"time"
)

// SnapshotView describes a reusable sandbox snapshot.
type SnapshotView struct {
	ID        string     `json:"id"`
	SandboxID string     `json:"sandbox_id"`
	Name      *string    `json:"name,omitempty"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	Shape     string     `json:"shape"`
	VCPU      int        `json:"vcpu"`
	MemMib    int        `json:"mem_mib"`
	DiskMib   int64      `json:"disk_mib"`
	SizeBytes int64      `json:"size_bytes"`
	ReadyAt   *time.Time `json:"ready_at,omitempty"`
}

// SnapshotCreateReq selects the capture type and optional display name.
type SnapshotCreateReq struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// SnapshotList is a page of snapshots.
type SnapshotList struct {
	Data       []SnapshotView `json:"data"`
	Pagination Pagination     `json:"pagination"`
}

// CreateSnapshot calls the public snapshot API.
func (c *SandboxClient) CreateSnapshot(ctx context.Context, id string, body SnapshotCreateReq) (*SnapshotView, error) {
	var envelope Response[SnapshotView]
	resp, err := c.Client.R().SetContext(ctx).SetResult(&envelope).SetPathParam("id", id).SetBody(body).Post("/v1/sandboxes/{id}/snapshots")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// GetSnapshot calls the public snapshot API.
func (c *SandboxClient) GetSnapshot(ctx context.Context, id string) (*SnapshotView, error) {
	var envelope Response[SnapshotView]
	resp, err := c.Client.R().SetContext(ctx).SetResult(&envelope).SetPathParam("id", id).Get("/v1/snapshots/{id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListSnapshots calls the public snapshot API.
func (c *SandboxClient) ListSnapshots(ctx context.Context, limit, offset int) (*SnapshotList, error) {
	var envelope Response[SnapshotList]
	resp, err := c.Client.R().SetContext(ctx).SetResult(&envelope).SetQueryParam("limit", strconv.Itoa(limit)).SetQueryParam("offset", strconv.Itoa(offset)).Get("/v1/snapshots")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteSnapshot removes a snapshot from the user's catalog.
func (c *SandboxClient) DeleteSnapshot(ctx context.Context, id string) error {
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).Delete("/v1/snapshots/{id}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
