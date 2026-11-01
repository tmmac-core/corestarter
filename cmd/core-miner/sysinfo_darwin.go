//go:build darwin

package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>
#include <libkern/OSThermalNotification.h>
#include <notify.h>
#include <mach/mach.h>
#include <pthread/qos.h>

// onAC: 1 on mains, 0 on battery, -1 unknown (desktop Macs report mains).
static int onAC(void) {
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (!info) return -1;
	CFStringRef src = IOPSGetProvidingPowerSourceType(info);
	int r = src ? CFStringCompare(src, CFSTR(kIOPMACPowerKey), 0) == kCFCompareEqualTo : -1;
	CFRelease(info);
	return r;
}

// reclaimable: free, inactive, speculative and purgeable pages in bytes, what
// macOS hands out without compressing or swapping.
static unsigned long long reclaimable(void) {
	vm_statistics64_data_t vs;
	mach_msg_type_number_t n = HOST_VM_INFO64_COUNT;
	if (host_statistics64(mach_host_self(), HOST_VM_INFO64, (host_info64_t)&vs, &n) != KERN_SUCCESS) return 0;
	unsigned long long pages = (unsigned long long)vs.free_count + vs.inactive_count + vs.speculative_count + vs.purgeable_count;
	return pages * vm_kernel_page_size;
}

// thermalLevel: macOS's thermal pressure (notify(3), the level behind
// ProcessInfo.thermalState): 0 nominal, 1 moderate, 2 heavy, 3 trapping, 4
// sleeping; -1 unknown.
static int thermalLevel(void) {
	static int token = -1;
	if (token == -1 && notify_register_check(kOSThermalNotificationPressureLevelName, &token) != NOTIFY_STATUS_OK) {
		token = -1;
		return -1;
	}
	uint64_t level;
	if (notify_get_state(token, &level) != NOTIFY_STATUS_OK) return -1;
	return (int)level;
}

static void threadQoS(int utility) {
	pthread_set_qos_class_self_np(utility ? QOS_CLASS_UTILITY : QOS_CLASS_DEFAULT, 0);
}
*/
import "C"

import (
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// maxThreads: every core, performance and efficiency alike (on an M5 the six
// efficiency cores add about as much as the four performance cores).
func maxThreads() int { return max(1, runtime.NumCPU()) }

// availableMemory: memory macOS can hand out without pressure. Compressed
// memory is not counted, so this is on the safe side.
func availableMemory() uint64 { return uint64(C.reclaimable()) }

// onBattery: a MacBook runs on its battery. Unknown counts as mains.
func onBattery() bool { return C.onAC() == 0 }

// tooHot: macOS already throttles hard (thermal pressure heavy or worse). A
// MacBook Air has no fan; mining on would only keep it there.
func tooHot() bool { return C.thermalLevel() >= C.kOSThermalPressureLevelHeavy }

// minerPriority: nice 19 for the process, so anything the user does comes
// first. Not the background QoS class: macOS runs those threads on the
// efficiency cores only, which would halve the hash rate.
func minerPriority() { setIdlePriority(true) }

// setIdlePriority sets nice 19 (or 0) for the process and the utility QoS
// class for the calling thread; hashing threads call workerThread.
func setIdlePriority(idle bool) {
	nice := 0
	if idle {
		nice = 19
	}
	unix.Setpriority(unix.PRIO_PROCESS, 0, nice)
	C.threadQoS(boolInt(idle))
	idleThreads = idle
}

var idleThreads bool

// workerThread runs on each locked hashing thread: QoS is per thread on macOS.
func workerThread() {
	if idleThreads {
		C.threadQoS(1)
	}
}

func boolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// pinThread: macOS has no thread affinity for user programs.
func pinThread(int) {}

// watchParent ends the miner when the helper that started it ends (macOS has
// no Pdeathsig). A parent that is already gone (re-parented to launchd) ends
// it at once. stdin EOF stays the first guard.
func watchParent() {
	ppid := os.Getppid()
	kq, err := unix.Kqueue()
	if err != nil {
		return
	}
	ev := unix.Kevent_t{Ident: uint64(ppid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil || ppid == 1 {
		os.Exit(0)
	}
	out := make([]unix.Kevent_t, 1)
	for {
		if _, err := unix.Kevent(kq, nil, out, nil); err == nil || err != unix.EINTR {
			os.Exit(0)
		}
	}
}
