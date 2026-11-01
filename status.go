package main

import (
	"strconv"
	"strings"
	"time"
)

// Status is the human-readable state shown in the tray menu and tooltip.
type Status struct {
	Line1   string // main state, e.g. "Synchronisierung: 45 %"
	Line2   string // peers, empty when there are none
	Tooltip string // Windows limits tray tooltips to 127 characters
	Synced  bool
}

// NodeState is what the supervisor knows independently of RPC.
type NodeState int

const (
	NodeStarting NodeState = iota
	NodeRunning
	NodeStopping
	NodeStopped
	NodeFailed
)

const (
	// statePhaseGap: in the initial fast sync the block download stops at the
	// pivot (64 blocks below the top); within this many blocks of the top the
	// node is downloading account state, which has no percentage.
	statePhaseGap = 1000
	// caughtUpGap: a running node is "up to date" when it is at most this many
	// blocks behind the best peer (new blocks arrive every ~7 s).
	caughtUpGap = 10
	// quietCaughtUp: with peers and no sync activity for this long, the node is
	// considered up to date even if we never saw a sync finish.
	quietCaughtUp = 10 * time.Minute
	// keepSnapshot: a few failed polls (busy disk) keep the last known status.
	keepSnapshot = 3
)

// tracker turns the stream of node snapshots into a stable, honest status.
// Two gocore facts drive it (go-core v2.2.2):
//   - xcb_blockNumber stays 0 during the whole initial fast sync, so Head == 0
//     identifies the first sync;
//   - xcb_syncing's knownStates includes progress persisted in the database and
//     never resets, so it is only meaningful while Head == 0.
type tracker struct {
	last        *Snapshot
	misses      int
	caughtUp    bool
	quietSince  time.Time
	lastNodePID uint32
}

// update is called every poll. pid identifies the node process, so a restart
// resets what we learned about the previous one.
func (tr *tracker) update(t texts, st NodeState, pid uint32, snap *Snapshot, diskFull bool, now time.Time) Status {
	if pid != tr.lastNodePID || st != NodeRunning {
		tr.last, tr.misses, tr.caughtUp, tr.quietSince = nil, 0, false, time.Time{}
		tr.lastNodePID = pid
	}
	if st == NodeRunning {
		if snap == nil {
			tr.misses++
			if tr.misses <= keepSnapshot {
				snap = tr.last
			}
		} else {
			tr.last, tr.misses = snap, 0
		}
	}

	var s Status
	switch st {
	case NodeStopping:
		s.Line1 = t.stopping
	case NodeStopped:
		s.Line1 = t.stopped
	case NodeFailed:
		if diskFull {
			s.Line1 = t.diskFull
		} else {
			s.Line1 = t.failed
		}
	case NodeStarting:
		s.Line1 = t.starting
	case NodeRunning:
		if snap == nil {
			s.Line1 = t.starting
			break
		}
		s.Line2 = t.peers(snap.Peers)
		s.Line1, s.Synced = tr.describeRunning(t, snap, now)
	}
	tip := "CoreStarter: " + s.Line1
	if s.Line2 != "" {
		tip += " · " + s.Line2
	}
	s.Tooltip = truncateRunes(tip, 127)
	return s
}

func (tr *tracker) describeRunning(t texts, snap *Snapshot, now time.Time) (string, bool) {
	// Initial fast sync: nothing imported yet.
	if snap.Head == 0 {
		tr.quietSince = time.Time{}
		switch {
		case snap.Syncing && snap.Highest > 0:
			if snap.Highest-min(snap.Current, snap.Highest) <= statePhaseGap && snap.Known > 0 {
				return t.syncState(formatInt(t, snap.Pulled)), false
			}
			return t.syncBlocks(percent(snap.Current, snap.Highest), formatInt(t, snap.Current), formatInt(t, snap.Highest)), false
		case snap.Peers == 0:
			return t.searching, false
		default:
			return t.waiting, false
		}
	}

	// Node has a chain: up to date, catching up after a pause, or checking.
	if snap.Syncing && snap.Highest > snap.Current {
		gap := snap.Highest - snap.Current
		if gap <= caughtUpGap {
			tr.caughtUp = true
		} else {
			tr.caughtUp = false
			tr.quietSince = time.Time{}
			done := snap.Current - min(snap.Starting, snap.Current)
			total := snap.Highest - min(snap.Starting, snap.Highest)
			return t.catchingUp(percent(done, total), formatInt(t, gap)), false
		}
	}
	if snap.Peers == 0 {
		tr.quietSince = time.Time{}
		return t.noPeers(formatInt(t, snap.Head)), false
	}
	if !snap.Syncing {
		if tr.quietSince.IsZero() {
			tr.quietSince = now
		}
		if now.Sub(tr.quietSince) >= quietCaughtUp {
			tr.caughtUp = true
		}
	}
	if tr.caughtUp {
		return t.synced(formatInt(t, snap.Head)), true
	}
	return t.checking(formatInt(t, snap.Head)), false
}

// percent never reports 100 while work is left.
func percent(done, total uint64) int {
	if total == 0 {
		return 0
	}
	p := int(float64(done) / float64(total) * 100)
	if p > 99 {
		p = 99
	}
	if p < 0 {
		p = 0
	}
	return p
}

// formatInt groups thousands with the language's separator (19.381.426 / 19,381,426).
func formatInt(t texts, n uint64) string {
	digits := strconv.FormatUint(n, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(t.thousandsSep)
		}
		b.WriteRune(c)
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// compareVersions compares dotted numeric versions ("0.1.0" < "0.10.0").
// Missing or non-numeric parts count as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
