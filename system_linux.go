//go:build linux

package main

import (
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

// systemTexts: the texts for the session's language (LC_ALL, LC_MESSAGES,
// LANG, as gettext reads them), English for any language the app does not have.
func systemTexts() texts {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(env)
		if v == "" {
			continue
		}
		code := strings.ToLower(v)
		if i := strings.IndexAny(code, "_.@"); i >= 0 {
			code = code[:i]
		}
		if t, ok := textsByLang[code]; ok {
			return *t
		}
		return textsEN // the first set variable decides, also for "C" or "POSIX"
	}
	return textsEN
}

func freeBytes(dir string) (uint64, error) {
	for {
		var st unix.Statfs_t
		err := unix.Statfs(dir, &st)
		if err == nil {
			return st.Bavail * uint64(st.Bsize), nil
		}
		// A folder that does not exist yet: ask its parent.
		parent := dir[:max(strings.LastIndexByte(dir, '/'), 1)]
		if !os.IsNotExist(err) || parent == dir {
			return 0, err
		}
		dir = parent
	}
}

// shellOpen opens a folder or a web page with the desktop's default program.
func shellOpen(path string) {
	cmd := exec.Command("xdg-open", path)
	cmd.Env = childEnv()
	if err := cmd.Start(); err != nil {
		log.Printf("xdg-open: %v", err)
		return
	}
	go cmd.Wait()
}

// warnUser shows a desktop notification (freedesktop notification service);
// the tray runs without a window and must not block on a dialog.
func warnUser(t texts, text string) {
	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("notification: %v", err)
		return
	}
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		t.appName, uint32(0), appID, t.appName, text, []string{},
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(1))}, int32(-1))
	if call.Err != nil {
		log.Printf("notification: %v", call.Err)
	}
}

// totalMemory is the installed RAM in bytes, 0 if unknown.
func totalMemory() uint64 {
	var si unix.Sysinfo_t
	if unix.Sysinfo(&si) != nil {
		return 0
	}
	return uint64(si.Totalram) * uint64(si.Unit)
}

// onARM64: the miner is built for x86_64 only.
func onARM64() bool { return runtime.GOARCH == "arm64" }

// childEnv: this process's environment without the WebKit modes the window
// set for itself (webkitStage), so the file manager, the tray and gocore do
// not inherit them. Only while a mode is active; a WEBKIT_DISABLE_* the user
// set by hand is then dropped too for children (the window itself keeps it).
func childEnv() []string {
	ours := map[string]bool{}
	if os.Getenv(webkitStageEnv) != "" {
		ours[webkitStageEnv] = true
		for _, st := range webkitStages {
			for _, kv := range st {
				k, _, _ := strings.Cut(kv, "=")
				ours[k] = true
			}
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !ours[k] {
			env = append(env, kv)
		}
	}
	return env
}
