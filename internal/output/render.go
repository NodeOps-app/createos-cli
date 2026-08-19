// Package output provides helpers for rendering CLI output in different formats.
package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// FormatKey is the metadata key for the output format.
const FormatKey = "output_format"

// FormatExplicitKey records whether the format came from --output rather
// than from TTY detection.
const FormatExplicitKey = "output_format_explicit"

// IsJSONExplicit reports whether the user actually asked for JSON with
// --output json. Commands whose stdout IS the payload — `sandbox exec`
// forwarding a program's own output — must use this instead of IsJSON, or
// a plain `… exec box -- cat data.csv > out` would silently write a JSON
// envelope instead of the file the caller expected.
func IsJSONExplicit(c *cli.Context) bool {
	explicit, ok := c.App.Metadata[FormatExplicitKey].(bool)
	return ok && explicit && IsJSON(c)
}

// IsJSON returns true if the output format is JSON.
func IsJSON(c *cli.Context) bool {
	return AppIsJSON(c.App)
}

// AppIsJSON reports whether the app is in JSON mode. It reads the same
// metadata IsJSON does, but works from the *cli.App alone — main.go handles
// errors after Run returns, where no *cli.Context is available.
func AppIsJSON(app *cli.App) bool {
	if app == nil {
		return false
	}
	if f, ok := app.Metadata[FormatKey].(string); ok {
		return f == "json"
	}
	return false
}

// Render outputs data as JSON if --output json is set, otherwise calls the table renderer.
func Render(c *cli.Context, data any, tableRenderer func()) {
	if IsJSON(c) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}
	tableRenderer()
}

// RenderError outputs an error as JSON if --output json is set, otherwise returns false.
func RenderError(c *cli.Context, code string, message string, hint string) bool {
	return AppRenderError(c.App, code, message, hint)
}

// AppRenderError is RenderError for callers that only hold the *cli.App.
// The envelope goes to stdout so a JSON consumer reading one stream still
// gets valid JSON on failure; the human-readable path in main.go writes to
// stderr instead.
//
// hint carries the same next-step suggestion humans get (APIError.Hint) and
// is omitted when empty — an agent relaying a failure to a user should be
// able to pass on the same advice.
func AppRenderError(app *cli.App, code string, message string, hint string) bool {
	if !AppIsJSON(app) {
		return false
	}
	body := map[string]string{
		"code":    code,
		"message": message,
	}
	if hint != "" {
		body["hint"] = hint
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"error": body}); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return true
}

// DetectFormat returns "json" if --output flag is "json" or if stdout is not a TTY.
func DetectFormat(c *cli.Context) string {
	explicit := c.String("output")
	if explicit != "" {
		return explicit
	}
	if !terminal.IsInteractive() {
		return "json"
	}
	return "table"
}
