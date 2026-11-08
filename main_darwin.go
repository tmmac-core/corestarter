//go:build darwin

package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func init() {
	// AppKit (window, menu bar, dialogs) belongs to the main thread.
	runtime.LockOSThread()
}

func main() {
	t := systemTexts()
	// As root the chain, the settings and the node would belong to root.
	// Checked first: even the run folder must not be created as root.
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
		// The launchd job: the background part with the menu bar icon.
		lock, ok := lockFile(trayLockName, 0)
		if !ok {
			return // already running (exit 0: launchd leaves it at that)
		}
		defer lock.Close()
		log.Printf("CoreStarter %s, gocore %s, miner %v", appVersion, gocoreVersion, minerAvailable)
		runTray(t, p)
	case hasArg(args, flagUI):
		view := ""
		if hasArg(args, flagViewMiner) {
			view = "miner"
		}
		runWindow(t, p, uiSettings, view, func(s Settings) error { return saveFromWindow(p, s) })
	case !inApplications(p.bin):
		// Opened from the DMG, Downloads or a translocated copy: a launchd job
		// pointing there would break once the DMG is ejected.
		msgBox(t, t.mac().moveToApps, msgInfo)
		shellOpen("/Applications")
		revealInFinder(p.bin)
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
		// Opened from the Finder, Launchpad or the Dock: make sure the
		// background part runs, then show the window. A helper that is still
		// quitting is waited for in the background, so the window shows at once.
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

// inApplications: the app lies in /Applications or ~/Applications (not in a
// DMG, Downloads, or App Translocation's read-only copy). A program outside
// any app bundle (tests, a build folder) counts as in place.
func inApplications(bundle string) bool {
	if !strings.HasSuffix(bundle, ".app") || os.Getenv("CORE_STARTER_ANYWHERE") == "1" {
		return true
	}
	if strings.Contains(bundle, "/AppTranslocation/") {
		return false
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Dir(bundle)
	return dir == "/Applications" || dir == filepath.Join(home, "Applications")
}

func revealInFinder(path string) {
	if err := startDetached("/usr/bin/open", "-R", path); err != nil {
		log.Printf("open -R: %v", err)
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
	if trayAccepting() { // a helper still running (settings were deleted): reload
		askTrayReload()
		return nil
	}
	return startTray(p)
}
