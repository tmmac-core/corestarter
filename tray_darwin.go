//go:build darwin

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
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"
)

// trayImage: the menu bar takes PNG, the largest image of the .ico.
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

	// End of the session (launchd stops the job at logout, or bootout): the
	// miner goes at once, the node gets the time launchd grants (ExitTimeOut).
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	// The quit event (main thread) and launchd's SIGTERM both come at logout:
	// whichever is second waits for the first to finish stopping (once.Do
	// blocks) instead of ending the process halfway.
	stopForSessionEnd := func(why string) {
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
	var endOnce sync.Once
	endSession := func(why string) { endOnce.Do(func() { stopForSessionEnd(why) }) }
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
	go watchForUpdate(p, &quitting, miner, sup)
	notifySetup()

	onReady := func() {
		t := cur()
		systray.SetIcon(trayIcons[lookOff]) // no SetTitle: on macOS it puts text next to the icon
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
		// Uninstalling is in the window (card "Stop"), with a question first.
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
	// A quit event to the helper (loginwindow at logout, Activity Monitor)
	// ends the process right after onExit: stop like at the end of a session.
	systray.Run(onReady, func() { endSession("quit event") })
}

// openWindow starts the settings window as its own process (a window crash
// cannot touch the node): the window app's program, not this helper copy.
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

// watchForUpdate: replacing the app in the Finder gives this program a new
// file (inode) at the same path. The helper then ends with exitUpdated and
// launchd starts the new one; the node keeps running and is adopted (and
// restarted if its own file changed), the miner ends with this process. An
// app bundle that stays gone for 2 minutes means the app was deleted or
// moved: then node and miner stop and the job's property lists are removed
// (a moved app writes them again when it is opened, settings.json keeps the
// autostart choice). While the Finder copies a replacement the bundle is
// there, however long that takes. The Trash alone is no reason: an app
// replaced in the Finder lands there too.
func watchForUpdate(p paths, quitting *atomic.Bool, miner *minerSup, sup *supervisor) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	id := fileID(self)
	var missingSince time.Time
	for !quitting.Load() {
		time.Sleep(30 * time.Second)
		now := fileID(self)
		if now == id {
			missingSince = time.Time{}
			continue
		}
		if now == "" {
			if exists(p.bin) { // being replaced right now
				missingSince = time.Time{}
				continue
			}
			if missingSince.IsZero() {
				missingSince = time.Now()
			} else if time.Since(missingSince) >= 2*time.Minute && !quitting.Swap(true) {
				log.Printf("program removed (%s): stopping node and miner", self)
				miner.Shutdown()
				sup.Shutdown()
				os.Remove(p.status)
				os.Remove(agentsPlist())
				os.Remove(sessionPlist(p))
				os.Exit(0)
			}
			continue
		}
		time.Sleep(5 * time.Second) // let the Finder finish copying the bundle
		if quitting.Swap(true) {
			return
		}
		log.Printf("program updated: restarting the helper")
		miner.ShutdownNow()
		os.Exit(exitUpdated)
	}
}
