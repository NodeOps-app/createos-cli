package sandbox

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSizeBytes accepts "5GB", "500MB", "1024" etc. (decimal SI units
// — 1 KB = 1000 bytes) plus binary suffixes (KiB/MiB/GiB/TiB). Pure
// digits are treated as raw bytes. Used by `sandbox edit`'s bandwidth
// top-up step and any future caller that needs human size parsing.
func parseSizeBytes(in string) (int64, error) {
	s := strings.TrimSpace(strings.ToUpper(in))
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	type unit struct {
		suffix string
		mul    int64
	}
	for _, u := range []unit{
		{"TIB", 1 << 40},
		{"GIB", 1 << 30},
		{"MIB", 1 << 20},
		{"KIB", 1 << 10},
		{"TB", 1_000_000_000_000},
		{"GB", 1_000_000_000},
		{"MB", 1_000_000},
		{"KB", 1_000},
		{"T", 1_000_000_000_000},
		{"G", 1_000_000_000},
		{"M", 1_000_000},
		{"K", 1_000},
		{"B", 1},
	} {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("could not read %q as a number", num)
			}
			return int64(f * float64(u.mul)), nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not read %q — try a value like 5GB or a raw byte count", in)
	}
	return n, nil
}
