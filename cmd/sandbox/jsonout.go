package sandbox

import (
	"encoding/json"
	"maps"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/output"
)

// renderResult is how every sandbox mutation reports what it did.
//
// In JSON mode it writes one object to stdout; otherwise it runs the
// human renderer untouched. The "action" field names the operation that
// completed, so a caller reading a stream of results can tell a pause from
// a resume without tracking which command it invoked.
//
// Keys are shared with the read commands on purpose — a caller can diff the
// object `create` returned against the one `get` returns later.
func renderResult(c *cli.Context, action string, fields map[string]any, human func()) {
	obj := make(map[string]any, len(fields)+1)
	obj["action"] = action
	maps.Copy(obj, fields)
	output.Render(c, obj, human)
}

// withResponse folds an API response's own JSON fields into a result object,
// so a caller gets everything the server returned (vcpu, mem_mib, egress,
// quotas…) as well as the action and the convenience keys. Curated keys win
// on collision — those are the documented, stable ones.
//
// This keeps the whole-payload behaviour of the earlier per-command JSON
// branches while routing every command through one code path.
func withResponse(resp any, fields map[string]any) map[string]any {
	raw, err := json.Marshal(resp)
	if err != nil {
		return fields
	}
	var flat map[string]any
	if err := json.Unmarshal(raw, &flat); err != nil {
		return fields
	}
	maps.Copy(flat, fields)
	return flat
}

// str dereferences an optional API string field. The sandbox API returns
// null for anything not yet assigned (a name, an IP on a still-creating
// box), and JSON consumers are better served by "" than by a missing key.
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
