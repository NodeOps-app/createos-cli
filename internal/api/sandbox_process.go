package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// CreateProcess creates a managed pipe process or PTY in a sandbox.
func (c *SandboxClient) CreateProcess(ctx context.Context, sandboxID string, req ProcessCreateRequest) (*ProcessDetails, error) {
	var envelope Response[ProcessDetails]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/processes")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ListProcesses lists managed processes and PTYs in a sandbox.
func (c *SandboxClient) ListProcesses(ctx context.Context, sandboxID string) ([]ProcessDetails, error) {
	var envelope Response[ProcessListResponse]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/processes")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return envelope.Data.Processes, nil
}

// GetProcess inspects one managed process.
func (c *SandboxClient) GetProcess(ctx context.Context, sandboxID, processID string) (*ProcessDetails, error) {
	var envelope Response[ProcessDetails]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/processes/{process_id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ConnectProcess replays and follows ordered output events.
func (c *SandboxClient) ConnectProcess(ctx context.Context, sandboxID, processID string, after int64, onEvent func(ProcessOutputEvent)) error {
	req := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetDoNotParseResponse(true)
	if after > 0 {
		req.SetQueryParam("after", fmt.Sprintf("%d", after))
	}
	resp, err := req.Get("/v1/sandboxes/{id}/processes/{process_id}/connect")
	if err != nil {
		return err
	}
	body := resp.RawBody()
	defer func() { _ = body.Close() }() //nolint:errcheck
	if resp.IsError() {
		raw, readErr := io.ReadAll(body)
		if readErr != nil {
			raw = nil
		}
		return ParseAPIError(resp.StatusCode(), raw)
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev ProcessOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		onEvent(ev)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read process stream: %w", err)
	}
	return nil
}

// WriteProcessInput writes ordered bytes to process stdin or a PTY.
func (c *SandboxClient) WriteProcessInput(ctx context.Context, sandboxID, processID string, req ProcessInputRequest) (*ProcessInputResponse, error) {
	var envelope Response[ProcessInputResponse]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetBody(req).
		SetResult(&envelope).
		Post("/v1/sandboxes/{id}/processes/{process_id}/input")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// CloseProcessStdin closes stdin for a pipe process.
func (c *SandboxClient) CloseProcessStdin(ctx context.Context, sandboxID, processID string) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		Post("/v1/sandboxes/{id}/processes/{process_id}/stdin/close")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ResizeProcessPTY resizes a managed PTY.
func (c *SandboxClient) ResizeProcessPTY(ctx context.Context, sandboxID, processID string, req ProcessResizeRequest) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetBody(req).
		Post("/v1/sandboxes/{id}/processes/{process_id}/resize")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// SignalProcess sends a signal to a process group or PTY foreground group.
func (c *SandboxClient) SignalProcess(ctx context.Context, sandboxID, processID string, req ProcessSignalRequest) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetBody(req).
		Post("/v1/sandboxes/{id}/processes/{process_id}/signal")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// WaitProcess waits for the main command or every process it started.
func (c *SandboxClient) WaitProcess(ctx context.Context, sandboxID, processID string, all bool, timeoutMs int64) (*ProcessDetails, error) {
	var envelope Response[ProcessDetails]
	req := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetResult(&envelope)
	if all {
		req.SetQueryParam("scope", "tree")
	} else {
		req.SetQueryParam("scope", "leader")
	}
	if timeoutMs > 0 {
		req.SetQueryParam("timeout_ms", fmt.Sprintf("%d", timeoutMs))
	}
	resp, err := req.Get("/v1/sandboxes/{id}/processes/{process_id}/wait")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// TerminateProcess terminates the complete managed process tree.
func (c *SandboxClient) TerminateProcess(ctx context.Context, sandboxID, processID string, graceMs int64) (*ProcessDetails, error) {
	var envelope Response[ProcessDetails]
	req := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", sandboxID).
		SetPathParam("process_id", processID).
		SetResult(&envelope)
	if graceMs >= 0 {
		req.SetQueryParam("grace_ms", fmt.Sprintf("%d", graceMs))
	}
	resp, err := req.Delete("/v1/sandboxes/{id}/processes/{process_id}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}
