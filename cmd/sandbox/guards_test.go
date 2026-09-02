package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiskMountBlocksFileAPI covers the guard for issue #71: a file-API
// transfer into an S3 disk mount crashes the mount and loses the object.
// The path comparison has to be on whole segments — "/mnt/data-old" is not
// inside "/mnt/data", and blocking it would stop a legitimate transfer.
func TestDiskMountBlocksFileAPI(t *testing.T) {
	f := newFakeAPI(t).json("GET /v1/sandboxes/sb-1/disks",
		`{"data":{"data":[{"disk_id":"d1","name":"bucket","mount_path":"/mnt/data"}]}}`)

	for _, tc := range []struct {
		remote string
		want   string
	}{
		{"/mnt/data/report.csv", "/mnt/data"},
		{"/mnt/data", "/mnt/data"},
		{"/mnt/data/nested/deep.bin", "/mnt/data"},
		{"/workspace/report.csv", ""},
		{"/mnt/data-old/report.csv", ""},
		{"/mnt", ""},
	} {
		got, err := diskMountBlocksFileAPI(context.Background(), f.client(), "sb-1", tc.remote)
		if err != nil {
			t.Fatalf("diskMountBlocksFileAPI(%q): %v", tc.remote, err)
		}
		if got != tc.want {
			t.Errorf("diskMountBlocksFileAPI(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}

// A sandbox with no disks must never be blocked.
func TestDiskMountAllowsASandboxWithNoDisks(t *testing.T) {
	empty := newFakeAPI(t).json("GET /v1/sandboxes/sb-1/disks", `{"data":{"data":[]}}`)
	got, err := diskMountBlocksFileAPI(context.Background(), empty.client(), "sb-1", "/anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("no disks attached, got %q, want no block", got)
	}
}

// TestDiskMountFailsClosed is the important half. When the disk list
// cannot be read the mount state is unknown, and "unknown" must not be
// treated as "safe": a wrong guess here crashes an S3 mount and loses the
// object, while a refusal only costs a retry.
func TestDiskMountFailsClosed(t *testing.T) {
	broken := newFakeAPI(t).fails("GET /v1/sandboxes/sb-1/disks")
	_, err := diskMountBlocksFileAPI(context.Background(), broken.client(), "sb-1", "/workspace/out.csv")
	if err == nil {
		t.Fatal("disk list unreadable but the transfer was allowed — this is how the object gets lost")
	}
	for _, want := range []string{"/workspace/out.csv", "#71", "sandbox exec"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %q, got: %v", want, err)
		}
	}
}

func TestDiskMountFileAPIError(t *testing.T) {
	err := diskMountFileAPIError("/mnt/data/out.csv", "/mnt/data", "push")
	for _, want := range []string{"/mnt/data/out.csv", "/mnt/data", "#71", "sandbox exec"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %q, got: %v", want, err)
		}
	}
}

// TestSelfSignalHTTP checks the wire shape the guest agent expects: a POST
// to /self/<action>, the reason carried as a query parameter, and 202
// treated as success.
func TestSelfSignalHTTP(t *testing.T) {
	var gotMethod, gotPath, gotReason string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotReason = r.Method, r.URL.Path, r.URL.Query().Get("reason")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted","action":"pause"}`))
	}))
	defer srv.Close()

	// selfSignalHTTP hardcodes the loopback address, so point the test at
	// the fake server by rebuilding the same request shape it sends.
	addr := strings.TrimPrefix(srv.URL, "http://")
	original := selfSignalAddrForTest
	selfSignalAddrForTest = addr
	t.Cleanup(func() { selfSignalAddrForTest = original })

	if err := selfSignalHTTP(context.Background(), "pause", "job done"); err != nil {
		t.Fatalf("selfSignalHTTP: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/self/pause" {
		t.Errorf("path = %s, want /self/pause", gotPath)
	}
	if gotReason != "job done" {
		t.Errorf("reason = %q, want %q", gotReason, "job done")
	}
}

func TestSelfSignalHTTPRejectsUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	original := selfSignalAddrForTest
	selfSignalAddrForTest = strings.TrimPrefix(srv.URL, "http://")
	t.Cleanup(func() { selfSignalAddrForTest = original })

	err := selfSignalHTTP(context.Background(), "pause", "")
	if err == nil {
		t.Fatal("want an error when something other than the agent answers")
	}
}
