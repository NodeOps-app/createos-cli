package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The readiness wait in `sandbox desktop` keeps polling while an error is
// retryable and gives up immediately when it is not. Misclassify one and the
// command either spins for the whole timeout on a failure that will never
// clear, or abandons a desktop that was still starting.
func TestComputerErrorRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
		why    string
	}{
		{http.StatusNotFound, true, "the screen appears only once the desktop is up"},
		{http.StatusConflict, true, "fc uses 409 for 'still booting' as well as 'action failed'"},
		{http.StatusTooManyRequests, true, "rate limiting clears on its own"},
		{http.StatusNotImplemented, false, "an image without desktop tools never grows them"},
		{http.StatusUnauthorized, false, "bad credentials do not fix themselves"},
		{http.StatusForbidden, false, "bad credentials do not fix themselves"},
	}
	for _, tc := range cases {
		err, ok := ParseComputerError(tc.status, nil).(*ComputerError)
		if !ok {
			t.Fatalf("status %d: expected a *ComputerError", tc.status)
		}
		if got := err.Retryable(); got != tc.want {
			t.Errorf("status %d: Retryable() = %v, want %v — %s", tc.status, got, tc.want, tc.why)
		}
	}
}

// A 409 mentioning ingress is a different fix from a 409 about the desktop,
// so the two must not collapse into one message.
func TestComputerErrorConflictDistinguishesIngress(t *testing.T) {
	body := []byte(`{"status":"fail","data":"ingress is not enabled"}`)
	ingress := ParseComputerError(http.StatusConflict, body).Error()
	if !strings.Contains(ingress, "public URL is off") {
		t.Errorf("ingress conflict should name the public URL, got: %q", ingress)
	}

	desktop := ParseComputerError(http.StatusConflict, []byte(`{"status":"fail","data":"desktop_unavailable"}`)).Error()
	if strings.Contains(desktop, "public URL is off") {
		t.Errorf("a desktop_unavailable conflict should not be reported as an ingress problem, got: %q", desktop)
	}
	if !strings.Contains(desktop, "still starting up") {
		t.Errorf("a desktop conflict should say it may still be starting, got: %q", desktop)
	}
}

func TestComputerScreenDefaults(t *testing.T) {
	if got := computerScreen(""); got != DefaultComputerScreen {
		t.Errorf("computerScreen(%q) = %q, want %q", "", got, DefaultComputerScreen)
	}
	if got := computerScreen("  "); got != DefaultComputerScreen {
		t.Errorf("computerScreen(whitespace) = %q, want %q", got, DefaultComputerScreen)
	}
	if got := computerScreen("screen-2"); got != "screen-2" {
		t.Errorf("computerScreen(%q) = %q, want it unchanged", "screen-2", got)
	}
}

func TestUnwrapEnvelope(t *testing.T) {
	wrapped := unwrapEnvelope([]byte(`{"status":"success","data":{"width":1280}}`))
	var got struct {
		Width int `json:"width"`
	}
	if err := json.Unmarshal(wrapped, &got); err != nil {
		t.Fatalf("unwrapping an enveloped body: %v", err)
	}
	if got.Width != 1280 {
		t.Errorf("width = %d, want 1280", got.Width)
	}

	// Not every computer route envelopes its payload, so a bare body has to
	// survive untouched rather than come back empty.
	bare := []byte(`[{"id":1}]`)
	if string(unwrapEnvelope(bare)) != string(bare) {
		t.Errorf("a bare body should pass through unchanged, got %q", unwrapEnvelope(bare))
	}
}
