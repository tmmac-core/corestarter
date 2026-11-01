//go:build darwin && e2e && !nominer

package main

import (
	"os"
	"path/filepath"
)

// Test build only (scripts/build-mac.sh with E2E=1, DMG name ends in -e2e):
// the whole app as shipped, but the node runs offline (no discovery, no
// peers, no UPnP) and the miner mines on tools/fakepool on this Mac. Never
// distributed: P2P and real mining are started by people, not by tests.
//
// CORE_STARTER_E2E_ROOT moves settings, data, logs and the run folder below
// one folder, so a test job runs next to a real installation of this user
// (scripts/mac-session-test.sh).
func init() {
	extraNodeArgs = []string{"--nodiscover", "--maxpeers", "0", "--nat", "none"}
	extraMinerArgs = []string{"--pool", "127.0.0.1:47999", "--light", "--api=false"}
	if r := os.Getenv("CORE_STARTER_E2E_ROOT"); r != "" {
		relocatePaths = func(p *paths) {
			bundle := p.bin
			*p = pathsAt(filepath.Join(r, "CoreStarter"), filepath.Join(r, "logs"), bundle)
			p.run = filepath.Join(r, "run")
		}
	}
}
