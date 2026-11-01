//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGlobalMemStatus = kernel32.NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	length, memoryLoad                   uint32
	totalPhys, availPhys                 uint64
	totalPageFile, availPageFile         uint64
	totalVirtual, availVirtual, availExt uint64
}

// totalMemory is the installed RAM in bytes, 0 if unknown.
func totalMemory() uint64 {
	m := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := procGlobalMemStatus.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0
	}
	return m.totalPhys
}

type rect struct{ left, top, right, bottom int32 }

// workArea is the primary monitor's desktop without the taskbar, in pixels
// (new windows open there).
func workArea() (rect, bool) {
	var r rect
	const spiGetWorkArea = 0x0030
	res, _, _ := procSystemParamsInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r, res != 0
}

var (
	procSystemParamsInfo = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")
	procSetWindowPos     = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowPos")
)

// onARM64 reports Windows on ARM, where this x64 program runs emulated.
func onARM64() bool {
	var process, native uint16
	const imageFileMachineARM64 = 0xAA64
	// Returns an error before Windows 10 1709 (no IsWow64Process2): not ARM64 then.
	return windows.IsWow64Process2(windows.CurrentProcess(), &process, &native) == nil && native == imageFileMachineARM64
}
