//go:build windows

package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
	procGetCurrentProcess    = kernel32.NewProc("GetCurrentProcess")
	procSetPriorityClass     = kernel32.NewProc("SetPriorityClass")
	procGetCurrentThread     = kernel32.NewProc("GetCurrentThread")
	procSetThreadAffinity    = kernel32.NewProc("SetThreadAffinityMask")
)

// maxThreads: every logical processor. RandomY's scratchpad is 128 KiB per
// thread (RANDOMX_SCRATCHPAD_L3 in third_party/RandomY/src/configuration.h),
// not RandomX's 2 MiB: it fits the L2 cache, so hyper-threads add hashes
// instead of fighting over L3. NumCPU honours the process affinity.
func maxThreads() int { return max(1, runtime.NumCPU()) }

type memoryStatusEx struct {
	length, memoryLoad                   uint32
	totalPhys, availPhys                 uint64
	totalPageFile, availPageFile         uint64
	totalVirtual, availVirtual, availExt uint64
}

// availableMemory in bytes, 0 if unknown.
func availableMemory() uint64 {
	m := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0
	}
	return m.availPhys
}

// onBattery: a laptop runs on battery (AC line offline).
func onBattery() bool {
	var s struct {
		acLine, batteryFlag, batteryPercent, systemStatus uint8
		lifeTime, fullLifeTime                            uint32
	}
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return false
	}
	return s.acLine == 0
}

// setIdlePriority: the Starter runs the miner at idle priority; --bench
// switches between that and normal to measure the difference.
func setIdlePriority(idle bool) {
	class := uintptr(windows.NORMAL_PRIORITY_CLASS)
	if idle {
		class = windows.IDLE_PRIORITY_CLASS
	}
	h, _, _ := procGetCurrentProcess.Call()
	procSetPriorityClass.Call(h, class)
}

// pinThread binds the calling OS thread to logical processor i (first
// processor group only, i < 64).
func pinThread(i int) {
	if i < 0 || i >= 64 {
		return
	}
	h, _, _ := procGetCurrentThread.Call()
	procSetThreadAffinity.Call(h, uintptr(1)<<i)
}

// minerPriority: the Starter already starts the miner at idle priority.
func minerPriority() {}
