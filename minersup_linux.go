//go:build linux && !nominer

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// minerGuard starts miners from one OS thread that never ends. The miner gets
// SIGKILL when the thread that started it dies (Pdeathsig follows the thread,
// not the process), so that thread must live as long as the tray. The second
// guard is the miner itself: it exits when its stdin closes, which happens
// whenever the tray process is gone.
type minerGuard struct {
	starts chan minerStart
}

type minerStart struct {
	cmd  *exec.Cmd
	done chan error
}

func (g minerGuard) ok() bool { return g.starts != nil }

func newMinerGuard() (minerGuard, error) {
	g := minerGuard{starts: make(chan minerStart)}
	go func() {
		runtime.LockOSThread() // never unlocked: the thread lives until the process ends
		for s := range g.starts {
			s.done <- s.cmd.Start()
		}
	}()
	return g, nil
}

func minerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
}

func startGuarded(g minerGuard, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	g.starts <- minerStart{cmd: cmd, done: done}
	return <-done
}

// minerIntact: the package manager owns the file; there is no copy to restore.
func minerIntact(p paths) bool {
	fi, err := os.Stat(p.miner)
	return err == nil && fi.Mode().IsRegular()
}
