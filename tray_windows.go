//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

const (
	trayReadyWait   = time.Minute      // icon not created by then (e.g. Explorer not up yet): restart the tray
	trayRetryLater  = 10 * time.Minute // wait between attempts after the first few
	fastTrayRetry   = 5
	sessionEndWait  = 25 * time.Second // how long onExit waits for gocore at logoff/shutdown
	relaunchDelay   = 15 * time.Second // still alive after this: the shutdown was cancelled
	reloadEventName = `Local\CoreStarter-reload`
	// acceptingName exists while the tray applies reloads; it goes away as soon
	// as the tray starts quitting, so the window knows to start a new one.
	acceptingName = `Local\CoreStarter-accepting`
)

// trayImage: the Windows tray takes the .ico itself and picks the size.
func trayImage(ico []byte) []byte { return ico }

func runTray(t texts, p paths, retry int) {
	trayTexts.Store(&t)
	cur := func() texts { return *trayTexts.Load() }
	// WebView2 data of the first-start window (it may still have been locked
	// when that window closed).
	// Not while that window still shows its "done" page (first start hands
	// over to this tray before the window closes); the next start cleans up.
	if !uiWindowOpen() {
		os.RemoveAll(filepath.Join(os.TempDir(), "CoreStarter-setup-ui"))
		os.Remove(uiDiagPath()) // explained a failed first window; the log has it now
	}
	sup := newSupervisor(p)
	miner := newMinerSup(p)
	var quitting, restarting, trayReady, sessionEnding, needSetup, nodeWanted atomic.Bool
	var current atomic.Pointer[menu]

	// Consent first: without saved settings nothing runs; the window asks.
	// applyFile always reads the newest file and runs one at a time, so a
	// reload during the (possibly slow) first apply is not lost.
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
	// The reload event exists before anything is applied, and only then does
	// the tray announce that it accepts reloads.
	reloadEv := openEvent(reloadEventName)
	var accepting windows.Handle
	if reloadEv != 0 {
		if name, err := windows.UTF16PtrFromString(acceptingName); err == nil {
			accepting, _ = windows.CreateEvent(nil, 1, 0, name)
		}
		go waitEvent(reloadEv, func() { applyFile(false) })
	}
	go applyFile(true)

	var acceptOnce sync.Once
	stopAccepting := func() {
		acceptOnce.Do(func() {
			if accepting != 0 {
				windows.CloseHandle(accepting)
			}
		})
	}
	quit := func() {
		if quitting.Swap(true) {
			return
		}
		stopAccepting()
		if trayReady.Load() {
			systray.SetTooltip("CoreStarter: " + cur().stopping)
		}
		miner.Shutdown()
		sup.Shutdown()
		os.Remove(p.status) // the window must not show a stale "running"
		if !trayReady.Load() {
			os.Exit(0) // no tray window to close
		}
		systray.Quit()
	}

	// Node, miner, quit/reload signals and status must not depend on the tray
	// icon: fyne systray never calls onReady if Windows refuses the icon.
	go watchQuitEvent(quit)
	go func() { // log caps run with or without a tray icon
		for {
			capLog(p.nodeLog, hardNodeLogBytes)
			capLog(p.minerLog, maxAppLog)
			capLog(filepath.Join(p.logs, "starter.log"), maxAppLog)
			time.Sleep(10 * time.Minute)
		}
	}()
	go statusLoop(p, sup, miner, &needSetup, &quitting, &current)
	go func() {
		wait := trayReadyWait
		if retry >= fastTrayRetry {
			wait = trayRetryLater
		}
		time.Sleep(wait)
		if trayReady.Load() || quitting.Load() || sessionEnding.Load() || shuttingDown() {
			return
		}
		// Restart only the tray; the running node is adopted by the new instance.
		log.Printf("tray icon not created after %v; restarting tray (attempt %d)", trayReadyWait, retry+1)
		if err := relaunch(p, flagTrayRetry+strconv.Itoa(retry+1)); err == nil {
			os.Exit(0)
		}
	}()

	// onExit also runs when Windows ends the session (WM_ENDSESSION), and fyne
	// systray calls it even when the user cancels the shutdown. So: stop gocore
	// cleanly while Windows still waits for us, and if we are still alive a bit
	// later the session did not end, so start a fresh tray instance.
	onExit := func() {
		if quitting.Load() {
			return
		}
		sessionEnding.Store(true)
		hurryStop.Store(true) // a gocore still opening its database gets the stop signal now
		log.Printf("session ending: stopping miner and gocore")
		stopped := make(chan struct{})
		go func() {
			miner.ShutdownNow() // the node needs the time Windows grants
			sup.Shutdown()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(sessionEndWait):
		}
		go func() {
			<-stopped
			// A real shutdown can take long when other apps block it; only a
			// cancelled one ends with SM_SHUTTINGDOWN cleared.
			time.Sleep(relaunchDelay)
			for shuttingDown() {
				time.Sleep(5 * time.Second)
			}
			log.Printf("session continues: restarting tray")
			if err := relaunch(p); err != nil {
				log.Printf("relaunch: %v", err)
				return // keep this process; the user can start the program again
			}
			os.Exit(0)
		}()
	}

	onReady := func() {
		t := cur()
		systray.SetIcon(trayIcons[lookOff])
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
		m.uninstall = systray.AddMenuItem(t.menuUninstall, "")
		m.quit = systray.AddMenuItem(t.menuQuit, "")
		current.Store(m)
		trayReady.Store(true)

		// Menu clicks. ShellExecute wants COM initialised on its thread.
		go func() {
			runtime.LockOSThread()
			if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err == nil {
				defer ole.CoUninitialize()
			}
			for {
				select {
				case <-m.settings.ClickedCh:
					openWindow(p)
				case <-m.minerView.ClickedCh:
					openWindow(p, flagViewMiner)
				case <-m.data.ClickedCh:
					os.MkdirAll(p.data, 0o755) // missing when only mining
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
					if keep, ok := askUninstall(cur(), p); ok {
						systray.SetTooltip("CoreStarter: " + cur().stopping)
						quitting.Store(true)
						stopAccepting()
						miner.Shutdown()
						doUninstall(cur(), p, sup, keep)
						systray.Quit()
						return
					}
				case <-m.quit.ClickedCh:
					quit()
					return
				}
			}
		}()
	}

	systray.Run(onReady, onExit)
}

