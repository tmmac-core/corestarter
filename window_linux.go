//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include "webview_linux.h"
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// nativeWin: GTK and WebKit state lives in webview_linux.c (one window per process).
type nativeWin struct{}

var theWindow *window

// uiWatchdog: the page reports its first drawn frame (requestAnimationFrame).
// None by then means the user sees nothing. Some GPU drivers (NVIDIA's own,
// virtual machines without 3D) leave WebKitGTK's view empty; the window then
// starts again with WebKit's safer drawing modes (webkitStage).
var uiWatchdog = 45 * time.Second

// webkitStage: 0 normal, 1 without the DMA-BUF renderer, 2 without
// accelerated compositing. Passed to the restarted process in its environment.
const webkitStageEnv = "CORE_STARTER_WEBKIT_STAGE"

var webkitStages = [][]string{
	nil,
	{"WEBKIT_DISABLE_DMABUF_RENDERER=1"},
	{"WEBKIT_DISABLE_DMABUF_RENDERER=1", "WEBKIT_DISABLE_COMPOSITING_MODE=1"},
}

var gtkReady bool

// runWindow shows the start/settings window and returns whether the user saved.
// view "miner" opens on the live miner view; an open window switches to it.
func runWindow(t texts, p paths, mode uiMode, view string, onSave func(Settings) error) bool {
	lock, ok := lockFile(windowLockName, 0)
	if !ok {
		// Already open: bring it to the front (and switch the view) instead of a second window.
		v := "form"
		if view == "miner" {
			v = "miner"
		}
		askControl(windowSockName, ctlRequest{Cmd: "show", View: v})
		return false
	}
	defer lock.Close()
	if os.Getenv(webkitStageEnv) == "" {
		applyStage(savedStage(p)) // a mode that worked here before: no 45 s wait again
	}
	if !initGTK() {
		fmt.Fprintln(os.Stderr, "CoreStarter: no display (DISPLAY or WAYLAND_DISPLAY) to show the window on.")
		return false
	}
	w := &window{t: t, p: p, mode: mode, view: view, onSave: onSave}
	theWindow = w
	err := serveControl(windowSockName, func(r ctlRequest) ctlReply {
		if r.Cmd != "show" {
			return ctlReply{}
		}
		w.mu.Lock()
		w.view = r.View // a page still loading picks it up with "init"
		view := "form"
		if r.View == "miner" {
			view = "miner"
		}
		w.calls = append(w.calls, func() {
			C.cs_present()
			if w.origin != "" {
				w.eval(`window.showView && showView("` + view + `")`)
			}
		})
		w.mu.Unlock()
		w.wake()
		return ctlReply{OK: true}
	})
	if err != nil {
		log.Printf("window socket: %v", err)
	}

	title := C.CString(t.appName)
	defer C.free(unsafe.Pointer(title))
	icon := C.CBytes(uiIcon)
	defer C.free(icon)
	C.cs_window(title, 560, 780, icon, C.int(len(uiIcon)))
	page := C.CString(withFonts(uiPage))
	defer C.free(unsafe.Pointer(page))
	C.cs_load(page)

	w.mu.Lock()
	w.looping = true
	w.mu.Unlock()
	watchdog := time.AfterFunc(uiWatchdog, func() {
		w.mu.Lock()
		w.calls = append(w.calls, func() {
			if !w.framed && w.failed == nil {
				w.failed = errNoPage
				C.cs_destroy()
			}
		})
		w.mu.Unlock()
		w.wake()
	})
	C.cs_run()
	watchdog.Stop()
	w.mu.Lock()
	w.looping = false
	calls := w.calls
	w.calls = nil
	w.mu.Unlock()
	for _, f := range calls {
		f()
	}
	if w.failed != nil {
		uiNote("window: %v (webkit stage %d)", w.failed, webkitStage())
		lock.Close()
		onWindowFailed(t, p)
		return false
	}
	return w.saved
}

// onWindowFailed: retryWindow; the tests replace it (it re-executes the process).
var onWindowFailed = retryWindow

