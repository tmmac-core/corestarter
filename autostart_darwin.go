//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The background part (helper with the menu bar icon) always runs as the
// launchd job info.tmmac.core-starter: restart after a crash, a stop budget at
// logout (ExitTimeOut), only in the graphical session (Aqua). Its property
// list lies in ~/Library/LaunchAgents when the user switched autostart on
// (launchd loads it at login), else in Application Support, from where the
// window loads it for this session. Same job either way.

const (
	flagTray      = "--tray"
	flagAutostart = "--autostart"
	jobLabel      = appID
	// exitUpdated: the helper's program was replaced; a non-zero exit makes
	// launchd start the new one (KeepAlive SuccessfulExit=false).
	exitUpdated = 75
)

func agentsPlist() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", jobLabel+".plist")
}

func sessionPlist(p paths) string { return filepath.Join(p.root, jobLabel+".plist") }

func guiDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func autostartEnabled(p paths) bool { return exists(agentsPlist()) }

// setAutostart moves the job's property list between LaunchAgents (loaded at
// login) and Application Support (loaded by the window). A loaded job keeps
// running either way.
func setAutostart(p paths, on bool) error {
	data, err := jobPlist(p)
	if err != nil {
		return err
	}
	if on {
		if err := os.MkdirAll(filepath.Dir(agentsPlist()), 0o755); err != nil {
			return err
		}
		if err := writePlist(agentsPlist(), data); err != nil {
			return err
		}
		os.Remove(sessionPlist(p))
		return nil
	}
	if err := writePlist(sessionPlist(p), data); err != nil {
		return err
	}
	if err := os.Remove(agentsPlist()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func writePlist(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// jobPlist: the launchd job for the helper of this app bundle.
func jobPlist(p paths) ([]byte, error) {
	exe := helperExe(p)
	if !filepath.IsAbs(exe) {
		return nil, fmt.Errorf("helper path not absolute: %q", exe)
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + jobLabel + `</string>
	<key>ProgramArguments</key>
	<array><string>` + html.EscapeString(exe) + `</string><string>` + flagTray + `</string></array>
	<key>AssociatedBundleIdentifiers</key><string>` + appID + `</string>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>LimitLoadToSessionType</key><string>Aqua</string>
	<key>ProcessType</key><string>Standard</string>
	<key>ExitTimeOut</key><integer>120</integer>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>StandardOutPath</key><string>/dev/null</string>
	<key>StandardErrorPath</key><string>/dev/null</string>
</dict>
</plist>
`)
	return b.Bytes(), nil
}

// jobLoaded: launchd knows the job in this session.
func jobLoaded() bool {
	return launchctl("print", guiDomain()+"/"+jobLabel) == nil
}

// startTray starts the background part through launchd. A helper that is
// still quitting (stopping gocore can take minutes) is waited for first,
// otherwise the start would go to the old one and be lost; one that is just
// starting is left alone. Otherwise the job is loaded afresh from a property
// list written now: a job loaded earlier may point to where the app was
// before it was moved, and kickstarting that would start nothing. Where the
// list goes (LaunchAgents or not) follows settings.json, so autostart comes
// back after a move.
func startTray(p paths) error {
	if trayQuitting() && !waitTrayGone(gracefulStopWait+time.Minute) {
		return errors.New("CoreStarter is still shutting down")
	}
	if helperRunning() {
		return nil
	}
	autostart := autostartEnabled(p)
	if s, err := LoadSettings(p.settings); err == nil {
		autostart = s.Autostart
	}
	if err := setAutostart(p, autostart); err != nil {
		return err
	}
	plist := sessionPlist(p)
	if autostart {
		plist = agentsPlist()
	}
	if jobLoaded() {
		if err := launchctl("bootout", guiDomain()+"/"+jobLabel); err != nil {
			log.Printf("start: %v", err)
		}
	}
	// bootout can return before launchd has let go of the label.
	var err error
	for range 10 {
		if err = launchctl("bootstrap", guiDomain(), plist); err == nil || jobLoaded() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// helperRunning: a helper of this user runs (from any copy of the app).
func helperRunning() bool {
	suffix := "/" + helperApp + "/Contents/MacOS/core-starter"
	procs, _ := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	for _, kp := range procs {
		if pid := int(kp.Proc.P_pid); pid != os.Getpid() {
			if exe, _, err := procArgs(pid); err == nil && strings.HasSuffix(exe, suffix) {
				return true
			}
		}
	}
	return false
}

// removeJob unloads the job (the helper gets SIGTERM if it still runs) and
// deletes both property lists.
func removeJob(p paths) {
	if jobLoaded() {
		if err := launchctl("bootout", guiDomain()+"/"+jobLabel); err != nil {
			log.Printf("launchctl bootout: %v", err)
		}
	}
	os.Remove(agentsPlist())
	os.Remove(sessionPlist(p))
}

func launchctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", args[0], err, bytes.TrimSpace(out))
	}
	return nil
}
