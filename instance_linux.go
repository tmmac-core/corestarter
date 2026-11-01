//go:build linux

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// One tray per user: it holds tray.lock (flock) while it runs and answers on
// tray.sock. The window has its own window.lock/window.sock, so a second start
// brings the open window to the front. Both live in the private run folder
// (paths.run, mode 0700), and every connection is checked with SO_PEERCRED.

const (
	trayLockName   = "tray.lock"
	traySockName   = "tray.sock"
	windowLockName = "window.lock"
	windowSockName = "window.sock"
)

// appP is the paths of this run, set by main before anything else; the tray
// helpers below keep the same signatures as on Windows.
var appP paths

type ctlRequest struct {
	Cmd  string `json:"cmd"`            // tray: ping, reload, quit; window: show
	View string `json:"view,omitempty"` // window "show": "form" or "miner"
}

type ctlReply struct {
	OK        bool   `json:"ok"`
	Version   string `json:"version,omitempty"`
	Accepting bool   `json:"accepting,omitempty"`
}

// lockFile takes an exclusive flock on name, held until the process ends
// (O_CLOEXEC: also released by an exec of a new version).
func lockFile(name string, wait time.Duration) (*os.File, bool) {
	f, err := os.OpenFile(filepath.Join(appP.run, name), os.O_RDWR|os.O_CREATE|unix.O_CLOEXEC, 0o600)
	if err != nil {
		log.Printf("lock %s: %v", name, err)
		return nil, false
	}
	deadline := time.Now().Add(wait)
	for {
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
			return f, true
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// serveControl answers requests on sockName until the process ends.
func serveControl(sockName string, handle func(ctlRequest) ctlReply) error {
	path := filepath.Join(appP.run, sockName)
	os.Remove(path) // left by a crashed run; we hold the lock, so it is not live
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			go func() {
				defer c.Close()
				if cred, err := peerCred(c); err != nil || int(cred.Uid) != os.Getuid() {
					return
				}
				c.SetDeadline(time.Now().Add(5 * time.Second))
				var req ctlRequest
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil || json.Unmarshal(line, &req) != nil {
					return
				}
				b, _ := json.Marshal(handle(req))
				c.Write(append(b, '\n'))
			}()
		}
	}()
	return nil
}

// askControl sends one request and returns the reply.
func askControl(sockName string, req ctlRequest) (ctlReply, error) {
	var r ctlReply
	c, err := net.DialTimeout("unix", filepath.Join(appP.run, sockName), 2*time.Second)
	if err != nil {
		return r, err
	}
	defer c.Close()
	if cred, err := peerCred(c); err != nil || int(cred.Uid) != os.Getuid() {
		return r, errors.New("control socket of another user")
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return r, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(line, &r)
}

// trayAccepting: a running tray that still applies reloads.
func trayAccepting() bool {
	r, err := askControl(traySockName, ctlRequest{Cmd: "ping"})
	return err == nil && r.OK && r.Accepting
}

// trayQuitting: a tray answers but no longer takes requests: it is shutting down.
func trayQuitting() bool {
	r, err := askControl(traySockName, ctlRequest{Cmd: "ping"})
	return err == nil && r.OK && !r.Accepting
}

func askTrayReload() { askControl(traySockName, ctlRequest{Cmd: "reload"}) }

func askTrayQuit() bool {
	r, err := askControl(traySockName, ctlRequest{Cmd: "quit"})
	return err == nil && r.OK
}

// waitTrayGone waits until no tray holds the lock any more.
func waitTrayGone(wait time.Duration) bool {
	f, ok := lockFile(trayLockName, wait)
	if ok {
		f.Close()
	}
	return ok
}
