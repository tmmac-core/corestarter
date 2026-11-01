//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mutexName     = `Local\CoreStarter`
	quitEventName = `Local\CoreStarter-quit`
	quitWait      = readyWait + gracefulStopWait + 2*time.Minute // worst-case node stop plus margin
	flagHandover  = "--handover"                                 // started by another instance that is just exiting
	flagTrayRetry = "--tray-retry="                              // restarted because the tray icon could not be created
	flagUninst    = "--uninstall"
	flagCleanup   = "--cleanup"
	helperPrefix  = "CoreStarter-cleanup-"
)

var (
	instanceMutex  windows.Handle
	procSetShutdwn = kernel32.NewProc("SetProcessShutdownParameters")
	procGetSysMet  = user32.NewProc("GetSystemMetrics")
)

// shuttingDown reports whether Windows is logging off or shutting down (SM_SHUTTINGDOWN).
func shuttingDown() bool {
	r, _, _ := procGetSysMet.Call(0x2000)
	return r != 0
}

func init() {
	// A mutex belongs to the thread that created or acquired it, and only that
	// thread can release it. Keep main on one OS thread so the instance mutex is
	// created, waited for and released on the same thread.
	runtime.LockOSThread()
	// Load system DLLs only from System32, never from the folder the exe was
	// started from (e.g. Downloads, where a planted DLL could otherwise be picked
	// up). Child processes such as gocore are not affected.
	windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	// Ask Windows to notify us early at shutdown, so gocore gets time to stop.
	procSetShutdwn.Call(0x3FF, 0)
}

