package api

import (
	"encoding/json"
	"time"
)

// WebhookEndpoint represents a user-created webhook endpoint.
type WebhookEndpoint struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	URL          string    `json:"url"`
	Events       []string  `json:"events"`
	Active       bool      `json:"active"`
	FailureCount int       `json:"failureCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// WebhookDelivery represents a delivery attempt for a webhook.
type WebhookDelivery struct {
	ID            string          `json:"id"`
	EndpointID    string          `json:"endpointId"`
	AuditLogID    string          `json:"auditLogId"`
	EventAction   string          `json:"eventAction"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	LastError     *string         `json:"lastError"`
	NextAttemptAt time.Time       `json:"nextAttemptAt"`
	CreatedAt     time.Time       `json:"createdAt"`
	DeliveredAt   *time.Time      `json:"deliveredAt"`
}

// CreateWebhookEndpointRequest is the request body for creating a webhook endpoint.
type CreateWebhookEndpointRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// CreateWebhookEndpointResponse is returned after creating a webhook endpoint.
type CreateWebhookEndpointResponse struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
}

// GetWebhookEndpointResponse wraps an endpoint with its recent deliveries.
type GetWebhookEndpointResponse struct {
	Endpoint   WebhookEndpoint   `json:"endpoint"`
	Deliveries []WebhookDelivery `json:"deliveries"`
}

// ListWebhookEndpoints returns all webhook endpoints for the authenticated user.
func (c *APIClient) ListWebhookEndpoints() ([]WebhookEndpoint, error) {
	var result Response[[]WebhookEndpoint]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/webhook-endpoints")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// GetWebhookEndpoint returns a single webhook endpoint with its recent deliveries.
func (c *APIClient) GetWebhookEndpoint(id string) (*GetWebhookEndpointResponse, error) {
	var result Response[GetWebhookEndpointResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/webhook-endpoints/" + id)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// CreateWebhookEndpoint creates a new webhook endpoint and returns the response including the signing secret.
func (c *APIClient) CreateWebhookEndpoint(req CreateWebhookEndpointRequest) (*CreateWebhookEndpointResponse, error) {
	var result Response[CreateWebhookEndpointResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(req).
		Post("/v1/webhook-endpoints")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// DeleteWebhookEndpoint deletes a webhook endpoint by ID.
func (c *APIClient) DeleteWebhookEndpoint(id string) error {
	resp, err := c.Client.R().
		Delete("/v1/webhook-endpoints/" + id)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// SuspendWebhookEndpoint suspends a webhook endpoint.
func (c *APIClient) SuspendWebhookEndpoint(id string) error {
	resp, err := c.Client.R().
		Post("/v1/webhook-endpoints/" + id + "/suspend")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ResumeWebhookEndpoint resumes a suspended webhook endpoint.
func (c *APIClient) ResumeWebhookEndpoint(id string) error {
	resp, err := c.Client.R().
		Post("/v1/webhook-endpoints/" + id + "/resume")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListWebhookActions returns all supported webhook event action names.
func (c *APIClient) ListWebhookActions() ([]string, error) {
	var result Response[[]string]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/webhook-endpoints/actions")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}
