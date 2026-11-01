//go:build !windows && !linux && !darwin

package main

import (
	"errors"
	"net"
	"os/exec"
	"syscall"
	"time"
)

// Stubs so the platform-neutral code builds and its tests run on a Mac
// (development only; the app ships for Windows and Linux).

type nodeHandle = int

const gracefulStopWait time.Duration = 0
const hurryReadyWait time.Duration = 0

var errUnsupported = errors.New("not supported on this system")

func startNode(paths, int) (*nodeProc, error)   { return nil, errUnsupported }
func adoptNode(paths) (*nodeProc, error)        { return nil, nil }
func dialNode(string, uint32) (net.Conn, error) { return nil, errUnsupported }
func freeBytes(string) (uint64, error)          { return 0, errUnsupported }
func (np *nodeProc) interrupt() error           { return errUnsupported }
func (np *nodeProc) kill()                      {}
func (np *nodeProc) release()                   {}
func (np *nodeProc) exitCode() int              { return -1 }

const minerExeName = "core-miner"

type minerGuard struct{}

func (minerGuard) ok() bool                    { return false }
func newMinerGuard() (minerGuard, error)       { return minerGuard{}, errUnsupported }
func minerProcAttr() *syscall.SysProcAttr      { return nil }
func startGuarded(minerGuard, *exec.Cmd) error { return errUnsupported }
func minerIntact(paths) bool                   { return false }
func killProcessesOf(string, string)           {}
func warnUser(texts, string)                   {}

func trayImage(ico []byte) []byte { return ico }

const gocoreVersion = "dev"

type nativeWin struct{}

func (w *window) wake()              {}
func (w *window) eval(string)        {}
func (w *window) closeWindow()       {}
func (w *window) firstFrame()        {}
func licenseDir(p paths) string      { return p.bin }
func askTrayReload()                 {}
func askTrayQuit() bool              { return false }
func startUninstall(paths) error     { return errUnsupported }
func trayAccepting() bool            { return false }
func startTray(paths) error          { return errUnsupported }
func autostartEnabled(paths) bool    { return false }
func setAutostart(paths, bool) error { return errUnsupported }
func totalMemory() uint64            { return 0 }
func onARM64() bool                  { return false }
func shellOpen(string)               {}
