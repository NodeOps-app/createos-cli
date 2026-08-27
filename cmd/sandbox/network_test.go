package sandbox

import (
	"reflect"
	"testing"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func TestLooksLikeSandboxRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "sb-01m10y7j0qgphydk8awvmnbza3", want: true},
		{ref: "sb_01m10y7j0qgphydk8awvmnbza3", want: true},
		{ref: "bhautikin", want: false},
		{ref: "dev-01m10y7j0qgphydk8awvmnbza3", want: false},
	}
	for _, tt := range tests {
		if got := looksLikeSandboxRef(tt.ref); got != tt.want {
			t.Fatalf("looksLikeSandboxRef(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

func TestNetworkMemberEndpointOptionsOnlyIncludesAttachedMembers(t *testing.T) {
	t.Parallel()

	network := &api.SandboxNetwork{
		ID:   "net-123",
		Name: "bhautikin",
		Members: []api.SandboxNetworkMember{{
			SandboxID: "sb-1",
			Name:      "app",
			Status:    "running",
			IP:        "10.0.0.4",
		}},
	}
	devs := []api.DeviceView{
		{ID: "dev-1", Name: "laptop", ClientIP: "100.64.0.8"},
		{ID: "dev-2", Name: "desktop", ClientIP: "100.64.0.9"},
	}
	deviceNetworks := map[string][]api.DeviceNetworkAttachmentView{
		"dev-1": {{NetworkID: "net-123", NetworkName: "bhautikin"}},
		"dev-2": {{NetworkID: "net-other", NetworkName: "other"}},
	}

	options, refs := networkMemberEndpointOptions(network, devs, deviceNetworks)
	wantOptions := []string{
		"sandbox: app   (id: sb-1, status: running, ip: 10.0.0.4)",
		"device:  laptop   (100.64.0.8, id: dev-1)",
	}
	if !reflect.DeepEqual(options, wantOptions) {
		t.Fatalf("options = %#v, want %#v", options, wantOptions)
	}
	if refs[options[0]] != "sb-1" {
		t.Fatalf("sandbox ref = %q", refs[options[0]])
	}
	if refs[options[1]] != "dev-1" {
		t.Fatalf("device ref = %q", refs[options[1]])
	}
}

func TestDeviceAttachedToNetworkMatchesNameOrID(t *testing.T) {
	t.Parallel()

	network := &api.SandboxNetwork{ID: "net-123", Name: "bhautikin"}
	if !deviceAttachedToNetwork(network, []api.DeviceNetworkAttachmentView{{NetworkID: "net-123"}}) {
		t.Fatal("expected ID match")
	}
	if !deviceAttachedToNetwork(network, []api.DeviceNetworkAttachmentView{{NetworkName: "bhautikin"}}) {
		t.Fatal("expected name match")
	}
	if deviceAttachedToNetwork(network, []api.DeviceNetworkAttachmentView{{NetworkID: "net-other", NetworkName: "other"}}) {
		t.Fatal("unexpected match")
	}
}
