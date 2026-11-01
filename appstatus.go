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
