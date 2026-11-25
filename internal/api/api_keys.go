package api

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// CreateAPIKeyInput specifies the name, expiration, and optional description of a key.
type CreateAPIKeyInput struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ExpiryAt    time.Time `json:"expiryAt"`
}

// CreatedAPIKey contains the ID and secret returned at creation.
type CreatedAPIKey struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

// CreateAPIKey creates a key for the authenticated user.
func (c *APIClient) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreatedAPIKey, error) {
	var result Response[CreatedAPIKey]
	// The response contains the new secret, so omit the Resty debug dump.
	resp, err := c.Client.R().SetDebug(false).SetContext(ctx).
		SetBody(input).SetResult(&result).Post("/v1/api-keys")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	if result.Data.ID == "" || result.Data.Secret == "" {
		return nil, fmt.Errorf("API key creation response is missing the ID or secret")
	}
	return &result.Data, nil
}

// KeySummary contains the key metadata returned by the list endpoint.
type KeySummary struct {
	ID           string     `json:"id"`
	Name         *string    `json:"name"`
	Description  *string    `json:"description"`
	UserID       string     `json:"userId"`
	MaskedSecret string     `json:"maskedSecret,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	ExpiredAt    *time.Time `json:"expiredAt"`
	RevokedAt    *time.Time `json:"revokedAt"`
}

// ListAPIKeys retrieves the authenticated user’s API keys and their masked previews.
func (c *APIClient) ListAPIKeys(ctx context.Context) ([]KeySummary, error) {
	var result Response[[]KeySummary]
	resp, err := c.Client.R().SetContext(ctx).SetResult(&result).Get("/v1/api-keys")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	// Some servers mask the stored secret before adding the public key prefix.
	for i := range result.Data {
		preview := result.Data[i].MaskedSecret
		if preview != "" && !strings.HasPrefix(preview, "skp-") {
			result.Data[i].MaskedSecret = "skp-" + preview
		}
	}
	return result.Data, nil
}

// RevokeAPIKey revokes the key with the given ID.
func (c *APIClient) RevokeAPIKey(ctx context.Context, id string) error {
	resp, err := c.Client.R().SetContext(ctx).Put("/v1/api-keys/" + url.PathEscape(id) + "/revoke")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
