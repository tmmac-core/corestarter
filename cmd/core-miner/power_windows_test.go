//go:build windows

package main

import (
	"runtime"
	"testing"
)

// TestPowerThrottling: Windows accepts every mode for the process and a
// thread, and minerPriority reports the opt-out.
func TestPowerThrottling(t *testing.T) {
	defer setProcessPower(powerFull)
	want := map[powerMode]powerThrottlingState{
		powerFull:   {version: 1, controlMask: 1, stateMask: 0},
		powerSystem: {version: 1, controlMask: 0, stateMask: 0},
		powerEco:    {version: 1, controlMask: 1, stateMask: 1},
	}
	// The thread calls run on a locked goroutine that ends without unlocking:
	// Go then retires its OS thread, so no tagged thread serves later tests.
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		for m, s := range want {
			if got := m.state(); got != s {
				t.Errorf("mode %d: state %+v, want %+v", m, got, s)
			}
			if err := setProcessPower(m); err != nil {
				t.Errorf("process, mode %d: %v", m, err)
			}
			if err := setThreadPower(m); err != nil {
				t.Errorf("thread, mode %d: %v", m, err)
			}
		}
	}()
	<-done
	minerPriority()
	if powerNote != "Windows power throttling off" {
		t.Errorf("powerNote %q", powerNote)
	}
}
