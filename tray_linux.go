//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

// trayImage: the desktop's tray (StatusNotifierItem) takes PNG, the largest image of the .ico.
func trayImage(ico []byte) []byte { return largestPNG(ico) }

// largestPNG returns the biggest PNG-coded image of an .ico file.
func largestPNG(ico []byte) []byte {
	if len(ico) < 6 {
		return nil
	}
	var best []byte
	bestW := -1
	for i := 0; i < int(binary.LittleEndian.Uint16(ico[4:])) && 6+16*(i+1) <= len(ico); i++ {
		e := ico[6+16*i:]
		size, off := int(binary.LittleEndian.Uint32(e[8:])), int(binary.LittleEndian.Uint32(e[12:]))
		if off+size > len(ico) || size < 8 || !bytes.HasPrefix(ico[off:], []byte("\x89PNG")) {
			continue
		}
		if cfg, err := png.DecodeConfig(bytes.NewReader(ico[off : off+size])); err == nil && cfg.Width > bestW {
			best, bestW = ico[off:off+size], cfg.Width
		}
	}
	return best
}

func runTray(t texts, p paths) {
	trayTexts.Store(&t)
	cur := func() texts { return *trayTexts.Load() }
	sup := newSupervisor(p)
	miner := newMinerSup(p)
	var quitting, sessionEnding, needSetup, nodeWanted, restarting, accepting atomic.Bool
	var current atomic.Pointer[menu]

	// Consent first: without saved settings nothing runs; the window asks.
	var applyMu sync.Mutex
	applyFile := func(first bool) {
		applyMu.Lock()
		defer applyMu.Unlock()
		if quitting.Load() || sessionEnding.Load() {
			return
		}
		s, err := LoadSettings(p.settings)
		if err != nil {
			if !first {
				log.Printf("reload settings: %v", err)
				return
			}
			log.Printf("settings: %v; waiting for the window", err)
			needSetup.Store(true)
			openWindow(p)
			sup.Stop() // a node left running by a killed tray, without consent on record
			return
		}
		if !first {
			log.Printf("settings reloaded (node=%v mine=%v power=%d)", s.Node, s.Mine, s.Power)
		}
		if nt := textsFor(s.Lang); nt.lang != cur().lang {
			trayTexts.Store(&nt)
			if m := current.Load(); m != nil {
				m.retitle(nt)
			}
		}
		needSetup.Store(false)
		nodeWanted.Store(s.Node)
		if s.Node {
			sup.Start()
		} else {
			sup.Stop()
		}
		applyMiner(miner, s)
	}

	trayUp := atomic.Bool{}
	quit := func() {
		if quitting.Swap(true) {
			return
		}
		accepting.Store(false)
		if trayUp.Load() {
			systray.SetTooltip("CoreStarter: " + cur().stopping)
		}
		miner.Shutdown()
		sup.Shutdown()
		os.Remove(p.status) // the window must not show a stale "running"
		log.Printf("quit")
		os.Exit(0)
	}

	// The control socket exists before anything is applied; reloads are
	// accepted from then on.
	err := serveControl(traySockName, func(r ctlRequest) ctlReply {
		switch r.Cmd {
		case "ping":
			return ctlReply{OK: true, Version: appVersion, Accepting: accepting.Load()}
		case "reload":
			if !accepting.Load() {
				return ctlReply{}
			}
			go applyFile(false)
			return ctlReply{OK: true}
		case "quit":
			log.Printf("quit requested by the window")
			go quit()
			return ctlReply{OK: true}
		}
		return ctlReply{}
	})
	if err != nil {
		log.Printf("control socket: %v", err)
	}
	accepting.Store(true)
	go applyFile(true)

	// End of the session (systemd stops the unit, logout, kill): the miner goes
	// at once, the node gets the time systemd grants (unit TimeoutStopSec).
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	// hurryStop: a gocore still opening its database is not waited for
	// (readyWait), the unit's stop budget is shorter.
	endSession := func(why string) {
		if quitting.Swap(true) {
			return
		}
		sessionEnding.Store(true)
		hurryStop.Store(true)
		accepting.Store(false)
		log.Printf("%s: stopping miner and gocore", why)
		miner.ShutdownNow()
		sup.Shutdown()
		os.Remove(p.status)
	}
	go func() {
		sig := <-sigs
		endSession(sig.String())
		os.Exit(0)
	}()

	go func() { // log caps
		for {
			capLog(p.nodeLog, hardNodeLogBytes)
			capLog(p.minerLog, maxAppLog)
			capLog(filepath.Join(p.logs, "starter.log"), maxAppLog)
			time.Sleep(10 * time.Minute)
		}
	}()
	go statusLoop(p, sup, miner, &needSetup, &quitting, &current)
	go watchForUpdate(&quitting, quit)
	go hintWithoutTrayIcon(p, cur)

	onReady := func() {
		t := cur()
		systray.SetIcon(trayIcons[lookOff])
		systray.SetTitle("CoreStarter")
		systray.SetTooltip("CoreStarter: " + t.starting)
		m := &menu{}
		m.status = systray.AddMenuItem(t.starting, "")
		m.status.Disable()
		m.peers = systray.AddMenuItem("", "")
		m.peers.Disable()
		m.peers.Hide()
		m.miner = systray.AddMenuItem("", "")
		m.miner.Disable()
		m.miner.Hide()
		m.minerView = systray.AddMenuItem(t.menuMiner, "")
		m.minerView.Hide()
		systray.AddSeparator()
		m.settings = systray.AddMenuItem(t.menuSettings, "")
		m.data = systray.AddMenuItem(t.menuOpenData, "")
		m.logs = systray.AddMenuItem(t.menuOpenLog, "")
		m.restart = systray.AddMenuItem(t.menuRestart, "")
		systray.AddSeparator()
		// Uninstalling is the package manager's job on Linux.
		m.uninstall = systray.AddMenuItem(t.menuUninstall, "")
		m.uninstall.Hide()
		m.quit = systray.AddMenuItem(t.menuQuit, "")
		current.Store(m)
		trayUp.Store(true)
		go func() {
			for {
				select {
				case <-m.settings.ClickedCh:
					openWindow(p)
				case <-m.minerView.ClickedCh:
					openWindow(p, flagViewMiner)
				case <-m.data.ClickedCh:
					os.MkdirAll(p.data, 0o700) // missing when only mining
					shellOpen(p.data)
				case <-m.logs.ClickedCh:
					shellOpen(p.logs)
				case <-m.restart.ClickedCh:
					// Only a node the settings switched on; the menu never starts one by itself.
					if nodeWanted.Load() && !needSetup.Load() && !restarting.Swap(true) {
						go func() {
							defer restarting.Store(false)
							sup.Restart()
						}()
					}
				case <-m.uninstall.ClickedCh:
				case <-m.quit.ClickedCh:
					quit()
					return
				}
			}
		}()
	}
	// Without a tray host (plain GNOME) systray only logs and the icon stays
	// invisible; everything above runs regardless, the window is the control.
	systray.Run(onReady, func() {})
}

