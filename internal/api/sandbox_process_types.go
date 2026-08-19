package api

import "time"

// ProcessPTYOptions requests a persistent PTY instead of separate pipes.
type ProcessPTYOptions struct {
	Rows int `json:"rows,omitempty"`
	Cols int `json:"cols,omitempty"`
}

// ProcessCreateRequest is the body of POST /v1/sandboxes/:id/processes.
// Omit PTY for a pipe process; include it for a managed PTY session.
type ProcessCreateRequest struct {
	Cmd  string             `json:"cmd,omitempty"`
	Args []string           `json:"args,omitempty"`
	Cwd  string             `json:"cwd,omitempty"`
	Env  map[string]string  `json:"env,omitempty"`
	PTY  *ProcessPTYOptions `json:"pty,omitempty"`
}

// ProcessOutputSummary describes retained output for a managed process.
type ProcessOutputSummary struct {
	OldestSeq int64 `json:"oldest_seq"`
	NewestSeq int64 `json:"newest_seq"`
	Bytes     int64 `json:"bytes"`
}

// ProcessForeground describes the current foreground command for a PTY.
type ProcessForeground struct {
	PID int    `json:"pid"`
	Cmd string `json:"cmd"`
}

// ProcessDetails is returned by create/list/get/wait/terminate.
type ProcessDetails struct {
	ProcessID    string               `json:"process_id"`
	Kind         string               `json:"kind"`
	Cmd          string               `json:"cmd,omitempty"`
	Args         []string             `json:"args,omitempty"`
	Cwd          string               `json:"cwd,omitempty"`
	Foreground   *ProcessForeground   `json:"foreground,omitempty"`
	PID          int                  `json:"pid"`
	State        string               `json:"state"`
	LeaderExited bool                 `json:"leader_exited"`
	TreeExited   bool                 `json:"tree_exited"`
	CreatedAt    time.Time            `json:"created_at"`
	FinishedAt   *time.Time           `json:"finished_at,omitempty"`
	ExitCode     *int                 `json:"exit_code,omitempty"`
	Signal       string               `json:"signal,omitempty"`
	Output       ProcessOutputSummary `json:"output"`
}

// ProcessListResponse is the body of GET /v1/sandboxes/:id/processes.
type ProcessListResponse struct {
	Processes []ProcessDetails `json:"processes"`
}

// ProcessInputRequest writes bytes to stdin or a PTY.
type ProcessInputRequest struct {
	DataBase64 string `json:"data_base64"`
}

// ProcessInputResponse confirms ordered input delivery.
type ProcessInputResponse struct {
	InputSeq int64 `json:"input_seq"`
}

// ProcessResizeRequest resizes a managed PTY.
type ProcessResizeRequest struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

// ProcessSignalRequest sends a signal to a process group / PTY foreground group.
type ProcessSignalRequest struct {
	Signal string `json:"signal"`
}

// ProcessOutputEvent is one NDJSON event from /connect.
type ProcessOutputEvent struct {
	Type               string `json:"type"`
	Seq                int64  `json:"seq,omitempty"`
	Stream             string `json:"stream,omitempty"`
	DataBase64         string `json:"data_base64,omitempty"`
	ExitCode           *int   `json:"exit_code,omitempty"`
	Signal             string `json:"signal,omitempty"`
	Error              string `json:"error,omitempty"`
	OldestAvailableSeq int64  `json:"oldest_available_seq,omitempty"`
}