func webkitStage() int {
	n, _ := strconv.Atoi(os.Getenv(webkitStageEnv))
	return min(max(n, 0), len(webkitStages)-1)
}

// retryWindow starts this program again with the next safer WebKit mode, or
// says that the window cannot be shown here.
func retryWindow(t texts, p paths) {
	next := webkitStage() + 1
	if next >= len(webkitStages) {
		msgBox(t, t.windowFailed(filepath.Join(p.logs, "starter.log")), msgWarn)
		return
	}
	env := append(os.Environ(), webkitStageEnv+"="+strconv.Itoa(next))
	env = append(env, webkitStages[next]...)
	self, err := os.Executable()
	if err == nil {
		err = syscall.Exec(self, os.Args, env)
	}
	log.Printf("restart window: %v", err)
}

func initGTK() bool {
	if !gtkReady {
		gtkReady = C.cs_init() != 0
	}
	return gtkReady
}

//export goMessage
func goMessage(s *C.char) {
	if w := theWindow; w != nil && w.failed == nil {
		w.origin = "page" // messages come only from our page (navigation is blocked)
		w.handle(C.GoString(s))
	}
}

//export goWake
func goWake() {
	if w := theWindow; w != nil {
		w.flushReplies()
	}
}

//export goNote
func goNote(s *C.char) { uiNote("%s", C.GoString(s)) }

// goCloseRequested: closing while an install/save runs would cut it off; the
// close follows once it is done (closeAsked).
//
//export goCloseRequested
func goCloseRequested() C.int {
	w := theWindow
	if w == nil {
		return 1
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.saving {
		w.closeAsked = true
		return 0
	}
	return 1
}

// wake makes the main loop run queued replies and calls (flushReplies).
func (w *window) wake() { C.cs_wake() }

func (w *window) eval(js string) {
	// Raw UTF-8 (asciiJS) and long scripts (splitScript) crash WebKitGTK 2.52.
	for _, piece := range splitScript(asciiJS(js)) {
		s := C.CString(piece)
		C.cs_eval(s)
		C.free(unsafe.Pointer(s))
	}
}

func (w *window) closeWindow() { C.cs_close() }

// firstFrame: the page drew. A safer WebKit mode that was needed for that is
// remembered (only once the settings exist: before consent nothing is written).
func (w *window) firstFrame() {
	if n := webkitStage(); n > 0 && n != savedStage(w.p) {
		if _, err := os.Stat(w.p.settings); err == nil {
			os.WriteFile(stageFile(w.p), []byte(strconv.Itoa(n)), 0o600)
		}
	}
}

func stageFile(p paths) string { return filepath.Join(filepath.Dir(p.status), "webkit-stage") }

func savedStage(p paths) int {
	b, _ := os.ReadFile(stageFile(p))
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return min(max(n, 0), len(webkitStages)-1)
}

// applyStage sets a WebKit mode for this process (before WebKit starts).
func applyStage(n int) {
	if n <= 0 {
		return
	}
	os.Setenv(webkitStageEnv, strconv.Itoa(n))
	for _, kv := range webkitStages[n] {
		k, v, _ := strings.Cut(kv, "=")
		os.Setenv(k, v)
	}
}

type msgKind int

const (
	msgInfo msgKind = iota
	msgWarn
	msgError
	msgQuestion
)

// msgBox shows a modal message on the main thread; true for OK/Yes. Without a
// display the text goes to stderr.
func msgBox(t texts, body string, kind msgKind) bool {
	if !initGTK() {
		fmt.Fprintln(os.Stderr, t.appName+": "+body)
		return false
	}
	title, text := C.CString(t.appName), C.CString(body)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(text))
	return C.cs_dialog(title, text, C.int(kind)) == 1
}

// licenseDir: the package puts the licence texts with its documentation.
func licenseDir(paths) string { return licensesPath }

// startUninstall: removing the program is the package manager's job; the
// page shows how instead of offering the button.
func startUninstall(paths) error { return errUninstallByPackage }
