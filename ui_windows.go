//go:build windows

package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const (
	uiClassName  = "CoreStarterWindow"
	uiMutexName  = `Local\CoreStarter-ui`
	webView2Page = "https://go.microsoft.com/fwlink/p/?LinkId=2124703" // Microsoft's WebView2 runtime download
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetWindowTPID    = user32.NewProc("GetWindowThreadProcessId")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procGetForegroundWnd = user32.NewProc("GetForegroundWindow")
	procAttachThreadIn   = user32.NewProc("AttachThreadInput")
	procBringWindowToTop = user32.NewProc("BringWindowToTop")
	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmClose            = 0x0010
	wmDpiChanged       = 0x02E0
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
	wmApp              = 0x8000
	wmShowView         = wmApp + 1 // another start asked for a view (wParam viewForm/viewMiner)
	viewForm           = 0
	viewMiner          = 1
	wsOverlappedWindow = 0x00CF0000
	wsMaximizeBox      = 0x00010000
	cwUseDefault       = 0x80000000
	swpNoSize          = 0x0001
	swpNoMove          = 0x0002
	hwndTopmost        = ^uintptr(0) // HWND_TOPMOST (-1)
	hwndNoTopmost      = ^uintptr(1) // HWND_NOTOPMOST (-2)
	swRestore          = 9
	idcArrow           = 32512
)

type wndClassEx struct {
	size, style                        uint32
	wndProc                            uintptr
	clsExtra, wndExtra                 int32
	instance, icon, cursor, background windows.Handle
	menuName, className                *uint16
	iconSm                             windows.Handle
}

type winMsg struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// nativeWin: the Win32 window and its WebView2.
type nativeWin struct {
	hwnd     uintptr
	chromium *edge.Chromium
}

var theWindow *window

// uiWatchdog: the page reports its first drawn frame (requestAnimationFrame).
// None by then means the user sees nothing (on a real PC the page's script
// ran while the window stayed white), so the window gives way to the plain
// dialog. Generous: a cold WebView2 start on a slow disk takes a while.
// uiEmbedWait: go-webview2's Embed waits forever when the environment cannot
// be created; the process then ends with a message instead of hanging.
// Vars for the tests.
var (
	uiWatchdog  = 45 * time.Second
	uiEmbedWait = 60 * time.Second
)

// uiBrowserArgs: software rendering, because a GPU driver that fails WebView2
// leaves the window white, and no native occlusion tracking, because an
// always-on-top overlay (GPU tools, recorders, chat overlays) can make
// Chromium think the window is covered, and a covered window is not painted.
// A static form loses nothing by either. A var for the tests.
var uiBrowserArgs = []string{"--disable-gpu", "--disable-features=CalculateNativeWinOcclusion"}

func webView2Available() bool {
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	return err == nil && v != ""
}

// runWindow shows the start/settings window and returns whether the user saved.
// Without the WebView2 runtime it falls back to plain dialogs.
// view "miner" opens on the live miner view (tray menu); an open window
// switches to it.
func runWindow(t texts, p paths, mode uiMode, view string, onSave func(Settings) error) bool {
	name, _ := windows.UTF16PtrFromString(uiMutexName)
	m, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		// Already open: bring it to the front instead of a second window.
		cls, _ := windows.UTF16PtrFromString(uiClassName)
		if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); h != 0 {
			bringToFront(h)
			// The tray's "Settings" or "Show miner": the open window switches.
			v := uintptr(viewForm)
			if view == "miner" {
				v = viewMiner
			}
			procPostMessageW.Call(h, wmShowView, v, 0)
		}
		windows.CloseHandle(m)
		return false
	}
	if m != 0 {
		defer windows.CloseHandle(m)
	}
	if !webView2Available() {
		return fallbackDialog(t, p, mode, onSave, false)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := &window{t: t, p: p, mode: mode, view: view, onSave: onSave}
	theWindow = w
	if v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(""); err == nil {
		uiNote("webview2: runtime %s", v)
	}
	err = w.run()
	if err != nil {
		uiNote("window: %v", err)
		writeUIDiag()
		theWindow = nil
		return fallbackDialog(t, p, mode, onSave, true)
	}
	return w.saved
}

