//go:build windows

package player

import "golang.org/x/sys/windows"

// openSharedLib resolves a shared library by name via Windows LoadLibrary.
func openSharedLib(name string) (uintptr, error) {
	h, err := windows.LoadLibrary(name)
	if err != nil {
		return 0, err
	}
	return uintptr(h), nil
}
