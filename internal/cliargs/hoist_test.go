package cliargs

import (
	"reflect"
	"testing"
)

func TestHoistGlobalFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "flag after subcommand is hoisted",
			in:   []string{"createos", "sandbox", "create", "--output", "json"},
			want: []string{"createos", "--output", "json", "sandbox", "create"},
		},
		{
			name: "inline value is hoisted as one token",
			in:   []string{"createos", "sandbox", "list", "--output=json"},
			want: []string{"createos", "--output=json", "sandbox", "list"},
		},
		{
			name: "bool flag and alias",
			in:   []string{"createos", "sandbox", "get", "box", "-d", "-o", "json"},
			want: []string{"createos", "-d", "-o", "json", "sandbox", "get", "box"},
		},
		{
			name: "already-global order is preserved",
			in:   []string{"createos", "--output", "json", "sandbox", "list"},
			want: []string{"createos", "--output", "json", "sandbox", "list"},
		},
		{
			name: "nothing to hoist returns input untouched",
			in:   []string{"createos", "sandbox", "list"},
			want: []string{"createos", "sandbox", "list"},
		},
		{
			name: "tokens after -- are never hoisted",
			in:   []string{"createos", "sandbox", "exec", "box", "--", "./ci.sh", "--debug", "--output", "json"},
			want: []string{"createos", "sandbox", "exec", "box", "--", "./ci.sh", "--debug", "--output", "json"},
		},
		{
			name: "global before -- is hoisted, passthrough after is not",
			in:   []string{"createos", "sandbox", "exec", "box", "--output", "json", "--", "env", "-d"},
			want: []string{"createos", "--output", "json", "sandbox", "exec", "box", "--", "env", "-d"},
		},
		{
			name: "subcommand flags keep their own values",
			in:   []string{"createos", "sandbox", "sync", "box", "--exclude", "*.log", "--output", "json"},
			want: []string{"createos", "--output", "json", "sandbox", "sync", "box", "--exclude", "*.log"},
		},
		{
			name: "trailing value-less string flag does not panic",
			in:   []string{"createos", "sandbox", "list", "--output"},
			want: []string{"createos", "--output", "sandbox", "list"},
		},
		{
			name: "bare program name",
			in:   []string{"createos"},
			want: []string{"createos"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Hoist(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Hoist(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
