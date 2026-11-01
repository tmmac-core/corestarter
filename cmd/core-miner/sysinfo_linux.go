//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// maxThreads: every logical processor the process may use (see the Windows
// file for why hyper-threads count). NumCPU honours the affinity mask.
func maxThreads() int { return max(1, runtime.NumCPU()) }

// availableMemory in bytes (MemAvailable), 0 if unknown.
func availableMemory() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "MemAvailable:"); ok {
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			return n << 10
		}
	}
	return 0
}

// onBattery: a laptop runs on battery: no mains supply online and a battery
// discharging. Desktops (no battery) and unknown states count as mains.
func onBattery() bool {
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	discharging := false
	for _, d := range dirs {
		typ := readSys(filepath.Join(d, "type"))
		switch typ {
		case "Mains", "USB":
			if readSys(filepath.Join(d, "online")) == "1" {
				return false
			}
		case "Battery":
			// "scope" Device: a mouse or headset battery, not the computer's.
			if readSys(filepath.Join(d, "scope")) != "Device" && readSys(filepath.Join(d, "status")) == "Discharging" {
				discharging = true
			}
		}
	}
	return discharging
}

func readSys(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

// minerPriority: idle scheduling for every thread, so the miner only gets CPU
// time nothing else wants (Windows: IDLE_PRIORITY_CLASS from the Starter).
func minerPriority() { setIdlePriority(true) }

// setIdlePriority sets SCHED_IDLE and nice 19 (or back to normal) on all
// threads; threads created later inherit it from their creator. Going back
// to normal may be refused to an unprivileged user (RLIMIT_NICE).
func setIdlePriority(idle bool) {
	policy, nice := uint32(unix.SCHED_NORMAL), 0
	if idle {
		policy, nice = unix.SCHED_IDLE, 19
	}
	tasks, _ := os.ReadDir("/proc/self/task")
	for _, t := range tasks {
		tid, err := strconv.Atoi(t.Name())
		if err != nil {
			continue
		}
		unix.SchedSetAttr(tid, &unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: policy}, 0)
		unix.Setpriority(unix.PRIO_PROCESS, tid, nice)
	}
}

// pinThread binds the calling OS thread to logical processor i.
func pinThread(i int) {
	var set unix.CPUSet
	set.Set(i)
	unix.SchedSetaffinity(0, &set)
}
