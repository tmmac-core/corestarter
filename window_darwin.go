//go:build darwin

package main

/*
#include <stdlib.h>
#include "webview_darwin.h"
*/
import "C"

import (
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// nativeWin: AppKit and WebKit state lives in webview_darwin.m (one window per process).
type nativeWin struct{}

var theWindow *window

var appReady bool

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
	initApp(t)
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
	C.cs_run()
	w.mu.Lock()
	w.looping = false
	calls := w.calls
	w.calls = nil
	w.mu.Unlock()
	for _, f := range calls {
		f()
	}
	return w.saved
}

// initApp makes this process the window app with its menus, once. Only the
// window process: the helper's menu bar app (systray) has its own.
func initApp(t texts) {
	if appReady {
		return
	}
	l, free := cLabels(t)
	defer free()
	appReady = C.cs_init(l) != 0
}

// setLabels: the dialog buttons in the user's language.
func setLabels(t texts) {
	l, free := cLabels(t)
	defer free()
	C.cs_labels(l)
}

// cLabels: the menu and button labels as C strings; webview_darwin.m copies
// them, free releases them afterwards.
func cLabels(t texts) (**C.char, func()) {
	labels := t.macMenu()
	cs := make([]*C.char, len(labels))
	for i, s := range labels {
		cs[i] = C.CString(s)
	}
	arr := C.CBytes(unsafe.Slice((*byte)(unsafe.Pointer(&cs[0])), len(cs)*int(unsafe.Sizeof(cs[0]))))
	return (**C.char)(arr), func() {
		for _, c := range cs {
			C.free(unsafe.Pointer(c))
		}
		C.free(arr)
	}
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
	s := C.CString(js)
	defer C.free(unsafe.Pointer(s))
	C.cs_eval(s)
}

func (w *window) closeWindow() { C.cs_close() }

// firstFrame: nothing to remember on macOS (no fallback drawing modes).
func (w *window) firstFrame() {}

type msgKind int

const (
	msgInfo msgKind = iota
	msgWarn
	msgError
	msgQuestion
)

// msgBox shows a modal message; true for OK/Yes. On the main thread, or from
// a goroutine while the main thread runs AppKit (window or menu bar).
func msgBox(t texts, body string, kind msgKind) bool {
	setLabels(t)
	title, text := C.CString(t.appName), C.CString(body)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(text))
	return C.cs_dialog(title, text, C.int(kind)) == 1
}

// licenseDir: the licence texts inside the app bundle.
func licenseDir(p paths) string { return filepath.Join(p.bin, "Contents", "Resources", "licenses") }

// startUninstall: after a question, stop the background part, remove its
// launchd job and move the app to the Trash. The data stays (as on Windows);
// the page says where.
func startUninstall(p paths) error {
	if t := theWindowTexts(); !msgBox(t, t.mac().uninstallConfirm, msgQuestion) {
		return errCancelled
	}
	go func() {
		if trayAccepting() || trayQuitting() {
			askTrayQuit()
			waitTrayGone(gracefulStopWait + time.Minute)
		}
		removeJob(p)
		stopLeftoverNode(p)
		killProcessesOf(p.miner, "")
		if !strings.HasSuffix(p.bin, ".app") {
			log.Printf("uninstall: %s is no app bundle, left in place", p.bin)
		} else if !recycle(p.bin) {
			log.Printf("uninstall: could not move %s to the Trash", p.bin)
			shellOpen("/Applications") // the user can still drag it there
		}
		if w := theWindow; w != nil {
			w.closeWindow()
		}
	}()
	return nil
}

var errCancelled = errors.New("cancelled")

// stopLeftoverNode: a gocore on our data that outlived the helper (launchd
// kills the helper after ExitTimeOut, gocore has its own process group) is
// stopped like the helper would, before its program goes to the Trash.
func stopLeftoverNode(p paths) {
	pid, start, _, err := findNode(p)
	if err != nil || pid == 0 {
		return
	}
	log.Printf("uninstall: stopping leftover gocore pid %d", pid)
	unix.Kill(pid, unix.SIGINT)
	for deadline := time.Now().Add(gracefulStopWait); time.Now().Before(deadline); {
		if !sameProcess(pid, start) {
			return
		}
		time.Sleep(time.Second)
	}
	if sameProcess(pid, start) {
		unix.Kill(pid, unix.SIGKILL)
	}
}

func theWindowTexts() texts {
	if w := theWindow; w != nil {
		return w.t
	}
	return systemTexts()
}
