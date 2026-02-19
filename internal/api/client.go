package api

import (
	"github.com/go-resty/resty/v2"
)

const DefaultBaseURL = "https://api-createos.nodeops.network"

type ApiClient struct {
	Client *resty.Client
}

// NewClient creates a resty client with the token, base URL and debug flag set
func NewClient(token, apiURL string, debug bool) ApiClient {
	if apiURL == "" {
		apiURL = DefaultBaseURL
	}

	client := resty.New().
		SetBaseURL(apiURL).
		SetHeader("x-api-key", token).
		SetHeader("Content-Type", "application/json").
		SetDebug(debug)

	return ApiClient{Client: client}
}

// ClientKey is the key used to store the resty client in cli.Context metadata
const ClientKey = "api_client"
