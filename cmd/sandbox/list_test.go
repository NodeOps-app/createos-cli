package sandbox

import (
	"testing"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func TestSandboxListTableBreakpoints(t *testing.T) {
	name := "demo"
	ip := "10.0.0.1"
	rows := []api.SandboxView{{
		ID:        "sb-01m0example",
		Name:      &name,
		Status:    "running",
		Shape:     "s-1vcpu-1gb",
		IP:        &ip,
		CreatedAt: time.Date(2026, 8, 25, 17, 59, 0, 0, time.UTC),
	}}

	tests := []struct {
		name    string
		width   int
		wide    bool
		headers []string
	}{
		{name: "compact", width: 69, headers: []string{"Name", "Status", "Size"}},
		{name: "medium", width: 89, headers: []string{"ID", "Name", "Status", "Size"}},
		{name: "default", width: 120, headers: []string{"ID", "Name", "Status", "Size", "IP"}},
		{name: "wide", width: 120, wide: true, headers: []string{"ID", "Name", "Status", "Size", "IP", "Created"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := sandboxListTable(rows, tt.width, tt.wide)
			if len(table) == 0 {
				t.Fatal("expected table data")
			}
			if len(table[0]) != len(tt.headers) {
				t.Fatalf("header count = %d, want %d: %#v", len(table[0]), len(tt.headers), table[0])
			}
			for i, want := range tt.headers {
				if table[0][i] != want {
					t.Fatalf("header[%d] = %q, want %q; headers = %#v", i, table[0][i], want, table[0])
				}
			}
		})
	}
}