func main() {
	t := systemTexts()
	p, err := appPaths()
	if err != nil {
		msgBox(t, t.installFailed(err), windows.MB_OK|windows.MB_ICONERROR)
		return
	}
	if s, err := LoadSettings(p.settings); err == nil {
		t = textsFor(s.Lang)
	}
	args := os.Args[1:]
	// Explicitly "Run as administrator": node, miner and autostart would run
	// elevated, or land in another account's profile when admin credentials
	// were typed in. Only that case is refused: with UAC off every token is
	// elevated, and uninstalling must always work.
	if explicitlyElevated() && !hasArg(args, flagUninst) && !hasArg(args, flagCleanup) {
		msgBox(t, t.noAdmin, windows.MB_OK|windows.MB_ICONWARNING)
		return
	}
	if len(args) > 0 && args[0] == flagCleanup {
		runCleanup(t, p, args[1:])
		return
	}
	log.SetOutput(io.Discard) // until we know the user agreed to an install
	if exists(p.exe) {
		setupLog(p)
	}
	if hasArg(args, flagUI) {
		view := ""
		if hasArg(args, flagViewMiner) {
			view = "miner"
		}
		runWindow(t, p, uiSettings, view, func(s Settings) error { return saveFromWindow(p, s) })
		return
	}
	removeOldHelpers()

	// The tray instance owns this mutex until it exits, so another instance can
	// wait for it to quit (WAIT_ABANDONED / WAIT_OBJECT_0).
	name, _ := windows.UTF16PtrFromString(mutexName)
	instanceMutex, err = windows.CreateMutex(nil, true, name)
	if instanceMutex == 0 {
		// e.g. access denied because an instance runs as administrator
		msgBox(t, t.alreadyRunning, windows.MB_OK|windows.MB_ICONINFORMATION)
		return
	}
	otherRunning := errors.Is(err, windows.ERROR_ALREADY_EXISTS)
	if otherRunning && hasArg(args, flagHandover) {
		// The instance that started us is exiting or someone else holds a handle; take over.
		// As long as a quitting tray may need to stop gocore (the window starts
		// a new tray right after saving, also while the old one quits).
		r, _ := windows.WaitForSingleObject(instanceMutex, uint32(quitWait/time.Millisecond))
		otherRunning = r != windows.WAIT_OBJECT_0 && r != windows.WAIT_ABANDONED
	}

	if hasArg(args, flagUninst) {
		keep, ok := askUninstall(t, p)
		if !ok {
			return
		}
		closeSettingsWindow()
		if otherRunning && !requestQuit() {
			msgBox(t, t.updateTimeout, windows.MB_OK|windows.MB_ICONWARNING)
			return
		}
		doUninstall(t, p, nil, keep)
		return
	}

	if otherRunning {
		switch {
		case !exists(p.exe):
			// The other instance is a first-start window that is still open.
			focusWindow()
		case p.runningFromInstallDir() || sameContent(p.exe):
			// Start menu or a second click: show the settings of the running program.
			openWindow(p)
		case newerInstalled(t, p):
		default:
			if msgBox(t, t.updatePrompt, windows.MB_YESNO|windows.MB_ICONQUESTION) != idYes {
				return
			}
			if !claimUpdate() {
				msgBox(t, t.alreadyRunning, windows.MB_OK|windows.MB_ICONINFORMATION)
				return
			}
			closeSettingsWindow() // its Save would otherwise start the old tray again
			if !requestQuit() {
				msgBox(t, t.updateTimeout, windows.MB_OK|windows.MB_ICONWARNING)
				return
			}
			updateAndRelaunch(t, p)
		}
		return
	}

	if !p.runningFromInstallDir() {
		if newerInstalled(t, p) {
			return
		}
		if exists(p.exe) {
			if !claimUpdate() {
				msgBox(t, t.alreadyRunning, windows.MB_OK|windows.MB_ICONINFORMATION)
				return
			}
			updateAndRelaunch(t, p)
			return
		}
		// First start: nothing is written before the user pressed "Start".
		// After the install the same window stays open as the settings window
		// (miner view, "Settings" from the tray menu): later saves go to the
		// running tray like any settings change.
		installed := false
		runWindow(t, p, uiInstall, "", func(s Settings) error {
			if installed {
				return saveFromWindow(p, s)
			}
			err := firstInstall(t, p, s)
			installed = err == nil
			return err
		})
		return
	}

	if shuttingDown() {
		return // never start the node while Windows is ending the session
	}
	if err := preparePayload(p); err != nil {
		log.Printf("payload: %v", err)
		msgBox(t, t.installFailed(err), windows.MB_OK|windows.MB_ICONERROR)
		return
	}
	log.Printf("CoreStarter %s, gocore %s, miner %v", appVersion, gocoreVersion, minerAvailable)
	runTray(t, p, trayRetry(args))
}

// firstInstall is the window's "Start" on a fresh PC: check, install, save the
// choices, hand over to the installed copy (which starts the tray).
func firstInstall(t texts, p paths, s Settings) error {
	if err := s.Normalize(); err != nil {
		return err
	}
	if err := preflight(p, s.Node); err != nil {
		return err
	}
	if err := install(p); err != nil {
		return err
	}
	if err := SaveSettings(p.settings, s); err != nil {
		return err
	}
	if err := setAutostart(p, s.Autostart); err != nil {
		log.Printf("autostart: %v", err)
	}
	return relaunch(p)
}

// updateAndRelaunch installs this (newer or same) version over an existing one.
func updateAndRelaunch(t texts, p paths) {
	closeSettingsWindow()
	if err := install(p); err != nil {
		log.Printf("install: %v", err)
		msgBox(t, t.installFailed(err), windows.MB_OK|windows.MB_ICONERROR)
		return
	}
	// Hand over to the installed copy, so the downloaded file can be deleted.
	if err := relaunch(p); err != nil {
		msgBox(t, t.installFailed(err), windows.MB_OK|windows.MB_ICONERROR)
	}
}

// focusWindow brings an open CoreStarter window to the front.
func focusWindow() {
	cls, _ := windows.UTF16PtrFromString(uiClassName)
	if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); h != 0 {
		bringToFront(h)
	}
}

