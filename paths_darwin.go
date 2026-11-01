//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <CoreFoundation/CoreFoundation.h>

// Security.framework, no public header (SecTranslocate.h, macOS 10.12+).
extern CFURLRef SecTranslocateCreateOriginalPathForURL(CFURLRef translocatedPath, CFErrorRef *error);

// cs_original_path: where a translocated bundle really lies; 0 if unknown.
static int cs_original_path(const char *path, char *out, long size) {
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, (CFIndex)strlen(path), true);
	if (!url) return 0;
	CFURLRef orig = SecTranslocateCreateOriginalPathForURL(url, NULL);
	CFRelease(url);
	if (!orig) return 0;
	Boolean ok = CFURLGetFileSystemRepresentation(orig, true, (UInt8 *)out, (CFIndex)size);
	CFRelease(orig);
	return ok ? 1 : 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	appID        = "info.tmmac.core-starter" // bundle id of the window app; also the launchd label
	helperID     = appID + ".helper"
	appDirName   = "Core Starter" // folder under Application Support and Logs
	appExeName   = "core-starter"
	minerExeName = "core-miner"
	gocoreName   = "gocore"
	ipcName      = "gocore.ipc"
	helperApp    = "Contents/Library/LoginItems/CoreStarter Helper.app"
)

// appPaths: everything per user under ~/Library (settings, state, run folder
// and chain in Application Support, logs where the Console app finds them).
// The programs come from the app bundle, which is never written to.
func appPaths() (paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths{}, err
	}
	self, err := os.Executable()
	if err != nil {
		return paths{}, err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	cache, err := userCacheDir()
	if err != nil {
		return paths{}, err
	}
	p := pathsAt(filepath.Join(home, "Library", "Application Support", appDirName),
		filepath.Join(home, "Library", "Logs", appDirName), untranslocated(bundleOf(self)))
	// Sockets and locks: in the user's cache folder (/var/folders/…/C), the
	// counterpart of $XDG_RUNTIME_DIR. Not $TMPDIR: macOS deletes files there
	// that were not accessed for 3 days (man confstr), a long-running helper
	// would lose its lock. Not Application Support: nothing is written there
	// before the user agreed.
	p.run = filepath.Join(cache, appID)
	if relocatePaths != nil {
		relocatePaths(&p)
	}
	if err := privateDir(p.run); err != nil {
		return paths{}, err
	}
	return p, nil
}

// relocatePaths is only set by the test build (e2e_darwin.go).
var relocatePaths func(*paths)

// userCacheDir: the per-user cache folder, from confstr (the environment of a
// launchd job may lack TMPDIR).
func userCacheDir() (string, error) {
	buf := make([]byte, 1024)
	n := C.confstr(C._CS_DARWIN_USER_CACHE_DIR, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return "", errors.New("no user cache folder")
	}
	return filepath.Clean(string(buf[:n-1])), nil
}

// bundleOf returns the window app's bundle for a program inside it, also for
// the helper's copy (…/CoreStarter.app/Contents/Library/LoginItems/Core
// Starter Helper.app/Contents/MacOS/core-starter). Outside a bundle (tests,
// a build folder) it is the program's folder.
func bundleOf(exe string) string {
	if i := strings.Index(exe, "/"+helperApp+"/"); i >= 0 {
		return exe[:i]
	}
	if dir := filepath.Dir(exe); filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		return filepath.Dir(filepath.Dir(dir))
	}
	return filepath.Dir(exe)
}

// untranslocated: macOS starts a downloaded (quarantined) app from a
// read-only copy at a random path (App Translocation), even when it lies in
// /Applications, until the Finder moved it there. The launchd job and the
// programs started from it must use the real place, so ask macOS for it.
func untranslocated(bundle string) string {
	if !strings.Contains(bundle, "/AppTranslocation/") {
		return bundle
	}
	cpath := C.CString(bundle)
	defer C.free(unsafe.Pointer(cpath))
	buf := make([]byte, 4096)
	if C.cs_original_path(cpath, (*C.char)(unsafe.Pointer(&buf[0])), C.long(len(buf))) == 0 {
		return bundle
	}
	orig := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
	if _, err := os.Stat(filepath.Join(orig, "Contents", "MacOS", appExeName)); err != nil {
		return bundle
	}
	return orig
}

// pathsAt: root holds settings, state, the run folder and the chain; bundle
// is CoreStarter.app (or a plain folder with the programs side by side).
func pathsAt(root, logs, bundle string) paths {
	p := paths{
		root:     root,
		bin:      bundle,
		data:     filepath.Join(root, "data"),
		logs:     logs,
		settings: filepath.Join(root, "settings.json"),
		status:   filepath.Join(root, "status.json"),
		pidFile:  filepath.Join(root, "gocore.pid"),
	}
	macos := bundle
	if strings.HasSuffix(bundle, ".app") {
		macos = filepath.Join(bundle, "Contents", "MacOS")
	}
	p.exe = filepath.Join(macos, appExeName)
	p.gocore = filepath.Join(macos, gocoreName)
	p.miner = filepath.Join(macos, minerExeName)
	p.keystore = filepath.Join(p.data, "keystore")
	p.nodeLog = filepath.Join(p.logs, "gocore.log")
	p.minerLog = filepath.Join(p.logs, "miner.log")
	return p
}

// helperExe is the background part's program: the copy in the helper bundle,
// so the menu bar app is its own app for macOS (a double click on Core
// Starter then opens the window instead of activating the helper).
func helperExe(p paths) string {
	if strings.HasSuffix(p.bin, ".app") {
		return filepath.Join(p.bin, helperApp, "Contents", "MacOS", appExeName)
	}
	return p.exe
}

// privateDir creates dir (mode 0700) and checks that it is a real folder of
// this user that nobody else can enter: it holds sockets and the keystore.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is not a folder of this user", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// ipcPath is where gocore serves its IPC socket, in the private run folder.
func ipcPath(p paths) string { return filepath.Join(p.run, ipcName) }
