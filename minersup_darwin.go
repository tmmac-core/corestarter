//go:build darwin && !nominer

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// minerGuard: macOS has no Pdeathsig. The miner ends by itself when its stdin
// closes (whenever the helper is gone) and when its parent ends (kqueue in
// core-miner), so starting it needs nothing special.
type minerGuard struct{ on bool }

func (g minerGuard) ok() bool { return g.on }

func newMinerGuard() (minerGuard, error) { return minerGuard{on: true}, nil }

func minerProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func startGuarded(_ minerGuard, cmd *exec.Cmd) error { return cmd.Start() }

// minerIntact: the miner is part of the signed app bundle; there is no copy
// to restore.
func minerIntact(p paths) bool {
	fi, err := os.Stat(p.miner)
	return err == nil && fi.Mode().IsRegular()
}
