package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Read executable metadata instead of launching a GUI browser for --version.
func installedBrowserVersion(path string) string {
	var handle windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &handle)
	if err != nil || size == 0 {
		return ""
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return ""
	}
	var info *windows.VS_FIXEDFILEINFO
	var length uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), `\`, unsafe.Pointer(&info), &length); err != nil || info == nil || length < uint32(unsafe.Sizeof(*info)) {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", info.FileVersionMS>>16, info.FileVersionMS&0xffff, info.FileVersionLS>>16, info.FileVersionLS&0xffff)
}
