package api

import (
	"context"
	"time"
)

// DirectoryCheckpoint is file history stored inside a sandbox.
type DirectoryCheckpoint struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      *string   `json:"name,omitempty"`
	ParentID  *string   `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CheckpointCreateReq selects a directory and optional display label.
type CheckpointCreateReq struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// CheckpointRestoreResp identifies the restored directory.
type CheckpointRestoreResp struct {
	CheckpointID string `json:"checkpoint_id"`
	Path         string `json:"path"`
}

// CreateCheckpoint calls the directory checkpoint API.
func (c *SandboxClient) CreateCheckpoint(ctx context.Context, id string, body CheckpointCreateReq) (*DirectoryCheckpoint, error) {
	var envelope Response[DirectoryCheckpoint]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).SetResult(&envelope).SetBody(body).Post("/v1/sandboxes/{id}/checkpoints")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListCheckpoints calls the directory checkpoint API.
func (c *SandboxClient) ListCheckpoints(ctx context.Context, id string) ([]DirectoryCheckpoint, error) {
	var envelope Response[[]DirectoryCheckpoint]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).SetResult(&envelope).Get("/v1/sandboxes/{id}/checkpoints")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data, nil
}

// RestoreCheckpoint calls the directory checkpoint API.
func (c *SandboxClient) RestoreCheckpoint(ctx context.Context, id string, checkpointID string) (*CheckpointRestoreResp, error) {
	var envelope Response[CheckpointRestoreResp]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).SetResult(&envelope).SetPathParam("checkpoint", checkpointID).Post("/v1/sandboxes/{id}/checkpoints/{checkpoint}/restore")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteCheckpoint removes a record while retaining layers still in use.
func (c *SandboxClient) DeleteCheckpoint(ctx context.Context, id, checkpointID string) error {
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).SetPathParam("checkpoint", checkpointID).Delete("/v1/sandboxes/{id}/checkpoints/{checkpoint}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