// uiDiagPath: only written when the window failed, so a window that showed
// nothing can be explained without installing anything. Technical values
// only (versions, sizes); the tray deletes it on its next start.
func uiDiagPath() string { return filepath.Join(os.TempDir(), "CoreStarter-diag.txt") }

func writeUIDiag() {
	uiNotesMu.Lock()
	text := strings.Join(uiNotes, "\r\n") + "\r\n"
	uiNotesMu.Unlock()
	os.WriteFile(uiDiagPath(), []byte(text), 0o644)
}

// fallbackDialog: the old consent dialog for the first start; mining needs the
// window. windowFailed: WebView2 is there but the window showed nothing.
func fallbackDialog(t texts, p paths, mode uiMode, onSave func(Settings) error, windowFailed bool) bool {
	_, noSettings := LoadSettings(p.settings)
	if mode == uiSettings && noSettings == nil {
		if windowFailed {
			msgBox(t, t.windowFailed(uiDiagPath()), windows.MB_OK|windows.MB_ICONWARNING)
			return false
		}
		if msgBox(t, t.noWebView, windows.MB_YESNO|windows.MB_ICONINFORMATION) == idYes {
			shellOpen(webView2Page)
		}
		return false
	}
	// First start, or settings missing/damaged (the tray waits for consent):
	// the old consent question, node only. Mining needs the window.
	body := fmt.Sprintf(t.installBody, gocoreVersion, needGB)
	if windowFailed {
		body = t.windowFailed(uiDiagPath()) + "\n\n" + body
	}
	if msgBox(t, body, windows.MB_YESNO|windows.MB_ICONINFORMATION) != idYes {
		return false
	}
	s := DefaultSettings()
	s.Consent.agree(true, false, time.Now()) // "Yes" to the notes in installBody
	if err := onSave(s); err != nil {
		msgBox(t, t.installFailed(err), windows.MB_OK|windows.MB_ICONERROR)
		return false
	}
	msgBox(t, t.installedHint, windows.MB_OK|windows.MB_ICONINFORMATION)
	return true
}

