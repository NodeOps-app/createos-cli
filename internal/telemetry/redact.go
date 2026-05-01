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
	if isSensitiveName(name) {
		return redactedSentinel
	}
	return value
}

// isSensitiveName reports whether a flag name (canonical or alias) matches
// any deny keyword via case-insensitive substring match.
func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range denyKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// findFlagByName walks c.Lineage() (child→root) and returns the cli.Flag whose
// Names() include name (canonical or alias). Returns nil when not found.
func findFlagByName(c *cli.Context, name string) cli.Flag {
	if c == nil {
		return nil
	}
	for _, ctx := range c.Lineage() {
		if ctx.Command != nil {
			for _, f := range ctx.Command.Flags {
				for _, n := range f.Names() {
					if n == name {
						return f
					}
				}
			}
		}
		if ctx.App != nil {
			for _, f := range ctx.App.Flags {
				for _, n := range f.Names() {
					if n == name {
						return f
					}
				}
			}
		}
	}
	return nil
}

// anyAliasSensitive returns true when any of the flag's Names()
// (canonical + aliases) matches the denylist.
func anyAliasSensitive(f cli.Flag) bool {
	if f == nil {
		return false
	}
	for _, n := range f.Names() {
		if isSensitiveName(n) {
			return true
		}
	}
	return false
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
		// Canonicalize: redact when ANY alias of this flag matches the
		// denylist, not just the user-supplied alias. Example: `login -t <T>`
		// reports name="t" via LocalFlagNames; the canonical "token" matches.
		if anyAliasSensitive(findFlagByName(c, name)) {
			out[name] = redactedSentinel
			continue
		}
		out[name] = RedactFlagValue(name, v)
	}
	return out
}
