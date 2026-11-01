package main

import (
	"strings"
	"testing"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

// Before the first job the tray says after two minutes that the pool is out of
// reach and names the usual causes, instead of "connecting" for ever.
func TestMinerLinePoolUnreachable(t *testing.T) {
	st := func(secs int64, never bool) AppStatus {
		return AppStatus{Miner: &minercore.MinerStatus{State: "connecting", OfflineSecs: secs, PoolNever: never}}
	}
	if got := minerLine(textsDE, st(60, true)); got != textsDE.minerConnecting {
		t.Fatalf("after 1 min: %q", got)
	}
	got := minerLine(textsDE, st(130, true))
	if !strings.Contains(got, "2 min") || !strings.Contains(got, "Virenscanner") {
		t.Fatalf("after 2 min: %q", got)
	}
	if got := minerLine(textsDE, st(600, false)); got != textsDE.minerConnecting {
		t.Fatalf("reconnect after a session: %q", got)
	}
}
