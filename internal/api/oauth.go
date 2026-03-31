package api

import "time"

// CreateOAuthClientInput holds the fields required to create an OAuth client.
type CreateOAuthClientInput struct {
	Name         string   `json:"name"`
	RedirectUris []string `json:"redirectUris"`
	Public       bool     `json:"public"`
	URI          string   `json:"uri"`
	PolicyURI    string   `json:"policyUri"`
	TOSURI       string   `json:"tosUri"`
	LogoURI      string   `json:"logoUri"`
}

// OAuthClientSummary is a brief representation of an OAuth client.
type OAuthClientSummary struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// OAuthClientDetail holds the full detail of an OAuth client including credentials.
type OAuthClientDetail struct {
	ID           string    `json:"id"`
	ClientID     string    `json:"clientId"`
	ClientName   string    `json:"clientName"`
	ClientSecret *string   `json:"clientSecret"`
	RedirectUris []string  `json:"redirectUris"`
	Public       bool      `json:"public"`
	ClientURI    *string   `json:"clientUri"`
	PolicyURI    *string   `json:"policyUri"`
	TOSURI       *string   `json:"tosUri"`
	LogoURI      *string   `json:"logoUri"`
	CreatedAt    time.Time `json:"createdAt"`
}

// OAuthConsent represents a user's consent granted to an OAuth client.
type OAuthConsent struct {
	ClientID   *string `json:"clientId"`
	ClientName *string `json:"clientName"`
	ClientURI  *string `json:"clientUri"`
	PolicyURI  *string `json:"policyUri"`
	TOSURI     *string `json:"tosUri"`
	LogoURI    *string `json:"logoUri"`
}

// CreateOAuthClient creates a new OAuth client and returns its ID.
func (c *APIClient) CreateOAuthClient(input CreateOAuthClientInput) (string, error) {
	var result Response[struct {
		ID string `json:"id"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(input).
		Post("/v1/-/oauth2/clients")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// ListOAuthClients returns all OAuth clients for the authenticated user.
func (c *APIClient) ListOAuthClients() ([]OAuthClientSummary, error) {
	var result Response[[]OAuthClientSummary]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/-/oauth2/clients")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// GetOAuthClient returns the full detail for a single OAuth client by its client ID.
func (c *APIClient) GetOAuthClient(clientID string) (*OAuthClientDetail, error) {
	var result Response[OAuthClientDetail]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/-/oauth2/clients/" + clientID)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// DeleteOAuthClient permanently deletes an OAuth client.
func (c *APIClient) DeleteOAuthClient(clientID string) error {
	resp, err := c.Client.R().
		Delete("/v1/-/oauth2/clients/" + clientID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListOAuthConsents returns all OAuth consents granted by the authenticated user.
func (c *APIClient) ListOAuthConsents() ([]OAuthConsent, error) {
	var result Response[[]OAuthConsent]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/users/oauth2/consents")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// RevokeOAuthConsent revokes all tokens for a given OAuth client on behalf of the user.
func (c *APIClient) RevokeOAuthConsent(clientID string) error {
	resp, err := c.Client.R().
		Delete("/v1/users/oauth2/clients/" + clientID + "/revoke-tokens")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
