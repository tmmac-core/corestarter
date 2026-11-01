package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTrackerInitialSync(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		st   NodeState
		snap *Snapshot
		want string
	}{
		{"starting", NodeStarting, nil, "Node startet"},
		{"no rpc yet", NodeRunning, nil, "Node startet"},
		{"no peers", NodeRunning, &Snapshot{}, "Suche andere Nodes"},
		{"peers, not started", NodeRunning, &Snapshot{Peers: 3}, "Warte auf Start"},
		{"blocks", NodeRunning, &Snapshot{Syncing: true, Current: 8817984, Highest: 19381426, Peers: 12},
			"Synchronisierung: 45 % (Block 8.817.984 von 19.381.426)"},
		{"never 100 while syncing", NodeRunning, &Snapshot{Syncing: true, Current: 19381425, Highest: 19381426, Peers: 1},
			"Synchronisierung: 99 %"},
		{"state phase", NodeRunning, &Snapshot{Syncing: true, Current: 19381000, Highest: 19381426, Pulled: 1234567, Known: 500, Peers: 5},
			"Lädt Kontostände (1.234.567 Einträge)"},
		{"failed", NodeFailed, nil, "abgestürzt"},
	}
	for _, c := range cases {
		var tr tracker
		got := tr.update(textsDE, c.st, 1, c.snap, false, now)
		if !strings.Contains(got.Line1, c.want) {
			t.Errorf("%s: Line1 = %q, want it to contain %q", c.name, got.Line1, c.want)
		}
		if got.Synced {
			t.Errorf("%s: must not be synced", c.name)
		}
		if utf8.RuneCountInString(got.Tooltip) > 127 {
			t.Errorf("%s: tooltip too long", c.name)
		}
	}
}

func TestTrackerAfterFirstSync(t *testing.T) {
	now := time.Now()
	var tr tracker
	// Right after a restart the node is not syncing yet: do not claim "up to date".
	s := tr.update(textsDE, NodeRunning, 7, &Snapshot{Head: 19000000, Peers: 4}, false, now)
	if s.Synced || !strings.Contains(s.Line1, "Prüft auf neue Blöcke") {
		t.Fatalf("fresh restart: %q", s.Line1)
	}
	// Catching up after the PC was off: persisted knownStates must not produce the state text.
	s = tr.update(textsDE, NodeRunning, 7, &Snapshot{Syncing: true, Head: 19000000, Starting: 19000000, Current: 19190000, Highest: 19381426, Known: 999999, Pulled: 999999, Peers: 8}, false, now)
	if s.Synced || !strings.Contains(s.Line1, "Holt auf: 49 %") || !strings.Contains(s.Line1, "191.426") {
		t.Fatalf("catching up: %q", s.Line1)
	}
	// Mini sync one block behind = up to date.
	s = tr.update(textsDE, NodeRunning, 7, &Snapshot{Syncing: true, Head: 19381425, Current: 19381425, Highest: 19381426, Known: 999999, Peers: 8}, false, now)
	if !s.Synced || !strings.Contains(s.Line1, "Auf dem neuesten Stand") {
		t.Fatalf("mini sync: %q", s.Line1)
	}
	// Stays up to date while idle.
	s = tr.update(textsDE, NodeRunning, 7, &Snapshot{Head: 19381426, Peers: 8}, false, now)
	if !s.Synced {
		t.Fatalf("idle after catch-up: %q", s.Line1)
	}
	// A restart of the node (new pid) forgets the previous state.
	s = tr.update(textsDE, NodeRunning, 8, &Snapshot{Head: 19381426, Peers: 8}, false, now)
	if s.Synced {
		t.Fatalf("new pid must start unsynced: %q", s.Line1)
	}
	// Quiet with peers for 10 minutes counts as up to date.
	s = tr.update(textsDE, NodeRunning, 8, &Snapshot{Head: 19381426, Peers: 8}, false, now.Add(quietCaughtUp))
	if !s.Synced {
		t.Fatalf("quiet for 10 min: %q", s.Line1)
	}
}

func TestTrackerKeepsSnapshotOnShortRPCGaps(t *testing.T) {
	now := time.Now()
	var tr tracker
	tr.update(textsDE, NodeRunning, 1, &Snapshot{Syncing: true, Current: 50, Highest: 100, Peers: 3}, false, now)
	for i := 0; i < keepSnapshot; i++ {
		if s := tr.update(textsDE, NodeRunning, 1, nil, false, now); !strings.Contains(s.Line1, "50 %") {
			t.Fatalf("miss %d: %q", i, s.Line1)
		}
	}
	if s := tr.update(textsDE, NodeRunning, 1, nil, false, now); !strings.Contains(s.Line1, "Node startet") {
		t.Fatalf("after %d misses: %q", keepSnapshot+1, s.Line1)
	}
}

func TestTrackerDiskFullAndPeers(t *testing.T) {
	var tr tracker
	if s := tr.update(textsDE, NodeFailed, 0, nil, true, time.Now()); !strings.Contains(s.Line1, "Speicher") {
		t.Fatalf("disk full: %q", s.Line1)
	}
	if s := tr.update(textsDE, NodeRunning, 2, &Snapshot{}, false, time.Now()); s.Line2 != "" {
		t.Fatalf("0 peers must hide the peer line, got %q", s.Line2)
	}
}

func TestFormatInt(t *testing.T) {
	for in, want := range map[uint64]string{0: "0", 999: "999", 1000: "1,000", 19381426: "19,381,426"} {
		if got := formatInt(textsEN, in); got != want {
			t.Errorf("formatInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0}, {"0.1.0", "0.2.0", -1}, {"0.10.0", "0.9.9", 1}, {"1.0", "1.0.0", 0}, {"", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAllTextsFitTooltip(t *testing.T) {
	for _, tx := range []texts{textsDE, textsEN} {
		long := tx.syncState(formatInt(tx, 123456789)) + " · " + tx.peers(50)
		var tr tracker
		s := tr.update(tx, NodeRunning, 1, &Snapshot{Syncing: true, Current: 995, Highest: 1000, Known: 1, Pulled: 123456789, Peers: 50}, false, time.Now())
		if utf8.RuneCountInString(s.Tooltip) > 127 {
			t.Errorf("tooltip too long: %q (%s)", s.Tooltip, long)
		}
	}
}
