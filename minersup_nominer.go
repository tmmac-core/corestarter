//go:build nominer

package main

import "github.com/tmmac-core/corestarter/internal/minercore"

// Build without the miner (-tags nominer): same app, mining is not offered.
const minerAvailable = false

type minerConfig struct{}

func minerConfigFrom(Settings) *minerConfig { return nil }

type minerSup struct{}

func newMinerSup(paths) *minerSup { return &minerSup{} }

func (*minerSup) Apply(*minerConfig) {}
func (*minerSup) SetThrottle(bool)   {}
func (*minerSup) Shutdown()          {}
func (*minerSup) ShutdownNow()       {}
func (*minerSup) View() (bool, bool, *minercore.MinerStatus, string) {
	return false, false, nil, ""
}
