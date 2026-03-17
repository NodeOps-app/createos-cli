package api

import (
	"fmt"
	"log"
	"strings"

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
		SetHeader("Content-Type", "application/json")

	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  token,
			masked: maskToken(token),
		})
	}

	return ApiClient{Client: client}
}

// NewClientWithAccessToken creates a resty client authenticated with an OAuth access token.
// Uses X-Access-Token header instead of x-api-key.
func NewClientWithAccessToken(accessToken, apiURL string, debug bool) ApiClient {
	if apiURL == "" {
		apiURL = DefaultBaseURL
	}

	client := resty.New().
		SetBaseURL(apiURL).
		SetHeader("X-Access-Token", accessToken).
		SetHeader("Content-Type", "application/json")

	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  accessToken,
			masked: maskToken(accessToken),
		})
	}

	return ApiClient{Client: client}
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
