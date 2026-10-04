package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStartDeviceAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.Method != http.MethodPost || r.FormValue("client_id") != "cli" || r.FormValue("scope") != "openid offline_access" {
			t.Error("incorrect device authorization request")
		}
		fmt.Fprint(w, `{"device_code":"private-code","user_code":"ABCD","verification_uri":"https://auth.example/verify","expires_in":600}`)
	}))
	defer server.Close()
	auth, err := StartDeviceAuthorization(context.Background(), server.URL, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Interval != 5 || auth.UserCode != "ABCD" || time.Until(auth.expiresAt) > 600*time.Second {
		t.Fatalf("incorrect authorization metadata: interval=%d", auth.Interval)
	}
}

func TestDevicePolling(t *testing.T) {
	for _, tc := range []struct {
		name      string
		responses []string
		intervals []time.Duration
		wantError string
	}{
		{"approval", []string{`{"error":"authorization_pending"}`, `{"access_token":"access","refresh_token":"refresh","token_type":"bearer","expires_in":3600}`}, []time.Duration{5 * time.Second, 5 * time.Second}, ""},
		{"slow down persists", []string{`{"error":"slow_down"}`, `{"error":"authorization_pending"}`, `{"error":"slow_down"}`, `{"access_token":"access","token_type":"bearer","expires_in":3600}`}, []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second, 15 * time.Second}, ""},
		{"denied", []string{`{"error":"access_denied"}`}, []time.Duration{5 * time.Second}, "denied"},
		{"expired", []string{`{"error":"expired_token"}`}, []time.Duration{5 * time.Second}, "expired"},
		{"already used", []string{`{"error":"invalid_grant"}`}, []time.Duration{5 * time.Second}, "no longer valid"},
		{"unregistered", []string{`{"error":"unauthorized_client"}`}, []time.Duration{5 * time.Second}, "not enabled"},
		{"malformed", []string{`broken`}, []time.Duration{5 * time.Second}, "could not complete"},
		{"empty token", []string{`{"access_token":"","token_type":"bearer","expires_in":3600}`}, []time.Duration{5 * time.Second}, "invalid session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				if r.FormValue("device_code") != "private-code" || r.FormValue("grant_type") != deviceGrantType || r.FormValue("client_id") != "cli" {
					t.Error("incorrect token request")
				}
				if calls >= len(tc.responses) {
					t.Error("polled after terminal response")
					w.WriteHeader(500)
					return
				}
				body := tc.responses[calls]
				calls++
				if !strings.Contains(body, "access_token") {
					w.WriteHeader(400)
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			var waits []time.Duration
			wait := func(ctx context.Context, interval time.Duration) error {
				waits = append(waits, interval)
				return ctx.Err()
			}
			auth := &DeviceAuthorization{DeviceCode: "private-code", Interval: 5, expiresAt: time.Now().Add(time.Minute)}
			token, err := pollDeviceToken(context.Background(), server.URL, "cli", auth, wait)
			if tc.wantError == "" {
				if err != nil || token == nil || token.AccessToken != "access" {
					t.Fatalf("approval failed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q, got %v", tc.wantError, err)
			}
			if !reflect.DeepEqual(waits, tc.intervals) {
				t.Fatalf("poll intervals: %v; want %v", waits, tc.intervals)
			}
		})
	}
}

func TestDeviceCancellationAndExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auth := &DeviceAuthorization{DeviceCode: "private-code", Interval: 5, expiresAt: time.Now().Add(time.Minute)}
	if _, err := PollDeviceToken(ctx, "http://127.0.0.1:1", "cli", auth); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("unexpected cancellation: %v", err)
	}
	auth.expiresAt = time.Now().Add(-time.Second)
	if _, err := PollDeviceToken(context.Background(), "http://127.0.0.1:1", "cli", auth); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("unexpected expiry: %v", err)
	}
}

func TestDeviceInvalidResponses(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"device_code":"secret","user_code":"code","verification_uri":"javascript:alert(1)","expires_in":600}`,
		`{"device_code":"secret","user_code":"code","verification_uri":"https://auth.example/verify","expires_in":0}`,
		`{"device_code":"secret","user_code":"code","verification_uri":"https://auth.example/verify","expires_in":600,"interval":-1}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		_, err := StartDeviceAuthorization(context.Background(), server.URL, "cli")
		server.Close()
		if err == nil {
			t.Fatal("accepted invalid response")
		}
	}
}

func TestDeviceConnectionFailureDoesNotReplay(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(502)
		fmt.Fprint(w, "upstream unavailable")
	}))
	defer server.Close()
	auth := &DeviceAuthorization{DeviceCode: "private-code", Interval: 5, expiresAt: time.Now().Add(time.Minute)}
	_, err := pollDeviceToken(context.Background(), server.URL, "cli", auth, func(context.Context, time.Duration) error { return nil })
	if err == nil || calls != 1 {
		t.Fatalf("unexpected retries: %d, %v", calls, err)
	}
}
