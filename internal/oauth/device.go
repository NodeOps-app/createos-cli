package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// DeviceAuthorization holds the codes and browser URLs for a pending device login.
// DeviceCode is a credential: never display it or write it to logs.
type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	expiresAt               time.Time
}

// StartDeviceAuthorization requests a device code from the public OAuth endpoint.
func StartDeviceAuthorization(ctx context.Context, endpoint, clientID string) (*DeviceAuthorization, error) {
	started := time.Now()
	status, body, err := devicePost(ctx, endpoint, url.Values{
		"client_id": {clientID}, "scope": {"openid offline_access"},
	})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, deviceResponseError(body)
	}
	var auth DeviceAuthorization
	if json.Unmarshal(body, &auth) != nil || auth.DeviceCode == "" || auth.UserCode == "" ||
		!validDeviceURL(auth.VerificationURI) || (auth.VerificationURIComplete != "" && !validDeviceURL(auth.VerificationURIComplete)) ||
		auth.ExpiresIn <= 0 || auth.ExpiresIn > int64((1<<63-1)/time.Second) || auth.Interval < 0 || auth.Interval > auth.ExpiresIn {
		return nil, fmt.Errorf("sign in returned an invalid response — run 'createos login' to try again")
	}
	if auth.Interval == 0 {
		auth.Interval = 5
	}
	auth.expiresAt = started.Add(time.Duration(auth.ExpiresIn) * time.Second)
	return &auth, nil
}

// PollDeviceToken waits for browser approval, respecting the server's polling interval.
func PollDeviceToken(ctx context.Context, endpoint, clientID string, auth *DeviceAuthorization) (*TokenResponse, error) {
	return pollDeviceToken(ctx, endpoint, clientID, auth, waitDevicePoll)
}

func pollDeviceToken(ctx context.Context, endpoint, clientID string, auth *DeviceAuthorization, wait func(context.Context, time.Duration) error) (*TokenResponse, error) {
	if auth == nil || auth.DeviceCode == "" || auth.expiresAt.IsZero() {
		return nil, fmt.Errorf("no pending sign in — run 'createos login' to start again")
	}
	ctx, cancel := context.WithDeadline(ctx, auth.expiresAt)
	defer cancel()
	interval := time.Duration(auth.Interval) * time.Second
	form := url.Values{"client_id": {clientID}, "device_code": {auth.DeviceCode}, "grant_type": {deviceGrantType}}
	for {
		if err := wait(ctx, interval); err != nil {
			return nil, deviceContextError(err)
		}
		status, body, err := devicePost(ctx, endpoint, form)
		if err != nil {
			// A lost response may already have issued tokens. Do not replay the code.
			return nil, err
		}
		if status == http.StatusOK {
			var token TokenResponse
			if json.Unmarshal(body, &token) != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn <= 0 {
				return nil, fmt.Errorf("sign in returned an invalid session — run 'createos login' to start again")
			}
			return &token, nil
		}
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && status == http.StatusBadRequest {
			switch failure.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				if interval > time.Until(auth.expiresAt)-5*time.Second {
					return nil, deviceContextError(context.DeadlineExceeded)
				}
				interval += 5 * time.Second
				continue
			}
		}
		return nil, deviceResponseError(body)
	}
}

func waitDevicePoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func devicePost(ctx context.Context, endpoint string, form url.Values) (int, []byte, error) {
	if !validDeviceURL(endpoint) {
		return 0, nil, fmt.Errorf("device sign in is unavailable — use 'createos login --token' or contact support")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("could not start sign in — run 'createos login' to try again")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, deviceContextError(ctx.Err())
		}
		return 0, nil, fmt.Errorf("lost connection during sign in — run 'createos login' to start a new attempt")
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("could not read the sign-in response — run 'createos login' to start again")
	}
	return resp.StatusCode, body, nil
}

func validDeviceURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

func deviceContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("sign-in code expired — run 'createos login' to get a new code")
	}
	return fmt.Errorf("sign in cancelled — run 'createos login' when you're ready")
}

func deviceResponseError(body []byte) error {
	var failure struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &failure); err != nil {
		return fmt.Errorf("could not complete sign in — run 'createos login' to start again")
	}
	switch failure.Error {
	case "access_denied":
		return fmt.Errorf("sign in was denied — run 'createos login' to try again")
	case "expired_token":
		return deviceContextError(context.DeadlineExceeded)
	case "invalid_grant":
		return fmt.Errorf("sign-in code is no longer valid — run 'createos login' to get a new code")
	case "unauthorized_client", "invalid_client", "unsupported_grant_type", "invalid_scope":
		return fmt.Errorf("device sign in is not enabled for this CLI — use 'createos login --token' or contact support")
	default:
		return fmt.Errorf("could not complete sign in — run 'createos login' to start again")
	}
}