// relaunch starts the installed copy and gives up this instance's mutex.
// The cwd is the program folder, not e.g. the download folder, so that one
// can be deleted or ejected.
// The child waits (--handover) until this process exits and its mutex is
// abandoned, so the mutex is only given up once the child really started.
func relaunch(p paths, extra ...string) error {
	cmd := exec.Command(p.exe, append(extra, flagHandover)...)
	cmd.Dir = p.root
	if err := cmd.Start(); err != nil {
		return err
	}
	onMainThread(releaseInstanceMutex)
	return nil
}

func trayRetry(args []string) int {
	for _, a := range args {
		if strings.HasPrefix(a, flagTrayRetry) {
			n, _ := strconv.Atoi(strings.TrimPrefix(a, flagTrayRetry))
			return n
		}
	}
	return 0
}

// releaseInstanceMutex must run on the main thread (see init). CloseHandle
// alone would keep the mutex owned until this process exits, so a waiting
// --handover child would time out while the install window is still open.
func releaseInstanceMutex() {
	if instanceMutex != 0 {
		if err := windows.ReleaseMutex(instanceMutex); err != nil {
			// Not the owning thread: the mutex stays owned until this process
			// exits, which callers outside the main thread do right after.
			log.Printf("release instance mutex: %v", err)
		}
		windows.CloseHandle(instanceMutex)
		instanceMutex = 0
	}
}

// requestQuit signals the running instance to stop its node and exit, then
// waits until it is gone (its mutex becomes ours).
func requestQuit() bool {
	if !signalEvent(quitEventName) {
		return false
	}
	r, _ := windows.WaitForSingleObject(instanceMutex, uint32(quitWait/time.Millisecond))
	return r == windows.WAIT_OBJECT_0 || r == windows.WAIT_ABANDONED
}

// newerInstalled refuses to downgrade an existing installation.
func newerInstalled(t texts, p paths) bool {
	raw, err := os.ReadFile(p.version)
	if err != nil {
		return false
	}
	installed := strings.TrimSpace(string(raw))
	if compareVersions(installed, appVersion) > 0 {
		msgBox(t, t.newerInstalled(installed, appVersion), windows.MB_OK|windows.MB_ICONINFORMATION)
		return true
	}
	return false
}

// sameContent reports whether this exe is byte-identical to the installed one.
func sameContent(installed string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	a, err1 := fileSHA256(self)
	b, err2 := fileSHA256(installed)
	return err1 == nil && err2 == nil && a == b
}

// install copies this program and the node into %LOCALAPPDATA%. Consent and
// the free-space check happen before (window or fallback dialog).
func install(p paths) error {
	if err := os.MkdirAll(p.root, 0o755); err != nil {
		return err
	}
	setupLog(p)
	flushUINotes()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// After an update hand-over the old process may still be unmapping its exe.
	for attempt := 0; ; attempt++ {
		if err = copyFile(self, p.exe); err == nil || attempt == 10 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return err
	}
	if err := preparePayload(p); err != nil {
		return err
	}
	if err := registerUninstall(p); err != nil {
		log.Printf("uninstall entry: %v", err)
	}
	if err := createShortcut(p); err != nil {
		log.Printf("start menu shortcut: %v", err)
	}
	if err := writeFileAtomic(p.version, []byte(appVersion)); err != nil {
		log.Printf("version file: %v", err)
	}
	log.Printf("installed %s", appVersion)
	return nil
}

// preparePayload extracts the node. If an old gocore from an earlier run is
// still holding the files (update while orphaned), it is stopped cleanly first.
func preparePayload(p paths) error {
	err := ensurePayload(p.bin)
	if err == nil {
		return nil
	}
	np, _ := adoptNode(p)
	if np == nil {
		return err
	}
	log.Printf("payload busy (%v); stopping running gocore pid %d", err, np.pid)
	np.stop()
	np.release()
	os.Remove(p.pidFile)
	return ensurePayload(p.bin)
}

