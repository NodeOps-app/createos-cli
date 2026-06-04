package api

import (
	"github.com/go-resty/resty/v2"
)

// DefaultSandboxBaseURL is the default fc-spawn API base URL. The
// sandbox surface lives on a different host from the main CreateOS API
// (api-createos.nodeops.network); these two clients are wired
// side-by-side under app.Metadata.
const DefaultSandboxBaseURL = "https://fc-spawn.bhautik.in"

// SandboxClient wraps a resty.Client configured for the fc-spawn API.
// Mirrors APIClient but targets the sandbox base URL and uses
// X-Api-Key as the auth header (fc-spawn's preferred header — Bearer
// is not accepted on user-facing routes).
type SandboxClient struct {
	Client *resty.Client
}

// NewSandboxClient builds a SandboxClient with the given token + URL.
// Empty url falls back to DefaultSandboxBaseURL. The same token used
// for the CreateOS API works here too — fc-spawn validates against the
// same upstream NodeOps auth service.
func NewSandboxClient(token, sandboxURL string, debug bool) SandboxClient {
	if sandboxURL == "" {
		sandboxURL = DefaultSandboxBaseURL
	}
	client := resty.New().
		SetBaseURL(sandboxURL).
		SetHeader("X-Api-Key", token).
		SetHeader("Content-Type", "application/json")
	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  token,
			masked: maskToken(token),
		})
	}
	return SandboxClient{Client: client}
}

// SandboxClientKey is the cli.Context metadata key for the sandbox client.
const SandboxClientKey = "sandbox_client"
