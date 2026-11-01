//go:build darwin && !nominer

package main

// Miner tests against the built app; see darwin_integration_test.go.

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func minerProcesses(p paths) []int {
	var out []int
	procs, _ := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	for _, kp := range procs {
		if exe, _, err := procArgs(int(kp.Proc.P_pid)); err == nil && exe == p.miner {
			out = append(out, int(kp.Proc.P_pid))
		}
	}
	return out
}

// runHelperMiner is a stand-in helper (re-executed test binary) that runs one
// miner until it is killed.
func init() { helperMiner = runHelperMiner }

func runHelperMiner() {
	p := pathsAt(os.Getenv("H_DATA"), os.Getenv("H_DATA"), os.Getenv("H_APP"))
	extraMinerArgs = []string{"--pool", os.Getenv("H_POOL"), "--light", "--api=false"}
	m := newMinerSup(p)
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "ci", Power: 25})
	select {}
}

// The miner must not outlive the helper: killed with SIGKILL (no cleanup
// runs), the miner follows (stdin EOF, and kqueue on its parent).
func TestDarwinMinerDiesWithHelper(t *testing.T) {
	p := installedPaths(t)
	if !minerAvailable || !exists(p.miner) {
		t.Skip("no core-miner in the app")
	}
	pool, _ := acceptAllPool(t)
	self, _ := os.Executable()
	h := exec.Command(self, "-test.run=^$")
	h.Env = append(os.Environ(), "CORE_STARTER_TEST_HELPER=miner", "H_DATA="+p.root, "H_POOL="+pool, "H_APP="+p.bin)
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
	time.Sleep(3 * time.Second)
	if len(minerProcesses(p)) == 0 {
		t.Fatal("miner died although the helper still runs")
	}
	h.Process.Kill()
	h.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for len(minerProcesses(p)) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("miner %v still running 3 s after the helper was killed", miners)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDarwinMinerSupervisor(t *testing.T) {
	p := installedPaths(t)
	if !minerAvailable || !exists(p.miner) {
		t.Skip("no core-miner in the app")
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
	// nice 19: whatever the user does comes first.
	for _, pid := range minerProcesses(p) {
		if n, err := unix.Getpriority(unix.PRIO_PROCESS, pid); err == nil && n != 19 {
			t.Errorf("miner pid %d nice %d, want 19", pid, n)
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