func (w *window) run() error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		if oe, ok := err.(*ole.OleError); !ok || oe.Code() != 1 { // S_FALSE: already initialised
			return err
		}
	}
	defer ole.CoUninitialize()

	inst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	icon, _, _ := procLoadIconW.Call(inst, 1) // go-winres stores the app icon as ID 1
	cls, _ := windows.UTF16PtrFromString(uiClassName)
	wc := wndClassEx{
		wndProc:  syscall.NewCallback(uiWndProc),
		instance: windows.Handle(inst),
		icon:     windows.Handle(icon),
		iconSm:   windows.Handle(icon),
		cursor:   windows.Handle(cursor),
		// Brand canvas until the view draws: never a white window.
		background: windows.Handle(canvasBrush()),
		className:  cls,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	// A second window in this process (fallback paths, tests) finds the class registered.
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			dpi = d
		}
	}
	title, _ := windows.UTF16PtrFromString(w.t.appName)
	width, height := 560*dpi/96, 780*dpi/96
	x, y := uintptr(cwUseDefault), uintptr(cwUseDefault)
	// e.g. 1920x1080 at 150 %: 1170 px would put "Save" under the taskbar.
	// A clamped window starts at the top of the work area, else Windows'
	// cascade offset would push its bottom under the taskbar again.
	if wa, ok := workArea(); ok && wa.bottom > wa.top && uintptr(wa.bottom-wa.top) < height {
		height = uintptr(wa.bottom - wa.top)
		x, y = uintptr(wa.left+(wa.right-wa.left-int32(width))/2), uintptr(wa.top)
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow&^wsMaximizeBox, x, y, width, height, 0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	w.hwnd = hwnd

	c := edge.NewChromium()
	w.chromium = c
	c.DataPath = w.dataPath()
	c.AdditionalBrowserArgs = uiBrowserArgs
	// go-webview2 exits the process right after this callback (except when the
	// environment cannot be created: then Embed waits, see uiEmbedWait).
	c.SetErrorCallback(func(err error) {
		uiNote("webview2: %v", err)
		writeUIDiag()
	})
	c.NavigationCompletedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) {
		uiNote("webview2: page loaded")
	}
	reloads := 0
	c.ProcessFailedCallback = func(wv *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
		kind, _ := args.GetProcessFailedKind()
		uiNote("webview2: browser process failed (kind %d)", kind)
		// The page's renderer ended: the window would stay empty. Load the page
		// again; it asks Go for its state once more (init).
		if kind == edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED && reloads < 3 {
			reloads++
			// Not c.NavigateToString: on an error it ends the whole process.
			if err := wv.NavigateToString(withFonts(uiPage)); err != nil {
				uiNote("webview2: reload: %v", err)
			}
		}
	}
	c.MessageCallback = func(msg string, _ *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		// Our page posts "init" as soon as it loads. Should the view ever end up
		// on another document (e.g. something dropped onto the window), that
		// document must not reach the bridge.
		if w.failed != nil {
			return // the watchdog gave up on this view; its window is gone
		}
		src, err := args.GetSource()
		if err != nil {
			return
		}
		if w.origin == "" {
			w.origin = src
		} else if src != w.origin {
			log.Printf("webview2: message from unexpected source ignored")
			return
		}
		w.handle(msg)
	}
	// Show the window before the view is created: a view made in a hidden
	// parent starts as hidden and relies on a later visibility change.
	bringToFront(hwnd)
	procUpdateWindow.Call(hwnd)
	embedGuard := time.AfterFunc(uiEmbedWait, func() {
		w.mu.Lock()
		stuck := !w.embedded
		w.mu.Unlock()
		if stuck {
			uiNote("webview2: no view after %v", uiEmbedWait)
			writeUIDiag()
			msgBox(w.t, w.t.windowFailed(uiDiagPath()), windows.MB_OK|windows.MB_ICONWARNING)
			os.Exit(1)
		}
	})
	defer embedGuard.Stop()
	if !c.Embed(hwnd) {
		// No WM_QUIT for this window: it would sit in the queue and make the
		// fallback dialog's MessageBox return at once.
		w.mu.Lock()
		w.noQuit = true
		w.mu.Unlock()
		procDestroyWindow.Call(hwnd)
		return errors.New("WebView2 could not be embedded")
	}
	w.mu.Lock()
	w.embedded = true
	w.mu.Unlock()
	if err := c.AllowExternalDrag(false); err != nil {
		log.Printf("webview2: external drop: %v", err)
	}
	if s, err := c.GetSettings(); err == nil {
		s.PutAreDevToolsEnabled(false)
		s.PutAreDefaultContextMenusEnabled(false)
		s.PutIsStatusBarEnabled(false)
		s.PutIsZoomControlEnabled(false)
		s.PutAreBrowserAcceleratorKeysEnabled(false)
		s.PutIsSwipeNavigationEnabled(false)
		s.PutAreHostObjectsAllowed(false)
	}
	c.Resize()
	c.NavigateToString(withFonts(uiPage))
	if err := c.Show(); err != nil { // visible is the default; say it anyway
		uiNote("webview2: show: %v", err)
	}
	var rc struct{ left, top, right, bottom int32 }
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if b, err := c.GetController().GetBounds(); err == nil {
		uiNote("window: client %dx%d, view %dx%d at %d,%d, dpi %d", rc.right-rc.left, rc.bottom-rc.top,
			b.Right-b.Left, b.Bottom-b.Top, b.Left, b.Top, dpi)
	}

	w.mu.Lock()
	w.looping = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.looping = false
		calls := w.calls
		w.calls = nil
		w.mu.Unlock()
		for _, f := range calls {
			f()
		}
	}()
	// Registered after the defer above, so it is stopped before looping goes
	// false; it only ever acts through the running loop (UI thread).
	watchdog := time.AfterFunc(uiWatchdog, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.looping {
			return
		}
		w.calls = append(w.calls, func() {
			if !w.framed && w.failed == nil {
				w.failed = errNoPage
				c.ShuttingDown() // no more scripts into a view whose window is gone
				procDestroyWindow.Call(w.hwnd)
			}
		})
		procPostMessageW.Call(w.hwnd, wmApp, 0, 0)
	})
	defer watchdog.Stop()
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if w.mode == uiInstall {
		os.RemoveAll(w.dataPath()) // best effort; WebView2 may still hold a file for a moment
	}
	return w.failed
}