// openWindow starts the settings window as its own process (WebView2 and the
// tray's message loop stay apart; a window crash cannot touch the node).
func openWindow(p paths, extra ...string) {
	cmd := exec.Command(p.exe, append([]string{flagUI}, extra...)...)
	cmd.Dir = p.root
	if err := cmd.Start(); err != nil {
		log.Printf("open window: %v", err)
		return
	}
	cmd.Process.Release()
}

// watchQuitEvent lets a newer download (update) or "Apps & Features" (uninstall)
// ask this running instance to quit cleanly.
func watchQuitEvent(quit func()) {
	watchEvent(quitEventName, func() {
		log.Printf("quit requested by another instance")
		quit()
	})
}

// watchEvent calls fn every time the named auto-reset event is set.
func watchEvent(name string, fn func()) {
	if ev := openEvent(name); ev != 0 {
		waitEvent(ev, fn)
	}
}

// openEvent creates (or opens) a named auto-reset event.
func openEvent(name string) windows.Handle {
	n, _ := windows.UTF16PtrFromString(name)
	// x/sys reports ERROR_ALREADY_EXISTS as an error although the handle is valid.
	ev, err := windows.CreateEvent(nil, 0, 0, n)
	if ev == 0 {
		log.Printf("event %s: %v", name, err)
	}
	return ev
}

// waitEvent calls fn each time ev is set.
func waitEvent(ev windows.Handle, fn func()) {
	for {
		if r, _ := windows.WaitForSingleObject(ev, windows.INFINITE); r != windows.WAIT_OBJECT_0 {
			return
		}
		fn()
	}
}

// signalEvent sets a named event (creating it if the tray has not yet; an
// auto-reset event stays set until someone waits on it).
func signalEvent(name string) bool {
	n, _ := windows.UTF16PtrFromString(name)
	ev, _ := windows.CreateEvent(nil, 0, 0, n)
	if ev == 0 {
		return false
	}
	defer windows.CloseHandle(ev)
	return windows.SetEvent(ev) == nil
}

// warnUser shows a warning without blocking the tray.
func warnUser(t texts, text string) { msgBox(t, text, windows.MB_OK|windows.MB_ICONWARNING) }
