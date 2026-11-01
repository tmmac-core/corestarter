//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath       = `Software\Microsoft\Windows\CurrentVersion\Run`
	uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\CoreStarter`
	runValueName     = "CoreStarter"
	shortcutName     = "CoreStarter.lnk"
	// Name up to 0.8.x; install and uninstall remove it so an update does not
	// leave two entries in the Start menu.
	oldShortcutName = "Core Starter.lnk"
)

// MessageBox return values (winuser.h); x/sys/windows does not define them.
const (
	idCancel = 2
	idYes    = 6
	idNo     = 7
)

var procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

// systemTexts: the texts for Windows' display language, English for any
// language the app does not have. Primary language ids: 0x07 German, 0x11
// Japanese, 0x13 Dutch, 0x18 Romanian (any region).
func systemTexts() texts {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	code := map[uintptr]string{0x07: "de", 0x11: "ja", 0x13: "nl", 0x18: "ro"}[lang&0x3ff]
	if t, ok := textsByLang[code]; ok {
		return *t
	}
	return textsEN
}

func msgBox(t texts, body string, flags uint32) int32 {
	b, _ := windows.UTF16PtrFromString(body)
	c, _ := windows.UTF16PtrFromString(t.appName)
	r, _ := windows.MessageBox(0, b, c, flags|windows.MB_SETFOREGROUND)
	return r
}

func shellOpen(path string) {
	verb, _ := windows.UTF16PtrFromString("open")
	p, _ := windows.UTF16PtrFromString(path)
	windows.ShellExecute(0, verb, p, nil, nil, windows.SW_SHOWNORMAL)
}

func freeBytes(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, totalFree uint64
	err = windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree)
	return avail, err
}

// --- Autostart (HKCU Run key, per user, no admin rights) ---

// Windows' own autostart switch (Task Manager / Settings → Apps → Startup)
// writes a binary flag here; an odd first byte means "disabled".
const startupApprovedPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

func autostartEnabled(p paths) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil || v != quoted(p.exe) {
		return false
	}
	if a, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedPath, registry.QUERY_VALUE); err == nil {
		defer a.Close()
		if flag, _, err := a.GetBinaryValue(runValueName); err == nil && len(flag) > 0 && flag[0]&1 == 1 {
			return false
		}
	}
	return true
}

func setAutostart(p paths, on bool) error {
	// A fresh profile may not have a Run key yet.
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Clearing Windows' own "disabled" flag makes our toggle the single source of truth.
	if a, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedPath, registry.SET_VALUE); err == nil {
		a.DeleteValue(runValueName)
		a.Close()
	}
	if on {
		return k.SetStringValue(runValueName, quoted(p.exe))
	}
	if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// --- "Apps & Features" entry so users can uninstall the usual Windows way ---

func registerUninstall(p paths) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, val := range map[string]string{
		"DisplayName":     "CoreStarter",
		"DisplayVersion":  appVersion,
		"Publisher":       "TMMAC",
		"DisplayIcon":     p.exe,
		"InstallLocation": p.root,
		"UninstallString": quoted(p.exe) + " --uninstall",
	} {
		if err := k.SetStringValue(name, val); err != nil {
			return err
		}
	}
	k.SetDWordValue("NoModify", 1)
	k.SetDWordValue("NoRepair", 1)
	return nil
}

func unregisterUninstall() {
	registry.DeleteKey(registry.CURRENT_USER, uninstallKeyPath)
}

// --- Start menu shortcut via the Windows Script Host COM object (no PowerShell) ---

func shortcutPath() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, shortcutName), nil
}

func createShortcut(p paths) error {
	lnk, err := shortcutPath()
	if err != nil {
		return err
	}
	// Known folders exist on normal profiles, but are not guaranteed.
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	// COM is per thread: the goroutine must not move between the calls below
	// (the first install runs this from a goroutine).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		// S_FALSE (already initialised on this thread) is fine.
		if oe, ok := err.(*ole.OleError); !ok || oe.Code() != 1 {
			return err
		}
	}
	defer ole.CoUninitialize()
	unk, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer unk.Release()
	shell, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	res, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return err
	}
	sc := res.ToIDispatch()
	defer sc.Release()
	for prop, val := range map[string]string{
		"TargetPath":       p.exe,
		"WorkingDirectory": p.root,
		"IconLocation":     p.exe + ",0",
		"Description":      "Core Blockchain node",
	} {
		if _, err := oleutil.PutProperty(sc, prop, val); err != nil {
			return fmt.Errorf("%s: %w", prop, err)
		}
	}
	if _, err = oleutil.CallMethod(sc, "Save"); err != nil {
		return err
	}
	os.Remove(filepath.Join(filepath.Dir(lnk), oldShortcutName))
	return nil
}

func removeShortcut() {
	if lnk, err := shortcutPath(); err == nil {
		os.Remove(lnk)
		os.Remove(filepath.Join(filepath.Dir(lnk), oldShortcutName))
	}
}

func quoted(s string) string { return `"` + s + `"` }
