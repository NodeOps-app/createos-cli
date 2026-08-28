// Package api provides the HTTP client and API methods.
package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// TokenRefresher obtains a fresh access token after the server rejects
// the current one with 401. The implementation is responsible for
// persisting the new token (e.g. back to ~/.createos/.oauth). Returning
// an error leaves the original 401 to propagate to the caller.
type TokenRefresher func() (string, error)

// installAuthRefresh makes client refresh its auth header and retry the
// request once when the server answers 401. authHeader is the header
// carrying the credential (e.g. "X-Access-Token"). A nil refresher is a
// no-op, so API-key clients keep their single-shot behaviour.
//
// The refresh fires on exactly the first 401 of a request and only when
// the request body is replayable: resty leaves Request.Body as an
// io.Reader only on its unbuffered streaming path (e.g. file uploads),
// where the reader is already consumed and a retry would send an empty
// body. Those requests skip the retry and surface the 401.
func installAuthRefresh(client *resty.Client, authHeader string, refresher TokenRefresher) {
	if refresher == nil {
		return
	}
	// The retry budget itself belongs to installTransientRetry, which every
	// constructor installs. This condition only adds one more reason to
	// spend an attempt, and Attempt==1 below keeps it to a single refresh.
	client.AddRetryCondition(func(resp *resty.Response, _ error) bool {
		if resp == nil || resp.StatusCode() != http.StatusUnauthorized {
			return false
		}
		// resty re-evaluates the condition on the final attempt too;
		// gating on Attempt==1 keeps us from firing a second,
		// refresh-token-rotating refresh.
		if resp.Request.Attempt != 1 {
			return false
		}
		if _, ok := resp.Request.Body.(io.Reader); ok {
			return false // non-replayable streaming body — don't retry
		}
		newToken, err := refresher()
		if err != nil {
			return false // refresh failed — surface the original 401
		}
		client.SetHeader(authHeader, newToken)
		resp.Request.Header.Set(authHeader, newToken)
		return true
	})
}

// Retry budget shared by installTransientRetry and installAuthRefresh.
// Three attempts covers a single flaky hop without turning a real outage
// into a long stall.
const (
	transientRetryCount   = 3
	transientRetryWait    = 300 * time.Millisecond
	transientRetryMaxWait = 3 * time.Second
)

// installTransientRetry retries the failures a retry can actually fix.
// One dropped connection used to kill a whole command; a fan-out over N
// sandboxes multiplies that exposure by N, so this is the difference
// between a flaky run and a failed one.
//
// What retries, and why the split by method:
//
//   - A connection that was never established (DNS failure, dial timeout,
//     connection refused). The request provably never reached the server,
//     so replaying it cannot duplicate anything. Safe for every method,
//     POST included.
//   - A 429 or 5xx answer, but only for methods with no side effect. A
//     POST that got a 500 may well have created the sandbox before it
//     failed, and a retry would leak a second one that nobody destroys.
//     Leaking billable machines is worse than surfacing the error.
func installTransientRetry(client *resty.Client) {
	client.SetRetryCount(transientRetryCount)
	client.SetRetryWaitTime(transientRetryWait)
	client.SetRetryMaxWaitTime(transientRetryMaxWait)
	client.AddRetryCondition(func(resp *resty.Response, err error) bool {
		if err != nil {
			return isConnectSetupError(err)
		}
		if resp == nil {
			return false
		}
		switch resp.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			return false
		}
		return resp.StatusCode() == http.StatusTooManyRequests || resp.StatusCode() >= http.StatusInternalServerError
	})
}

// isConnectSetupError reports whether err failed before any bytes reached
// the server. Only "dial" operations qualify: a read or write error means
// the request was already on the wire and may have been acted on.
func isConnectSetupError(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial"
	}
	return false
}

// Auth header names. HTTP header keys are case-insensitive (and Go
// canonicalises them on the wire), so these double as the API-key and
// OAuth-access-token headers for both the main API and the fc-spawn
// sandbox API.
const (
	headerAPIKey      = "X-Api-Key"      // #nosec G101 -- HTTP header name, not a credential  // pragma: allowlist secret
	headerAccessToken = "X-Access-Token" // #nosec G101 -- HTTP header name, not a credential  // pragma: allowlist secret
)

// DefaultBaseURL is the default CreateOS API base URL.
const DefaultBaseURL = "https://api-createos.nodeops.network"

// APIClient wraps a resty.Client configured for the CreateOS API.
type APIClient struct { //nolint:revive
	Client *resty.Client
}

// NewClient creates a resty client with the token, base URL and debug flag set
func NewClient(token, apiURL string, debug bool) APIClient {
	if apiURL == "" {
		apiURL = DefaultBaseURL
	}

	client := resty.New().
		SetBaseURL(apiURL).
		SetHeader(headerAPIKey, token).
		SetHeader("Content-Type", "application/json")

	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  token,
			masked: maskToken(token),
		})
	}

	installTransientRetry(client)

	return APIClient{Client: client}
}

// NewClientWithAccessToken creates a resty client authenticated with an OAuth access token.
// Uses X-Access-Token header instead of x-api-key. When refresher is non-nil the client
// refreshes the token and retries once on a 401 (see installAuthRefresh).
func NewClientWithAccessToken(accessToken, apiURL string, debug bool, refresher TokenRefresher) APIClient {
	if apiURL == "" {
		apiURL = DefaultBaseURL
	}

	client := resty.New().
		SetBaseURL(apiURL).
		SetHeader(headerAccessToken, accessToken).
		SetHeader("Content-Type", "application/json")

	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  accessToken,
			masked: maskToken(accessToken),
		})
	}

	installTransientRetry(client)
	installAuthRefresh(client, headerAccessToken, refresher)

	return APIClient{Client: client}
}

// maskToken returns a redacted version like "skp_Ex6v••••••••3fae"
func maskToken(token string) string {
	if len(token) <= 8 {
		return "••••••••"
	}
	return fmt.Sprintf("%s••••••••%s", token[:6], token[len(token)-4:])
}

// maskingLogger wraps the default resty logger and redacts the API token.
type maskingLogger struct {
	token  string
	masked string
}

func (l *maskingLogger) redact(s string) string {
	return strings.ReplaceAll(s, l.token, l.masked)
}

func (l *maskingLogger) Errorf(format string, v ...interface{}) {
	log.Printf("ERROR RESTY "+l.redact(format), v...)
}

func (l *maskingLogger) Warnf(format string, v ...interface{}) {
	log.Printf("WARN RESTY "+l.redact(format), v...)
}

func (l *maskingLogger) Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Print("DEBUG RESTY " + l.redact(msg))
}

// ClientKey is the key used to store the resty client in cli.Context metadata
const ClientKey = "api_client"
