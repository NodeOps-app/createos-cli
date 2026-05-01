// Package telemetry provides event capture and error categorization.
//
// Error categorization uses string-matching needles to map locally-raised CLI
// errors to a category. The list is intentionally small and acknowledged-fragile;
// the CLI's user-facing error strings are stable enough (UX-tested) that flips
// between buckets are rare. When error messages change in cmd/auth/login.go,
// cmd/root/root.go, or anywhere else, update the corresponding needle below.
//
// Order of checks (first match wins):
//  1. *api.APIError → category by HTTP status.
//  2. context.DeadlineExceeded / net.Error → "network".
//  3. local sentinel substrings → "user_input" first, then "auth".
//     user_input is checked first because some validation messages contain
//     auth-shaped phrases (e.g. "use --token flag to sign in").
//  4. default → "unknown".
package telemetry

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// CategorizeError returns a stable category label and (when known) the API
// HTTP status code. apiStatusCode is 0 for non-API errors.
func CategorizeError(err error) (category string, apiStatusCode int) {
	if err == nil {
		return "unknown", 0
	}

	// 1. API errors — bucket by HTTP status.
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401, apiErr.StatusCode == 403:
			return "auth", apiErr.StatusCode
		case apiErr.StatusCode == 404:
			return "not_found", apiErr.StatusCode
		case apiErr.StatusCode == 400, apiErr.StatusCode == 422:
			return "validation", apiErr.StatusCode
		case apiErr.StatusCode == 429:
			return "rate_limit", apiErr.StatusCode
		case apiErr.StatusCode >= 500:
			return "api_5xx", apiErr.StatusCode
		}
		return "unknown", apiErr.StatusCode
	}

	// 2. Network / deadline.
	if errors.Is(err, context.DeadlineExceeded) {
		return "network", 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network", 0
	}

	// 3. Locally-raised sentinels — string match on err.Error().
	// user_input first: some validation messages embed "sign in" wording
	// (e.g. "non-interactive mode: use --token flag to sign in").
	msg := err.Error()
	for _, needle := range userInputNeedles {
		if strings.Contains(msg, needle) {
			return "user_input", 0
		}
	}
	for _, needle := range authNeedles {
		if strings.Contains(msg, needle) {
			return "auth", 0
		}
	}

	return "unknown", 0
}

// authNeedles match locally-raised auth errors (signed-in checks, login
// prompts). Update these in lockstep with cmd/auth/login.go and cmd/root.
var authNeedles = []string{
	"sign in",
	"signed in",
	"login",
}

// userInputNeedles match validation errors raised by us OR by urfave/cli's
// framework. Update when we change a "please provide ..." or "--api-url
// must ..." string in the CLI.
var userInputNeedles = []string{
	"--api-url must",
	"must use HTTPS",
	"non-interactive mode",
	"could not save",
	"please provide",
	"missing argument",
	"required flag",
	"flag provided but not defined",
}
