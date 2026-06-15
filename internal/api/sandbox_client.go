package api

import (
	"github.com/go-resty/resty/v2"
)

// DefaultSandboxBaseURL is the default fc-spawn API base URL. The
// sandbox surface lives on a different host from the main CreateOS API
// (api-createos.nodeops.network); these two clients are wired
// side-by-side under app.Metadata.
const DefaultSandboxBaseURL = "https://api.sb.createos.sh"

// SandboxClient wraps a resty.Client configured for the fc-spawn API.
// Mirrors APIClient but targets the sandbox base URL and uses
// X-Api-Key as the auth header (fc-spawn's preferred header — Bearer
// is not accepted on user-facing routes).
type SandboxClient struct {
	Client *resty.Client
	// authHeader is the header name this client sends its credential
	// under (X-Api-Key for an api key, X-Access-Token for an OAuth JWT).
	authHeader string
}

// AuthHeader returns the header name and the current token this client
// authenticates with. The hand-rolled HTTP-Upgrade streaming paths
// (sandbox shell PTY, tunnel, sync) reuse this so they send the same
// header and the same (refresh-rotated) token as every other sandbox
// call, instead of re-deriving credentials and hardcoding X-Api-Key.
func (c *SandboxClient) AuthHeader() (header, token string) {
	return c.authHeader, c.Client.Header.Get(c.authHeader)
}

// NewSandboxClient builds a SandboxClient for API-key auth. Empty url
// falls back to DefaultSandboxBaseURL. The api key is sent as X-Api-Key
// — fc-spawn validates it against the same upstream NodeOps auth service.
//
// For OAuth/browser logins the credential is an access-token JWT, NOT an
// api key: fc-spawn rejects it under X-Api-Key ("invalid api key") and
// requires the X-Access-Token header instead. Use
// NewSandboxClientWithAccessToken for that case.
func NewSandboxClient(token, sandboxURL string, debug bool) SandboxClient {
	return newSandboxClient(headerAPIKey, token, sandboxURL, debug, nil)
}

// NewSandboxClientWithAccessToken builds a SandboxClient authenticated
// with an OAuth access token, sent via the X-Access-Token header. This
// mirrors NewClientWithAccessToken on the main API client — fc-spawn
// accepts the same token under this header. When refresher is non-nil
// the client refreshes the token and retries once on a 401.
func NewSandboxClientWithAccessToken(accessToken, sandboxURL string, debug bool, refresher TokenRefresher) SandboxClient {
	return newSandboxClient(headerAccessToken, accessToken, sandboxURL, debug, refresher)
}

// newSandboxClient is the shared builder behind the two auth schemes.
func newSandboxClient(authHeader, token, sandboxURL string, debug bool, refresher TokenRefresher) SandboxClient {
	if sandboxURL == "" {
		sandboxURL = DefaultSandboxBaseURL
	}
	client := resty.New().
		SetBaseURL(sandboxURL).
		SetHeader(authHeader, token).
		SetHeader("Content-Type", "application/json")
	if debug {
		client.SetDebug(true)
		client.SetLogger(&maskingLogger{
			token:  token,
			masked: maskToken(token),
		})
	}
	installAuthRefresh(client, authHeader, refresher)
	return SandboxClient{Client: client, authHeader: authHeader}
}

// SandboxClientKey is the cli.Context metadata key for the sandbox client.
const SandboxClientKey = "sandbox_client" // #nosec G101 -- context metadata key, not a credential  // pragma: allowlist secret
