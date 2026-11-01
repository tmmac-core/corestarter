//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	appID        = "core-starter" // package, .desktop file, systemd unit, Wayland app id
	appExeName   = "core-starter"
	minerExeName = "core-miner"
	gocoreName   = "gocore"
	runtimeDir   = "runtime" // bundled glibc for an official gocore built against a newer one
	ipcName      = "gocore.ipc"
	licensesPath = "/usr/share/doc/core-starter"
)

// appPaths follows the XDG base directories: settings in ~/.config, state and
// logs in ~/.local/state, the chain in ~/.local/share. The programs come from
// the package folder (the folder of this executable).
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
	run, err := privateRunDir()
	if err != nil {
		return paths{}, err
	}
	p := pathsAt(
		filepath.Join(xdgDir("XDG_DATA_HOME", home, ".local/share"), appID),
		filepath.Join(xdgDir("XDG_CONFIG_HOME", home, ".config"), appID),
		filepath.Join(xdgDir("XDG_STATE_HOME", home, ".local/state"), appID),
		run, filepath.Dir(self),
	)
	p.exe = self
	return p, nil
}

func pathsAt(data, config, state, run, bin string) paths {
	p := paths{
		root:     data,
		bin:      bin,
		data:     filepath.Join(data, "data"),
		logs:     filepath.Join(state, "logs"),
		settings: filepath.Join(config, "settings.json"),
		status:   filepath.Join(state, "status.json"),
		pidFile:  filepath.Join(state, "gocore.pid"),
		run:      run,
		glibc:    filepath.Join(bin, runtimeDir),
	}
	p.exe = filepath.Join(bin, appExeName)
	p.keystore = filepath.Join(p.data, "keystore")
	p.gocore = filepath.Join(bin, gocoreName)
	p.nodeLog = filepath.Join(p.logs, "gocore.log")
	p.miner = filepath.Join(bin, minerExeName)
	p.minerLog = filepath.Join(p.logs, "miner.log")
	return p
}

// xdgDir: the variable if it holds an absolute path (the spec says to ignore
// relative ones), else the default under home.
func xdgDir(env, home, def string) string {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home, def)
}

// privateRunDir is a folder only this user can enter, for sockets and locks:
// $XDG_RUNTIME_DIR/core-starter, or /tmp/core-starter-<uid> on systems
// without one (no systemd-logind). In /tmp anyone could have created that
// name first, so it must be a real directory of ours with mode 0700.
func privateRunDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	dir := filepath.Join(base, appID)
	if !filepath.IsAbs(base) {
		dir = filepath.Join(os.TempDir(), appID+"-"+strconv.Itoa(os.Getuid()))
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("%s is not a private folder of this user", dir)
	}
	return dir, nil
}

// ipcPath is where gocore serves its IPC socket. The folder is private, so no
// random name is needed; dialNode still checks the server's process id.
func ipcPath(p paths) string { return filepath.Join(p.run, ipcName) }