var procCreateSolidBrush = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateSolidBrush")

// canvasBrush: the page's --canvas colour (#030504) as a GDI brush (COLORREF
// is 0x00BBGGRR). Lives as long as the process, like the window class.
func canvasBrush() uintptr {
	b, _, _ := procCreateSolidBrush.Call(0x040503)
	return b
}

// dataPath: before the install nothing may land in the program folder, so the
// first window keeps its WebView2 data in %TEMP%.
func (w *window) dataPath() string {
	if w.mode == uiInstall {
		return filepath.Join(os.TempDir(), "CoreStarter-setup-ui")
	}
	return w.p.uiData
}

func uiWndProc(hwnd windows.Handle, msg uint32, wp, lp uintptr) uintptr {
	w := theWindow
	switch msg {
	case wmSize:
		if w != nil && w.chromium != nil {
			w.chromium.Resize()
		}
		return 0
	case wmApp:
		if w != nil {
			w.flushReplies()
		}
		return 0
	case wmShowView:
		if w != nil {
			view := "form"
			if wp == viewMiner {
				view = "miner"
			}
			w.mu.Lock()
			w.view = view // a page still loading picks it up with "init"
			w.mu.Unlock()
			if w.chromium != nil && w.origin != "" {
				w.chromium.Eval(`window.showView && showView("` + view + `")`)
			}
		}
		return 0
	case wmDpiChanged:
		// Moved to a monitor with another scale: take the size Windows suggests.
		r := (*struct{ left, top, right, bottom int32 })(unsafe.Add(nil, lp)) // RECT* from Windows
		procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(r.left), uintptr(r.top),
			uintptr(r.right-r.left), uintptr(r.bottom-r.top), swpNoZOrder|swpNoActivate)
		return 0
	case wmClose:
		if w != nil {
			w.mu.Lock()
			if !w.embedded && !w.noQuit {
				// go-webview2's Embed still runs its own message loop and cannot
				// be left early; closing means leaving the program (nothing is
				// saved before the view exists).
				w.mu.Unlock()
				os.Exit(0)
			}
			busy := w.saving
			w.mu.Unlock()
			if busy {
				w.mu.Lock()
				w.closeAsked = true
				w.mu.Unlock()
				return 0 // the page shows the progress; the close follows once it is done
			}
		}
	case wmDestroy:
		quit := true
		if w != nil {
			w.mu.Lock()
			quit = !w.noQuit
			w.mu.Unlock()
		}
		if quit {
			procPostQuitMessage.Call(0)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return r
}

// onMainThread runs f on the main thread: directly when called from there
// (no window loop running), else queued for the window's message loop, which
// runs on the main thread.
func onMainThread(f func()) {
	if w := theWindow; w != nil {
		w.mu.Lock()
		if w.looping {
			w.calls = append(w.calls, f)
			w.mu.Unlock()
			procPostMessageW.Call(w.hwnd, wmApp, 0, 0)
			return
		}
		w.mu.Unlock()
	}
	f()
}

// wake makes the UI thread run queued replies and calls (flushReplies).
func (w *window) wake() { procPostMessageW.Call(w.hwnd, wmApp, 0, 0) }

func (w *window) eval(js string) { w.chromium.Eval(js) }

func (w *window) closeWindow() { procPostMessageW.Call(w.hwnd, wmClose, 0, 0) }

// firstFrame re-applies the bounds once more, the cheap cure for a view that
// only shows after the window was moved or resized.
func (w *window) firstFrame() {
	w.chromium.Resize()
	w.chromium.NotifyParentWindowPositionChanged()
}

// licenseDir: the licence texts lie next to gocore in the program folder.
func licenseDir(p paths) string { return p.bin }

func askTrayReload() { signalEvent(reloadEventName) }

func askTrayQuit() bool { return signalEvent(quitEventName) }

// startUninstall runs the installed exe's own uninstall: it asks, closes this
// window and stops the tray.
func startUninstall(p paths) error {
	cmd := exec.Command(p.exe, flagUninst)
	cmd.Dir = p.root
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// trayAccepting: a running tray that still applies reloads.
func trayAccepting() bool {
	name, _ := windows.UTF16PtrFromString(acceptingName)
	h, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

func startTray(p paths) error {
	cmd := exec.Command(p.exe, flagHandover)
	cmd.Dir = p.root
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// uiWindowOpen reports whether a CoreStarter window exists.
func uiWindowOpen() bool {
	cls, _ := windows.UTF16PtrFromString(uiClassName)
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	return h != 0
}

// closeSettingsWindow closes an open CoreStarter window of another process and
// waits until that process is gone: it runs from the installed exe and its
// WebView2 holds files in the program folder, which an update or uninstall
// must replace or delete.
func closeSettingsWindow() {
	cls, _ := windows.UTF16PtrFromString(uiClassName)
	for i := 0; i < 3; i++ {
		h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			return
		}
		var pid uint32
		procGetWindowTPID.Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid == windows.GetCurrentProcessId() {
			return
		}
		proc, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			return
		}
		procPostMessageW.Call(h, wmClose, 0, 0)
		// A window in the middle of saving closes once that is done (closeAsked).
		if r, _ := windows.WaitForSingleObject(proc, 60_000); r != windows.WAIT_OBJECT_0 {
			windows.TerminateProcess(proc, 1)
			windows.WaitForSingleObject(proc, 5_000)
		}
		windows.CloseHandle(proc)
	}
}

// bringToFront shows h restored and in front of the other windows.
//
// A start by another program can ask for a minimized window: an antivirus
// that scans the exe first starts it itself afterwards (seen with Norton on a
// real PC: the window sat in the taskbar). ShowWindow ignores its argument on
// a process's first call when the launcher passed a show mode, so it is
// called twice. Windows also only lets the foreground process take the focus;
// a start that waited for a scan has lost that right, so the input of the
// current foreground window is attached for the moment of the switch, and a
// brief top-most step lifts the window even where the focus is refused.
// uiShowCalls/uiShowCmd are vars for the test that replays the old single
// ShowWindow(SW_SHOW) to show the launcher's mode really wins there.
var (
	uiShowCalls = 2
	uiShowCmd   = uintptr(swRestore)
)

func bringToFront(h uintptr) {
	for i := 0; i < uiShowCalls; i++ {
		procShowWindow.Call(h, uiShowCmd)
	}
	fg, _, _ := procGetForegroundWnd.Call()
	if fg == h {
		return
	}
	self := uintptr(windows.GetCurrentThreadId())
	if fg != 0 {
		if other, _, _ := procGetWindowTPID.Call(fg, 0); other != 0 && other != self {
			procAttachThreadIn.Call(other, self, 1)
			defer procAttachThreadIn.Call(other, self, 0)
		}
	}
	procSetWindowPos.Call(h, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
	procSetWindowPos.Call(h, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
	procBringWindowToTop.Call(h)
	procSetForegroundWnd.Call(h)
}
