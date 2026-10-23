package sandbox

import (
	"fmt"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"
)

func labelFlag() *cli.GenericFlag {
	return &cli.GenericFlag{Name: "label", Usage: "Sandbox label (repeatable): KEY=VALUE; list matches all labels, edit merges labels", Value: &labelFlagValues{}}
}

func parseLabelFlags(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	labels := make(map[string]string, len(raw))
	for _, entry := range raw {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--label %q must be KEY=VALUE with a non-empty key", entry)
		}
		if len(key) > 128 || len(value) > 1024 || strings.ContainsRune(entry, 0) {
			return nil, fmt.Errorf("--label keys must be at most 128 bytes and values at most 1024 bytes, without NUL bytes")
		}
		labels[key] = value
	}
	total := 0
	for key, value := range labels {
		total += len(key) + len(value) + 4
	}
	if len(labels) > 100 || total > 65536 {
		return nil, fmt.Errorf("labels must contain at most 100 pairs and 65536 bytes in total")
	}
	return labels, nil
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, key+"="+labels[key])
	}
	return strings.Join(entries, ", ")
}

// Keep each occurrence intact, including commas and whitespace in values.
type labelFlagValues struct{ values []string }

func (v *labelFlagValues) Set(raw string) error {
	v.values = append(v.values, raw)
	return nil
}
func (v *labelFlagValues) String() string { return strings.Join(v.values, ",") }
func labelFlagsFromContext(c *cli.Context) []string {
	v, ok := c.Generic("label").(*labelFlagValues)
	if !ok || v == nil {
		return nil
	}
	return append([]string(nil), v.values...)
}
