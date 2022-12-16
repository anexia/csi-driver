package v1

import (
	"strings"
)

// mapPointerSlice transforms the values of the provided pointer slice `in`
// using the provided function `f`, but returns nil if the provided `in` argument
// is nil.
func mapPointerSlice[T, U any](f func(T) U, in *[]T) *[]U {
	if in == nil {
		return nil
	}
	out := make([]U, 0, len(*in))
	for _, v := range *in {
		out = append(out, f(v))
	}
	return &out
}

// joinPointerString is similar to strings.Join, but instead of
// a slice of strings it expects and returns a pointer to a slice
// of strings. It returns nil if the input is nil.
func joinPointerString(in *[]string, sep string) *string {
	if in == nil {
		return nil
	}
	out := strings.Join(*in, sep)
	return &out
}
