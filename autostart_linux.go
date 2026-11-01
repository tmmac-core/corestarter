//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The background part (tray) runs as the systemd user unit core-starter.service
// where there is one: a fixed stop budget for gocore at logout and shutdown,
// restart after a crash, logs in the journal. The unit is not enabled; it is
// started by the session's autostart entry (when the user switched autostart
// on) or by the window, so it always runs inside a graphical session. Without
// systemd the tray is started directly.

const (
	unitName      = appID + ".service"
	flagTray      = "--tray"
	flagAutostart = "--autostart"
)

func autostartFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", home, ".config"), "autostart", appID+".desktop")
}

// autostartEnabled: our entry exists and the desktop's own switch (Hidden,
// X-GNOME-Autostart-enabled) has not turned it off.
func autostartEnabled(p paths) bool {
	raw, err := os.ReadFile(autostartFile())
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		switch strings.TrimSpace(line) {
		case "Hidden=true", "X-GNOME-Autostart-enabled=false":
			return false
		}
	}
	return true
}

func setAutostart(p paths, on bool) error {
	f := autostartFile()
	if !on {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if strings.ContainsAny(p.exe, "\r\n") {
		return fmt.Errorf("program path with a line break: %q", p.exe)
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=CoreStarter\n" +
		"Exec=" + desktopQuote(p.exe) + " " + flagAutostart + "\n" +
		"Icon=" + appID + "\n" +
		"NoDisplay=true\n" +
		"X-GNOME-Autostart-enabled=true\n"
	return writeFileAtomic(f, []byte(entry))
}

// desktopQuote quotes a path for an Exec line (Desktop Entry spec): field
// codes (%) doubled, then the Exec quoting, then the escaping every string
// value gets (a backslash doubles once more). Line breaks cannot be written
// into a key at all; setAutostart refuses such a path.
func desktopQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\n\"'\\><~|&;$*?#()`") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`)
	return strings.ReplaceAll(`"`+r.Replace(s)+`"`, `\`, `\\`)
}

// startTray starts the background part: the systemd unit, else directly. A
// tray that is still quitting (stopping gocore can take minutes) is waited
// for first, otherwise the start would go to the old one and be lost.
func startTray(p paths) error {
	if trayQuitting() && !waitTrayGone(gracefulStopWait+time.Minute) {
		return errors.New("CoreStarter is still shutting down")
	}
	if startUnit() {
		return nil
	}
	cmd := exec.Command(p.exe, flagTray)
	cmd.Dir = "/"
	cmd.Env = childEnv()
	// Its own session: closing the terminal or window that started it does not end it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// startUnit starts core-starter.service if this system has a systemd user
// manager and the package installed the unit. The display variables of the
// calling session go to the user manager first: some desktops do not pass
// them on, and the tray needs them to open the window.
func startUnit() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "systemctl", "--user", "cat", unitName).CombinedOutput(); err != nil {
		log.Printf("no systemd user unit (%v: %s); starting the tray directly", err, bytes.TrimSpace(out))
		return false
	}
	var vars []string
	for _, v := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE"} {
		if os.Getenv(v) != "" {
			vars = append(vars, v)
		}
	}
	if len(vars) > 0 {
		exec.CommandContext(ctx, "systemctl", append([]string{"--user", "import-environment"}, vars...)...).Run()
	}
	if out, err := exec.CommandContext(ctx, "systemctl", "--user", "start", unitName).CombinedOutput(); err != nil {
		log.Printf("systemctl start: %v: %s", err, bytes.TrimSpace(out))
		return false
	}
	return true
}
