//go:build linux && !nominer

package main

// Miner tests against the installed package; see linux_integration_test.go.

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func minerProcesses(p paths) []int {
	var out []int
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if exe, err := os.Readlink("/proc/" + e.Name() + "/exe"); err == nil && exe == p.miner {
			out = append(out, pid)
		}
	}
	return out
}

// runHelperMiner is a stand-in tray (re-executed test binary) that runs one miner
// until it is killed.
func init() { helperMiner = runHelperMiner }

func runHelperMiner() {
	p := pathsAt(os.Getenv("H_DATA"), os.Getenv("H_DATA"), os.Getenv("H_DATA"), os.Getenv("H_DATA"), installedBin)
	extraMinerArgs = []string{"--pool", os.Getenv("H_POOL"), "--light", "--api=false"}
	m := newMinerSup(p)
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "ci", Power: 25})
	select {}
}

// The miner must not outlive the tray: killed with SIGKILL (no cleanup runs),
// the tray takes the miner with it.
func TestLinuxMinerDiesWithTray(t *testing.T) {
	p := installedPaths(t)
	if !minerAvailable || !exists(p.miner) {
		t.Skip("no core-miner in the package")
	}
	pool, _ := acceptAllPool(t)
	self, _ := os.Executable()
	h := exec.Command(self, "-test.run=^$")
	h.Env = append(os.Environ(), "CORE_STARTER_TEST_HELPER=miner", "H_DATA="+p.root, "H_POOL="+pool)
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	var miners []int
	deadline := time.Now().Add(time.Minute)
	for len(miners) == 0 {
		if time.Now().After(deadline) {
			h.Process.Kill()
			t.Fatal("helper never started a miner")
		}
		time.Sleep(200 * time.Millisecond)
		miners = minerProcesses(p)
	}
	// Still running a while later: the starting goroutine's end did not kill it.
	time.Sleep(3 * time.Second)
	if len(minerProcesses(p)) == 0 {
		t.Fatal("miner died although the tray still runs")
	}
	h.Process.Kill()
	h.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for len(minerProcesses(p)) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("miner %v still running 3 s after the tray was killed", miners)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestLinuxMinerSupervisor(t *testing.T) {
	p := installedPaths(t)
	if !minerAvailable || !exists(p.miner) {
		t.Skip("no core-miner in the package")
	}
	pool, accepted := acceptAllPool(t)
	extraMinerArgs = []string{"--pool", pool, "--light", "--api=false"}
	defer func() { extraMinerArgs = nil }()
	m := newMinerSup(p)
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "ci", Power: 50})
	deadline := time.Now().Add(3 * time.Minute)
	for {
		on, _, st, failure := m.View()
		if failure != "" {
			t.Fatalf("miner failed: %s", failure)
		}
		if on && st != nil && st.State == "mining" && st.Accepted >= 2 && accepted.Load() >= 2 {
			t.Logf("status: %+v", *st)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not mining after 3 minutes: %+v", st)
		}
		time.Sleep(time.Second)
	}
	// Idle scheduling on the miner's threads (the Starter's only priority knob).
	for _, pid := range minerProcesses(p) {
		if attr, err := unix.SchedGetAttr(pid, 0); err == nil && attr.Policy != unix.SCHED_IDLE {
			t.Errorf("miner pid %d policy %d, want SCHED_IDLE", pid, attr.Policy)
		}
	}
	m.Apply(nil)
	if n := len(minerProcesses(p)); n != 0 {
		t.Fatalf("%d core-miner still running", n)
	}
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4964", Worker: "ci", Power: 25})
	waitFailure(t, m)
	m.Shutdown()
}