// askUninstall asks for confirmation and whether to keep the blockchain data.
func askUninstall(t texts, p paths) (keepData, ok bool) {
	if msgBox(t, t.uninstallConfirm, windows.MB_YESNO|windows.MB_ICONQUESTION) != idYes {
		return false, false
	}
	if !exists(p.data) {
		return false, true
	}
	switch msgBox(t, t.uninstallKeepData(formatGB(t, dirSize(p.data))), windows.MB_YESNOCANCEL|windows.MB_ICONQUESTION) {
	case idYes:
		return false, true
	case idNo:
		return true, true
	}
	return false, false
}

// doUninstall stops the node, removes autostart and starts the cleanup helper:
// a temporary copy of this program, because a running exe cannot delete itself.
// sup is nil when called from "Apps & Features" instead of the tray.
func doUninstall(t texts, p paths, sup *supervisor, keepData bool) {
	closeSettingsWindow()
	if sup != nil {
		sup.Shutdown()
	} else if np, _ := adoptNode(p); np != nil {
		np.stop()
		np.release()
	}
	if err := setAutostart(p, false); err != nil {
		log.Printf("autostart off: %v", err)
	}
	// The uninstall entry and shortcut are removed by the helper once deletion
	// worked, so a failed cleanup can be retried from "Apps & Features".
	err := func() error {
		token, err := randomHex(16)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(p.token, []byte(token)); err != nil {
			return err
		}
		self, err := os.Executable()
		if err != nil {
			return err
		}
		helper := filepath.Join(os.TempDir(), fmt.Sprintf("%s%d.exe", helperPrefix, os.Getpid()))
		if err := copyFile(self, helper); err != nil {
			return err
		}
		cmd := exec.Command(helper, flagCleanup, strconv.Itoa(os.Getpid()), strconv.FormatBool(keepData), token)
		cmd.Dir = os.TempDir() // a process's working directory cannot be deleted
		return cmd.Start()
	}()
	if err != nil {
		log.Printf("cleanup helper: %v", err)
		msgBox(t, t.uninstallFailed(err), windows.MB_OK|windows.MB_ICONERROR)
	}
}

// runCleanup runs from the temporary helper copy: it waits for the uninstalling
// process to exit, then deletes the program folder. The folder is always
// recomputed from %LOCALAPPDATA%, never taken from the command line, and the
// helper only acts with the one-time token the uninstaller wrote there.
func runCleanup(t texts, p paths, args []string) {
	defer deleteSelfLater()
	log.SetOutput(io.Discard)
	os.Chdir(os.TempDir())
	if len(args) != 3 {
		return
	}
	raw, err := os.ReadFile(p.token)
	if err != nil || strings.TrimSpace(string(raw)) != args[2] {
		return
	}
	keepData, err := strconv.ParseBool(args[1])
	if err != nil {
		keepData = true // when in doubt, keep the user's data
	}
	if pid, err := strconv.ParseUint(args[0], 10, 32); err == nil {
		if h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)); err == nil {
			windows.WaitForSingleObject(h, 60_000)
			windows.CloseHandle(h)
		}
	}
	killProcessesOf(p.miner, "core-miner.exe") // normally gone with the tray's job object
	if nodeStillRunning(p) {
		msgBox(t, t.uninstallFailed(errors.New("gocore.exe is still running")), windows.MB_OK|windows.MB_ICONERROR)
		os.Remove(p.token)
		return
	}
	keystoreKept := false
	for attempt := 0; attempt < 10; attempt++ {
		if keystoreKept, err = removeInstall(p, keepData); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		msgBox(t, t.uninstallFailed(err), windows.MB_OK|windows.MB_ICONERROR)
		return
	}
	unregisterUninstall()
	removeShortcut()
	switch {
	case keepData:
		msgBox(t, t.uninstallDoneKeptData(p.data), windows.MB_OK|windows.MB_ICONINFORMATION)
	case keystoreKept:
		msgBox(t, t.keystoreKept(p.keystore), windows.MB_OK|windows.MB_ICONINFORMATION)
	default:
		msgBox(t, t.uninstallDone, windows.MB_OK|windows.MB_ICONINFORMATION)
	}
}

