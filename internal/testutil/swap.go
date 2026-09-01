package testutil

import "testing"

func Swap[T any](t *testing.T, target *T, value T) {
	t.Helper()

	previous := *target
	*target = value
	t.Cleanup(func() { *target = previous })
}
