package api

import (
	"context"
	"time"
)

// DeviceView is the wire shape returned by the device endpoints.
type DeviceView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ClientIP   string     `json:"client_ip"`
	Pubkey     string     `json:"pubkey"`
	Hostname   string     `json:"hostname,omitempty"`
	OS         string     `json:"os,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// DeviceCreateReq is the body for POST /v1/devices.
type DeviceCreateReq struct {
	Name     string `json:"name"`
	Pubkey   string `json:"pubkey"` // base64 32-byte Curve25519
	Hostname string `json:"hostname,omitempty"`
	OS       string `json:"os,omitempty"`
}

// DeviceNetworkAttachmentView is one row of GET /v1/devices/:id/networks.
type DeviceNetworkAttachmentView struct {
	NetworkID   string    `json:"network_id"`
	NetworkName string    `json:"network_name"`
	AttachedAt  time.Time `json:"attached_at"`
}

// DeviceSessionView is the response from POST /v1/devices/:id/sessions.
// ClientConfig is the full wg-quick .conf body the caller writes to disk
// (after prepending its locally-held PrivateKey).
type DeviceSessionView struct {
	SessionID    string    `json:"session_id"`
	DeviceID     string    `json:"device_id"`
	RelayHostID  string    `json:"relay_host_id"`
	ClientConfig string    `json:"client_config"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// CreateDevice registers a machine. Pubkey is required; the CLI keeps
// the matching private key on disk (never shipped to the server).
func (c *SandboxClient) CreateDevice(ctx context.Context, req DeviceCreateReq) (*DeviceView, error) {
	var envelope Response[DeviceView]
	resp, err := c.Client.R().SetContext(ctx).SetBody(req).SetResult(&envelope).Post("/v1/devices")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeviceList is the inner shape under data for GET /v1/devices.
type DeviceList struct {
	Data       []DeviceView `json:"data"`
	Pagination Pagination   `json:"pagination"`
}

// ListDevices returns the caller's registered devices.
func (c *SandboxClient) ListDevices(ctx context.Context) ([]DeviceView, error) {
	var envelope Response[DeviceList]
	resp, err := c.Client.R().SetContext(ctx).SetResult(&envelope).Get("/v1/devices")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Data, nil
}

// GetDevice fetches one device by id.
func (c *SandboxClient) GetDevice(ctx context.Context, id string) (*DeviceView, error) {
	var envelope Response[DeviceView]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).
		SetResult(&envelope).Get("/v1/devices/{id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteDevice revokes a device. All sessions and network attachments
// are removed atomically server-side.
func (c *SandboxClient) DeleteDevice(ctx context.Context, id string) error {
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", id).Delete("/v1/devices/{id}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// AttachDeviceToNetwork persists a device ↔ network link.
func (c *SandboxClient) AttachDeviceToNetwork(ctx context.Context, deviceID, networkRef string) error {
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", deviceID).
		SetBody(map[string]string{"network_id": networkRef}).
		Post("/v1/devices/{id}/networks")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListDeviceNetworks returns the networks a device is attached to.
func (c *SandboxClient) ListDeviceNetworks(ctx context.Context, deviceID string) ([]DeviceNetworkAttachmentView, error) {
	var envelope Response[[]DeviceNetworkAttachmentView]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", deviceID).
		SetResult(&envelope).Get("/v1/devices/{id}/networks")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data, nil
}

// DetachDeviceFromNetwork removes one attachment.
func (c *SandboxClient) DetachDeviceFromNetwork(ctx context.Context, deviceID, networkRef string) error {
	resp, err := c.Client.R().SetContext(ctx).
		SetPathParam("id", deviceID).SetPathParam("nid", networkRef).
		Delete("/v1/devices/{id}/networks/{nid}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// CreateDeviceSession opens a new VPN session for the device and returns
// the wg-quick config the caller should bring up.
func (c *SandboxClient) CreateDeviceSession(ctx context.Context, deviceID string) (*DeviceSessionView, error) {
	var envelope Response[DeviceSessionView]
	resp, err := c.Client.R().SetContext(ctx).SetPathParam("id", deviceID).
		SetResult(&envelope).Post("/v1/devices/{id}/sessions")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// DeleteDeviceSession closes one session.
func (c *SandboxClient) DeleteDeviceSession(ctx context.Context, deviceID, sessionID string) error {
	resp, err := c.Client.R().SetContext(ctx).
		SetPathParam("id", deviceID).SetPathParam("sid", sessionID).
		Delete("/v1/devices/{id}/sessions/{sid}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// RenewDeviceSession bumps the session's expires_at forward by one
// server-side TTL. The CLI's renewal goroutine calls this every ~TTL/2
// while a tunnel is live. A 404 from this endpoint means the session
// has expired or been deleted server-side — the caller's local WG iface
// is now orphan and must be torn down. Use IsNotFound to discriminate.
func (c *SandboxClient) RenewDeviceSession(ctx context.Context, deviceID, sessionID string) error {
	resp, err := c.Client.R().SetContext(ctx).
		SetPathParam("id", deviceID).SetPathParam("sid", sessionID).
		Put("/v1/devices/{id}/sessions/{sid}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
