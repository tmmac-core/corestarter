//go:build windows && !nominer

package main

import (
	"slices"
	"testing"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

// The tray's miner supervisor with the real core-miner.exe from the payload.
func TestWindowsMinerSupervisor(t *testing.T) {
	integration(t)
	p := installedPaths(t)
	if !slices.ContainsFunc(payloadFiles, func(f payloadFile) bool { return f.Name == "core-miner.exe" }) {
		t.Skip("payload has no core-miner.exe")
	}
	if exists(p.miner) {
		t.Fatal("core-miner.exe installed before mining was switched on")
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

	// Throttle (node's first sync) and back, without a restart.
	_, _, before, _ := m.View()
	m.SetThrottle(true)
	waitView(t, m, func(on, throttled bool, threads int) bool {
		return throttled && threads == minercore.ThreadsFor(minerThrottlePct, before.MaxThreads)
	})
	m.SetThrottle(false)

	// A power change goes to the running miner; switching off stops it.
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963", Worker: "ci", Power: 100})
	waitView(t, m, func(on, _ bool, threads int) bool { return threads == before.MaxThreads })
	m.Apply(nil)
	if on, _, _, _ := m.View(); on {
		t.Fatal("still on after Apply(nil)")
	}
	if n := countProcesses("core-miner.exe"); n != 0 {
		t.Fatalf("%d core-miner.exe still running", n)
	}

	// A rejected address is a permanent failure, not a restart loop.
	m.Apply(&minerConfig{Address: "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4964", Worker: "ci", Power: 25})
	waitFailure(t, m)
	m.Shutdown()
}
