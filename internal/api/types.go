package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// APIError is a structured error returned by the CreateOS API.
type APIError struct { //nolint:revive
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// Hint returns a contextual suggestion based on the HTTP status code.
func (e *APIError) Hint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Run 'createos login' to sign in again."
	case http.StatusNotFound:
		return "Double-check the ID is correct. Run the list command to see available items."
	case http.StatusBadRequest:
		return "Check that the value you provided is correct and try again."
	default:
		return ""
	}
}

// ParseAPIError extracts a human-readable message from an API error response body.
//
// JSend "fail" bodies can shape `data` three different ways:
//
//	"data": "shape is required"                          // plain string
//	"data": {"shape": "shape \"x\" not allowed..."}      // field-error object
//	"data": {"auth": "invalid api key"}                  // single-field gate
//
// We try each in order and join field-error values into one human line so
// the user actually sees what's wrong instead of "request failed with status 403".
func ParseAPIError(statusCode int, body []byte) *APIError {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	msg := ""
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		// 1. plain string
		var s string
		if err := json.Unmarshal(envelope.Data, &s); err == nil && s != "" {
			msg = s
		}
		// 2. field-error map { "field": "msg", ... }
		if msg == "" {
			var fields map[string]string
			if err := json.Unmarshal(envelope.Data, &fields); err == nil && len(fields) > 0 {
				parts := make([]string, 0, len(fields))
				for _, v := range fields {
					if v != "" {
						parts = append(parts, v)
					}
				}
				sort.Strings(parts)
				msg = strings.Join(parts, "; ")
			}
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("request failed with status %d", statusCode)
	}
	return &APIError{StatusCode: statusCode, Message: msg}
}

// User represents a CreateOS user
type User struct {
	ID               string  `json:"id"`
	DisplayName      *string `json:"displayName"`
	Username         *string `json:"username"`
	Email            string  `json:"email"`
	ProfileImagePath *string `json:"profileImagePath"`
	SuspendedAt      *string `json:"suspendedAt"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// Response wraps a single-item API response envelope.
type Response[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
}
