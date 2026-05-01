package telemetry

import (
	"net/url"
	"strings"

	"github.com/urfave/cli/v2"
)

// denyKeywords are case-insensitive substrings that mark a flag value as
// secret. Match → redact.
var denyKeywords = []string{
	"token", "password", "passwd", "secret", "key", "credential", "bearer", "auth",
}

// redactedSentinel is the placeholder substituted for secret-flag values.
const redactedSentinel = "[REDACTED]"

// RedactFlagValue returns the original value or the sentinel when the flag
// name matches any deny keyword (case-insensitive substring match).
func RedactFlagValue(name string, value any) any {
	lower := strings.ToLower(name)
	for _, kw := range denyKeywords {
		if strings.Contains(lower, kw) {
			return redactedSentinel
		}
	}
	return value
}

// NormalizeAPIURL strips path/query/fragment, returning "scheme://host" only.
// Returns "" when the input cannot be parsed or has no host.
func NormalizeAPIURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + u.Host
}

// FlagsFromContext extracts the locally-set flags on a cli.Context as a
// redacted map suitable for telemetry.
func FlagsFromContext(c *cli.Context) map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{}
	for _, name := range c.LocalFlagNames() {
		v := c.Value(name)
		if name == "api-url" {
			if s, ok := v.(string); ok {
				out[name] = NormalizeAPIURL(s)
				continue
			}
		}
		out[name] = RedactFlagValue(name, v)
	}
	return out
}
