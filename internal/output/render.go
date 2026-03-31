// Package output provides helpers for rendering CLI output in different formats.
package output

import (
	"encoding/json"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// FormatKey is the metadata key for the output format.
const FormatKey = "output_format"

// IsJSON returns true if the output format is JSON.
func IsJSON(c *cli.Context) bool {
	if f, ok := c.App.Metadata[FormatKey].(string); ok {
		return f == "json"
	}
	return false
}

// Render outputs data as JSON if --output json is set, otherwise calls the table renderer.
func Render(c *cli.Context, data any, tableRenderer func()) {
	if IsJSON(c) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data)
		return
	}
	tableRenderer()
}

// RenderError outputs an error as JSON if --output json is set, otherwise returns false.
func RenderError(c *cli.Context, code string, message string) bool {
	if !IsJSON(c) {
		return false
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
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