// openWindow starts the settings window as its own process (a window crash
// cannot touch the node).
func openWindow(p paths, extra ...string) {
	cmd := exec.Command(p.exe, append([]string{flagUI}, extra...)...)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("open window: %v", err)
		return
	}
	go cmd.Wait()
}

// watchForUpdate: after a package update the running program file is replaced
// (/proc/self/exe then ends in " (deleted)"). The tray starts the new version
// in its place: same pid, the node keeps running and is adopted, the miner
// ends with this process image and the new one starts it again. A file that
// stays missing means the package was removed: then everything stops.
func watchForUpdate(quitting *atomic.Bool, quit func()) {
	var missingSince time.Time
	for !quitting.Load() {
		time.Sleep(30 * time.Second)
		exe, err := os.Readlink("/proc/self/exe")
		if err != nil || !strings.HasSuffix(exe, " (deleted)") {
			continue
		}
		path := strings.TrimSuffix(exe, " (deleted)")
		if fi, err := os.Stat(path); err != nil || fi.Mode()&0o111 == 0 {
			if missingSince.IsZero() {
				missingSince = time.Now()
			} else if time.Since(missingSince) >= 2*time.Minute {
				log.Printf("program removed (%s): stopping node and miner", path)
				quit()
				return
			}
			continue // being replaced right now
		}
		missingSince = time.Time{}
		time.Sleep(5 * time.Second) // let the package manager finish the other files
		if quitting.Load() {
			return
		}
		log.Printf("program updated: restarting the tray")
		if err := syscall.Exec(path, os.Args, os.Environ()); err != nil {
			log.Printf("exec %s: %v", path, err)
			return
		}
	}
}

// hintWithoutTrayIcon: plain GNOME shows no tray icons. Node and miner would
// then run without any visible sign, so once per login a notification says
// where they are controlled (the marker lives in the run folder, which the
// session clears).
func hintWithoutTrayIcon(p paths, cur func() texts) {
	time.Sleep(10 * time.Second) // let the desktop's tray host come up after login
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	var has bool
	if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has) != nil || has {
		return
	}
	marker := filepath.Join(p.run, "no-tray-hint")
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return // already shown in this session
	}
	f.Close()
	t := cur()
	warnUser(t, noTrayHint[t.lang])
}
