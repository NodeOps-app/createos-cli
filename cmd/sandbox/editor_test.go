package sandbox

import (
	"strings"
	"testing"
)

// Both transports must pin known_hosts to the sandbox id. Tunnel mode shares
// 127.0.0.1:22 across every box and VPN mode recycles overlay IPs, so without
// the pin the second sandbox reads as a changed host key.
func TestRenderSSHBlockPinsHostKeyAliasToSandboxID(t *testing.T) {
	for _, mode := range []string{"tunnel", "vpn"} {
		t.Run(mode, func(t *testing.T) {
			block, err := renderSSHBlock(
				"sb-aaa", mode, "sb-aaa", "10.0.0.7",
				"gateway.sb.createos.sh", 2222, "root", "/keys/sb-aaa", "my-box",
			)
			if err != nil {
				t.Fatalf("renderSSHBlock: %v", err)
			}
			if !strings.Contains(block, "HostKeyAlias      sb-aaa") {
				t.Errorf("block is missing the HostKeyAlias pin:\n%s", block)
			}
		})
	}
}

func TestRenderSSHBlockGivesEachSandboxADistinctHostKeyIdentity(t *testing.T) {
	render := func(id string) string {
		block, err := renderSSHBlock(
			id, "tunnel", id, "10.0.0.7",
			"gateway.sb.createos.sh", 2222, "root", "/keys/"+id, "",
		)
		if err != nil {
			t.Fatalf("renderSSHBlock(%s): %v", id, err)
		}
		return block
	}
	if aliasLine(render("sb-aaa")) == aliasLine(render("sb-bbb")) {
		t.Error("two sandboxes share one host-key identity; the second will trip a changed-host-key error")
	}
}

func TestRenderSSHBlockRejectsUnknownMode(t *testing.T) {
	if _, err := renderSSHBlock("sb-aaa", "carrier-pigeon", "sb-aaa", "10.0.0.7",
		"gateway.sb.createos.sh", 2222, "root", "/keys/sb-aaa", ""); err == nil {
		t.Error("expected an error for an unknown mode")
	}
}

func aliasLine(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "HostKeyAlias") {
			return trimmed
		}
	}
	return ""
}
