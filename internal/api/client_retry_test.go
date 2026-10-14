package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestLifecyclePOSTSendsContentLength guards the pause/resume regression.
// A body-less resty POST makes Go omit Content-Length, and control's
// forwarder drops any inbound content-length header, so the owning host
// answered "Content-Length is required" and every pause and resume failed
// while fork (which always had a body) kept working.
func TestLifecyclePOSTSendsContentLength(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*SandboxClient, context.Context) error
	}{
		{"pause", func(c *SandboxClient, ctx context.Context) error {
			_, err := c.PauseSandbox(ctx, "sb-1")
			return err
		}},
		{"resume", func(c *SandboxClient, ctx context.Context) error {
			_, err := c.ResumeSandbox(ctx, "sb-1")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotLength int64 = -1
			var gotHeader, gotType string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotLength = r.ContentLength
				gotHeader = r.Header.Get("Content-Length")
				gotType = r.Header.Get("Content-Type")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"id":"sb-1","status":"pausing"}}`))
			}))
			defer srv.Close()

			client := NewSandboxClient("tok", srv.URL, false)
			if err := tc.call(&client, context.Background()); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if gotLength < 0 {
				t.Errorf("ContentLength = %d, want >= 0 (unknown length means no header on the wire)", gotLength)
			}
			if gotHeader == "" {
				t.Error("Content-Length header absent — this is the exact failure the fix targets")
			}
			if gotType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json (RequireJSON rejects anything else)", gotType)
			}
		})
	}
}

// TestTransientRetryOnlyReplaysSafeRequests pins the split that keeps a
// retry from leaking a second billable sandbox: read-only methods retry on
// 5xx, mutating ones do not.
func TestTransientRetryOnlyReplaysSafeRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		post  bool
		calls int32
	}{
		{"get retries on 500", false, transientRetryCount + 1},
		{"post does not retry on 500", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			client := NewSandboxClient("tok", srv.URL, false)
			req := client.Client.R()
			var err error
			if tc.post {
				_, err = req.SetBody(struct{}{}).Post("/v1/thing")
			} else {
				_, err = req.Get("/v1/thing")
			}
			if err != nil {
				t.Fatalf("request error: %v", err)
			}
			if got := atomic.LoadInt32(&calls); got != tc.calls {
				t.Errorf("server saw %d call(s), want %d", got, tc.calls)
			}
		})
	}
}

func TestIsConnectSetupError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"dns failure never reached the server", &net.DNSError{Err: "no such host"}, true},
		{"dial timeout never reached the server", &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}, true},
		{"read error means the request was already sent", &net.OpError{Op: "read", Err: errors.New("reset")}, false},
		{"write error means the request was already sent", &net.OpError{Op: "write", Err: errors.New("broken pipe")}, false},
		{"plain error", errors.New("boom"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnectSetupError(tc.err); got != tc.want {
				t.Errorf("isConnectSetupError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
