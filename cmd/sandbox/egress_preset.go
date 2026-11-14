package sandbox

import (
	"fmt"
	"sort"
	"strings"
)

// egressPresets name the hosts one toolchain needs to fetch its
// dependencies. A sandbox with no --egress reaches the whole internet;
// naming a preset is the cheap way to close that down to the registry the
// build actually uses, without anyone having to remember that cargo also
// pulls from static.rust-lang.org.
//
// Presets compose: --egress-preset npm --egress-preset github unions both
// lists, and --egress adds single hosts on top.
var egressPresets = map[string][]string{
	"python-uv": {
		"astral.sh", "releases.astral.sh", "pypi.org", "files.pythonhosted.org",
	},
	"rust-cargo": {
		"crates.io", "static.crates.io", "index.crates.io", "static.rust-lang.org", "cdn.pyke.io",
	},
	"npm": {
		"registry.npmjs.org",
	},
	"github": {
		"github.com", "objects.githubusercontent.com", "raw.githubusercontent.com", "codeload.github.com",
	},
}

// egressPresetNames lists the presets in a stable order, for help text
// and error messages.
func egressPresetNames() []string {
	names := make([]string, 0, len(egressPresets))
	for n := range egressPresets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resolveEgress expands preset names and merges them with explicit hosts.
// The result is deduplicated and sorted, so two invocations with the same
// intent produce the same allowlist.
//
// An empty result means "unrestricted", which is what the backend does
// with an empty list. Callers that want to warn about that check len().
func resolveEgress(presets, hosts []string) ([]string, error) {
	seen := make(map[string]struct{})
	for _, p := range presets {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		domains, ok := egressPresets[p]
		if !ok {
			return nil, fmt.Errorf("unknown egress preset %q\n\n  Available: %s",
				p, strings.Join(egressPresetNames(), ", "))
		}
		for _, d := range domains {
			seen[d] = struct{}{}
		}
	}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			seen[h] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}