// explicitlyElevated: an elevated token split off by UAC (TokenElevationTypeFull).
// A default token (UAC off, built-in Administrator) is not refused.
func explicitlyElevated() bool {
	const tokenElevationType, tokenElevationTypeFull = 18, 2
	var typ, n uint32
	tok := windows.GetCurrentProcessToken()
	if windows.GetTokenInformation(tok, tokenElevationType, (*byte)(unsafe.Pointer(&typ)), 4, &n) != nil {
		return false
	}
	return typ == tokenElevationTypeFull
}

// claimUpdate: only one updater at a time; a second double-click while the
// first waits for the old tray would otherwise fail on the new, running exe.
// The handle stays open until this process exits.
func claimUpdate() bool {
	name, _ := windows.UTF16PtrFromString(`Local\CoreStarter-update`)
	_, err := windows.CreateMutex(nil, false, name)
	return err == nil
}

// deleteSelfLater removes this cleanup helper from %TEMP% after it exited:
// after an uninstall no CoreStarter runs again that could clean it up.
func deleteSelfLater() {
	self, err := os.Executable()
	// cmd.exe expands %VAR% and !VAR! even inside quotes: never hand it such a path.
	if err != nil || strings.ContainsAny(self, "%!\"") || !strings.HasPrefix(filepath.Base(self), helperPrefix) {
		return
	}
	// Same folder, also when one of the two is an 8.3 short path.
	a, err1 := os.Stat(filepath.Dir(self))
	b, err2 := os.Stat(os.TempDir())
	if err1 != nil || err2 != nil || !os.SameFile(a, b) {
		return
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return
	}
	cmd := exec.Command(filepath.Join(sys, "cmd.exe"))
	// ping 127.0.0.1 is the usual console-free wait; nothing leaves the PC.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /d /c "ping -n 3 127.0.0.1 >nul & del /f /q "` + self + `""`, // CmdLine replaces the whole line, argv0 included
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	cmd.Dir = os.TempDir()
	if cmd.Start() == nil {
		cmd.Process.Release()
	}
}

// nodeStillRunning reports whether any gocore from our folder still runs (or a
// gocore.exe we cannot inspect).
func nodeStillRunning(p paths) bool {
	_, h, err := findNode(p)
	if h != 0 {
		windows.CloseHandle(h)
		return true
	}
	return err != nil
}

// removeInstall deletes the program folder. Account keys (data\keystore) are
// never deleted; it reports whether such keys were kept.
func removeInstall(p paths, keepData bool) (keystoreKept bool, err error) {
	if !strings.EqualFold(filepath.Base(p.root), appFolder) {
		return false, fmt.Errorf("refusing to delete unexpected folder %s", p.root)
	}
	if !keepData && dirHasFiles(p.keystore) {
		keepData, keystoreKept = true, true
		if err := removeChildrenExcept(p.data, filepath.Base(p.keystore)); err != nil {
			return keystoreKept, err
		}
	}
	if !keepData {
		return false, os.RemoveAll(p.root)
	}
	return keystoreKept, removeChildrenExcept(p.root, filepath.Base(p.data))
}

// removeOldHelpers deletes cleanup helpers left in %TEMP% by earlier uninstalls.
func removeOldHelpers() {
	self, _ := os.Executable()
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), helperPrefix+"*.exe"))
	for _, m := range matches {
		if !strings.EqualFold(filepath.Clean(m), filepath.Clean(self)) {
			os.Remove(m)
		}
	}
}
