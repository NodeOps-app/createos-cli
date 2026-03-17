package api

import (
	"encoding/json"
	"fmt"
	"net/http"
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
func ParseAPIError(statusCode int, body []byte) *APIError {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	msg := ""
	if err := json.Unmarshal(body, &envelope); err == nil {
		// data may be a plain string or an object
		var s string
		if err := json.Unmarshal(envelope.Data, &s); err == nil {
			msg = s
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
