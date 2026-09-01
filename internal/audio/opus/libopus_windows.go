//go:build windows

package opus

import "golang.org/x/sys/windows"

func openSharedLib(name string) (uintptr, error) {
	h, err := windows.LoadLibrary(name)
	if err != nil {
		return 0, err
	}
	return uintptr(h), nil
}
