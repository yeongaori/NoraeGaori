//go:build !windows

package player

import "github.com/ebitengine/purego"

// openSharedLib resolves a shared library by name via dlopen with RTLD_NOW.
func openSharedLib(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
