package api

import "time"

type CreateOAuthClientInput struct {
	Name         string   `json:"name"`
	RedirectUris []string `json:"redirectUris"`
	Public       bool     `json:"public"`
	URI          string   `json:"uri"`
	PolicyURI    string   `json:"policyUri"`
	TOSURI       string   `json:"tosUri"`
	LogoURI      string   `json:"logoUri"`
}

type OAuthClientSummary struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

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

type OAuthConsent struct {
	ClientID   *string `json:"clientId"`
	ClientName *string `json:"clientName"`
	ClientURI  *string `json:"clientUri"`
	PolicyURI  *string `json:"policyUri"`
	TOSURI     *string `json:"tosUri"`
	LogoURI    *string `json:"logoUri"`
}

func (c *ApiClient) CreateOAuthClient(input CreateOAuthClientInput) (string, error) {
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

func (c *ApiClient) ListOAuthClients() ([]OAuthClientSummary, error) {
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

func (c *ApiClient) GetOAuthClient(clientID string) (*OAuthClientDetail, error) {
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

func (c *ApiClient) ListOAuthConsents() ([]OAuthConsent, error) {
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

func (c *ApiClient) RevokeOAuthConsent(clientID string) error {
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
