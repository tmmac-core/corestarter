//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// SetProcessInformation / SetThreadInformation exist since Windows 8; the
// power throttling classes since Windows 10 1709. Older systems refuse them,
// which only costs the opt-out, not the mining.
var (
	procSetProcessInformation = kernel32.NewProc("SetProcessInformation")
	procSetThreadInformation  = kernel32.NewProc("SetThreadInformation")
)

const (
	processPowerThrottling = 4 // PROCESS_INFORMATION_CLASS ProcessPowerThrottling
	threadPowerThrottling  = 3 // THREAD_INFORMATION_CLASS ThreadPowerThrottling
	powerThrottlingVersion = 1 // PROCESS/THREAD_POWER_THROTTLING_CURRENT_VERSION
	throttleExecutionSpeed = 1 // PROCESS/THREAD_POWER_THROTTLING_EXECUTION_SPEED
)

// powerThrottlingState is PROCESS_POWER_THROTTLING_STATE and
// THREAD_POWER_THROTTLING_STATE (same layout).
type powerThrottlingState struct {
	version, controlMask, stateMask uint32
}

// state: ControlMask says which policy we decide ourselves, StateMask whether
// it is on. Execution speed decided and off = never throttled (high QoS);
// nothing decided = Windows' heuristics; decided and on = EcoQoS.
func (m powerMode) state() powerThrottlingState {
	s := powerThrottlingState{version: powerThrottlingVersion}
	switch m {
	case powerFull:
		s.controlMask = throttleExecutionSpeed
	case powerEco:
		s.controlMask, s.stateMask = throttleExecutionSpeed, throttleExecutionSpeed
	}
	return s
}

// setProcessPower applies the mode to the whole miner process.
func setProcessPower(m powerMode) error {
	if err := procSetProcessInformation.Find(); err != nil {
		return err
	}
	s := m.state()
	if r, _, err := procSetProcessInformation.Call(uintptr(windows.CurrentProcess()), processPowerThrottling,
		uintptr(unsafe.Pointer(&s)), unsafe.Sizeof(s)); r == 0 {
		return err
	}
	return nil
}

// setThreadPower applies the mode to the calling OS thread as well, so a
// hashing thread keeps it even if Windows decides per thread.
func setThreadPower(m powerMode) error {
	if err := procSetThreadInformation.Find(); err != nil {
		return err
	}
	s := m.state()
	if r, _, err := procSetThreadInformation.Call(uintptr(windows.CurrentThread()), threadPowerThrottling,
		uintptr(unsafe.Pointer(&s)), unsafe.Sizeof(s)); r == 0 {
		return err
	}
	return nil
}

// workerThread: every hashing thread (locked to its OS thread) applies
// threadPower to itself. A refusal is not logged per thread: the process-wide
// setting (minerPriority, logged once) still applies.
func workerThread() { _ = setThreadPower(threadPower) }

// watchParent: the job object ends the miner with the Starter; stdin EOF is
// the second guard.
func watchParent() {}
