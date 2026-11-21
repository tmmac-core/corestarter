package main

import (
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

// AppStatus is what the tray writes to status.json every few seconds and the
// window reads. The tray is the only writer.
type AppStatus struct {
	Version   string    `json:"version"`
	Gocore    string    `json:"gocore"`
	Updated   time.Time `json:"updated"`
	NeedSetup bool      `json:"needSetup"` // no settings saved yet: nothing runs

	NodeOn    bool   `json:"nodeOn"`
	NodeLine  string `json:"nodeLine"` // same text as the tray menu
	PeersLine string `json:"peersLine"`
	Synced    bool   `json:"synced"`
	Syncing   bool   `json:"syncing"` // initial sync running: mining is throttled
	// Progress for the window's bar (see Status): phase, 0..1 (-1: no total)
	// and the estimated seconds left (0: not estimated yet).
	SyncPhase  string  `json:"syncPhase,omitempty"`
	SyncFrac   float64 `json:"syncFrac,omitempty"`
	SyncEtaSec int64   `json:"syncEtaSec,omitempty"`
	// Why the node does not run (only while it has failed): "disk",
	// "foreign", "port" or "crash", with the explanation the tray shows.
	NodeCause string `json:"nodeCause,omitempty"`
	NodeHint  string `json:"nodeHint,omitempty"`
	// For the window's network card: connections now, and how many of them
	// were opened from outside (-1: not known, e.g. the node is off or does
	// not answer).
	Peers   int `json:"peers"`
	Inbound int `json:"inbound"`
	Port    int `json:"port,omitempty"` // P2P port, for the port-forwarding hint
	// Since the node started: minutes running, and whether any node has
	// connected from outside meanwhile (the window's "reachable" line).
	RunMin      int  `json:"runMin"`
	InboundEver bool `json:"inboundEver,omitempty"`
	// New blocks this node imported and checked itself since it started
	// (head now minus the first head seen in this run; the fast sync's
	// blocks are not executed and do not count). For "Your contribution".
	BlocksChecked uint64 `json:"blocksChecked,omitempty"`

	MinerOn     bool                   `json:"minerOn"`
	MinerLine   string                 `json:"minerLine"`
	MinerError  string                 `json:"minerError,omitempty"`  // supervisor-level failure
	MineConsent bool                   `json:"mineConsent,omitempty"` // mining is on but waits for the current consent
	Throttled   bool                   `json:"throttled"`
	Miner       *minercore.MinerStatus `json:"miner,omitempty"`
	// For the window's miner view: hashrate per poll (oldest first, up to
	// historyLen) and when the accepted count last went up (Unix seconds).
	History   []float64 `json:"history,omitempty"`
	LastShare int64     `json:"lastShare,omitempty"`
}

// historyLen: ten minutes of samples at the tray's poll interval (5 s).
const historyLen = 120

// minerTrack follows the miner between polls for History and LastShare.
type minerTrack struct {
	history   []float64
	accepted  int64
	lastShare time.Time
}

// update records one poll. A stopped miner clears the history; a new miner
// process starts its count at 0 again.
func (m *minerTrack) update(on bool, ms *minercore.MinerStatus, now time.Time) ([]float64, int64) {
	if !on {
		*m = minerTrack{}
		return nil, 0
	}
	rate := 0.0
	if ms != nil && ms.State == "mining" {
		rate = ms.Hashrate
	}
	m.history = append(m.history, rate)
	if len(m.history) > historyLen {
		m.history = append(m.history[:0:0], m.history[len(m.history)-historyLen:]...)
	}
	if ms != nil {
		switch {
		case ms.Accepted < m.accepted:
			m.lastShare = time.Time{} // a new miner process: its count and "last share" start over
		case ms.Accepted > m.accepted:
			m.lastShare = now
		}
		m.accepted = ms.Accepted
	}
	var last int64
	if !m.lastShare.IsZero() {
		last = m.lastShare.Unix()
	}
	return append([]float64(nil), m.history...), last
}
