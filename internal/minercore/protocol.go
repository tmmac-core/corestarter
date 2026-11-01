package minercore

import "time"

// The tray talks to core-miner.exe through its standard streams, one JSON
// object per line: commands on stdin, status and log lines on stdout. When the
// tray goes away, stdin reaches EOF and the miner stops by itself.

// Exit codes of core-miner.
const (
	ExitOK            = 0
	ExitInitFailed    = 2 // RandomY could not be set up (memory)
	ExitSelfTest      = 3 // not even the interpreter computes correct hashes
	ExitLoginRejected = 4 // the pool refused the address
	ExitBadArgs       = 5
)

// Command from the tray.
type Command struct {
	Cmd     string `json:"cmd"`               // "power" or "stop"
	Percent int    `json:"percent,omitempty"` // for "power": 25, 50, 75 or 100
}

// MinerLine is one line of miner output: Type "status" or "log".
type MinerLine struct {
	Type   string       `json:"type"`
	Msg    string       `json:"msg,omitempty"`
	Status *MinerStatus `json:"status,omitempty"`
}

// MinerStatus is the miner's state as shown in the window and tray.
type MinerStatus struct {
	State         string    `json:"state"` // "starting", "connecting", "mining", "paused-battery", "paused-heat" (macOS), "error"
	Error         string    `json:"error,omitempty"`
	Hashrate      float64   `json:"hashrate"` // H/s, average over the last minute
	Threads       int       `json:"threads"`  // active hashing threads
	MaxThreads    int       `json:"maxThreads"`
	Accepted      int64     `json:"accepted"`
	Rejected      int64     `json:"rejected"`
	Pool          string    `json:"pool,omitempty"` // region of the connected server, e.g. "de"
	JIT           bool      `json:"jit"`
	FullMem       bool      `json:"fullMem"`
	SupportActive bool      `json:"supportActive"`
	SupportHashes int64     `json:"supportHashes"`
	OfflineSecs   int64     `json:"offlineSecs,omitempty"` // "connecting": seconds without a pool session
	PoolNever     bool      `json:"poolNever,omitempty"`   // "connecting": no job from the pool since the miner started
	ThreadRates   []float64 `json:"threadRates,omitempty"` // H/s per hashing thread (index = thread), smoothed over about a minute
	ShareDiff     float64   `json:"shareDiff,omitempty"`   // hashes per share on average (pool target of the current job)
	AnswerMs      int64     `json:"answerMs,omitempty"`    // how long the pool took to answer the last share
	TotalHashes   int64     `json:"totalHashes"`
	XCBPerDay     float64   `json:"xcbPerDay,omitempty"`
	Earnings      *Earnings `json:"earnings,omitempty"`
}

// How long without work from the pool counts as "pool unreachable". Before the
// first job two minutes are enough: CTR answers within seconds, so a longer
// silence means something blocks it (antivirus, firewall, network filter).
// After a working session short reconnects are normal; only 15 minutes count.
const (
	UnreachableFirst = 2 * time.Minute
	UnreachableLater = 15 * time.Minute
)

// UnreachableAfter is the limit that applies to a miner without a pool session.
func UnreachableAfter(never bool) time.Duration {
	if never {
		return UnreachableFirst
	}
	return UnreachableLater
}

// PoolUnreachable reports a miner that has waited for the pool past that limit.
func (s *MinerStatus) PoolUnreachable() bool {
	return s != nil && s.State == "connecting" &&
		time.Duration(s.OfflineSecs)*time.Second >= UnreachableAfter(s.PoolNever)
}
