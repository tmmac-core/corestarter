//go:build linux

package main

import (
	"errors"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
)

var errUninstallByPackage = errors.New("remove CoreStarter with your software manager")

func init() {
	// GTK belongs to the thread that initialised it: keep main on one OS thread.
	runtime.LockOSThread()
}

func main() {
	t := systemTexts()
	// As root the chain, the settings and the node would belong to root and
	// run with all rights; CoreStarter is meant for the own account. Checked
	// first: even the run folder must not be created as root.
	if os.Geteuid() == 0 {
		msgBox(t, t.noAdmin, msgWarn)
		return
	}
	p, err := appPaths()
	if err != nil {
		msgBox(t, t.installFailed(err), msgError)
		return
	}
	appP = p
	if s, err := LoadSettings(p.settings); err == nil {
		t = textsFor(s.Lang)
	}
	args := os.Args[1:]
	log.SetOutput(io.Discard) // until the user agreed (settings saved)
	_, noSettings := LoadSettings(p.settings)
	if noSettings == nil {
		setupLog(p)
	}

	switch {
	case hasArg(args, flagTray):
		lock, ok := lockFile(trayLockName, 0)
		if !ok {
			return // already running
		}
		defer lock.Close()
		log.Printf("CoreStarter %s, gocore %s, miner %v", appVersion, gocoreVersion, minerAvailable)
		runTray(t, p)
	case hasArg(args, flagAutostart):
		// The session's autostart entry: the background part only, no window.
		if noSettings == nil {
			if err := startTray(p); err != nil {
				log.Printf("autostart: %v", err)
			}
		}
	case hasArg(args, flagUI):
		view := ""
		if hasArg(args, flagViewMiner) {
			view = "miner"
		}
		runWindow(t, p, uiSettings, view, func(s Settings) error { return saveFromWindow(p, s) })
	case noSettings != nil:
		// First start: nothing is written before the user pressed "Start". The
		// same window then stays open as the settings window.
		started := false
		runWindow(t, p, uiInstall, "", func(s Settings) error {
			if started {
				return saveFromWindow(p, s)
			}
			err := firstStart(p, s)
			started = err == nil
			return err
		})
	default:
		// Started from the menu: make sure the background part runs, then show
		// the window (on plain GNOME there is no tray icon to click).
		// A tray that is still quitting is waited for in the background, so
		// the window shows at once; this process stays until the start is done.
		if os.Getenv(webkitStageEnv) == "" {
			applyStage(savedStage(p)) // before childEnv reads the environment
		}
		started := make(chan struct{})
		go func() {
			defer close(started)
			if !trayAccepting() {
				if err := startTray(p); err != nil {
					log.Printf("start tray: %v", err)
				}
			}
		}()
		runWindow(t, p, uiSettings, "", func(s Settings) error { return saveFromWindow(p, s) })
		<-started
	}
}

// firstStart is the window's "Start" on a fresh account: check, save the
// choices, set autostart, start the background part.
func firstStart(p paths, s Settings) error {
	if err := s.Normalize(); err != nil {
		return err
	}
	if err := preflight(p, s.Node); err != nil {
		return err
	}
	if err := SaveSettings(p.settings, s); err != nil {
		return err
	}
	setupLog(p)
	flushUINotes()
	log.Printf("first start: CoreStarter %s", appVersion)
	if err := setAutostart(p, s.Autostart); err != nil {
		log.Printf("autostart: %v", err)
	}
	if n := webkitStage(); n > 0 { // the consent is on record now
		os.WriteFile(stageFile(p), []byte(strconv.Itoa(n)), 0o600)
	}
	if trayAccepting() { // a tray still running (settings were deleted): reload
		askTrayReload()
		return nil
	}
	return startTray(p)
}
