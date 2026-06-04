// Package utils provides utility functions for the createos-cli project.
package utils //nolint:revive // shared helper package; name is intentional and widely imported

// Ptr is a generic helper function that takes a value of any type and returns a pointer to it.
func Ptr[T any](v T) *T {
	return &v
}
