package sandbox

import "testing"

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
