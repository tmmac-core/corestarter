//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework UserNotifications -framework WebKit
#include <stdlib.h>
#include "cocoa_darwin.h"
*/
import "C"

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// systemTexts: the first language in System Settings → General → Language &
// Region (apps from the Finder get no LANG), English for any other.
func systemTexts() texts {
	buf := make([]byte, 64)
	if C.cs_lang((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) == 0 {
		return textsEN
	}
	code := strings.ToLower(C.GoString((*C.char)(unsafe.Pointer(&buf[0]))))
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}
	if t, ok := textsByLang[code]; ok {
		return *t
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
		parent := filepath.Dir(dir)
		if !os.IsNotExist(err) || parent == dir {
			return 0, err
		}
		dir = parent
	}
}

// shellOpen opens a folder in the Finder or a web page in the browser.
func shellOpen(path string) {
	cmd := exec.Command("/usr/bin/open", path)
	if err := cmd.Start(); err != nil {
		log.Printf("open: %v", err)
		return
	}
	go cmd.Wait()
}

// warnUser shows a notification; the helper runs without a window and must
// not block on a dialog.
func warnUser(t texts, text string) {
	title, body := C.CString(t.appName), C.CString(text)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(body))
	C.cs_notify(title, body)
}

// totalMemory is the installed RAM in bytes, 0 if unknown.
func totalMemory() uint64 {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return n
}

// onARM64: the Intel build on a Mac with Apple silicon, where node and miner
// run translated by Rosetta and the page tells the user to get the Apple
// silicon build. hw.optional.arm64 is 1 there even inside a translated
// process (and missing on Intel); runtime.GOARCH alone cannot tell.
func onARM64() bool {
	if runtime.GOARCH != "amd64" {
		return false
	}
	n, err := unix.SysctlUint32("hw.optional.arm64")
	return err == nil && n == 1
}

func childEnv() []string { return os.Environ() }

// excludeFromBackup keeps the chain (many GB, rebuilt from the network) out
// of Time Machine and Spotlight. Called once the user agreed and the node
// starts; a failure only costs backup space.
func excludeFromBackup(dir string) {
	marker := filepath.Join(dir, ".metadata_never_index")
	if _, err := os.Stat(marker); err == nil {
		return // done before
	}
	if out, err := exec.Command("/usr/bin/tmutil", "addexclusion", dir).CombinedOutput(); err != nil {
		log.Printf("tmutil addexclusion: %v: %s", err, strings.TrimSpace(string(out)))
	}
	os.WriteFile(marker, nil, 0o600)
}

// recycle moves path (the app) to the Trash.
func recycle(path string) bool {
	s := C.CString(path)
	defer C.free(unsafe.Pointer(s))
	return C.cs_recycle(s) == 1
}

// notifySetup asks once for permission to show notifications (helper only:
// it runs after the user agreed).
func notifySetup() { C.cs_notify_setup() }

// startDetached starts a helper program without waiting for it.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
