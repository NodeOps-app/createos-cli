// Package utils provides utility functions for the createos-cli project.
package utils

// Ptr is a generic helper function that takes a value of any type and returns a pointer to it.
func Ptr[T any](v T) *T {
	return &v
}
