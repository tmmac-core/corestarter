//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	appFolder  = "CoreStarter"
	appExeName = "CoreStarter.exe"
)

func appPaths() (paths, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return paths{}, err
	}
	return pathsAt(filepath.Join(base, appFolder)), nil
}

func pathsAt(root string) paths {
	p := paths{
		root: root,
		exe:  filepath.Join(root, appExeName),
		bin:  filepath.Join(root, "bin"),
		data: filepath.Join(root, "data"),
		logs: filepath.Join(root, "logs"),
	}
	p.keystore = filepath.Join(p.data, "keystore")
	p.gocore = filepath.Join(p.bin, "gocore.exe")
	p.nodeLog = filepath.Join(p.logs, "gocore.log")
	p.pidFile = filepath.Join(root, "gocore.pid")
	p.pipeFile = filepath.Join(root, "pipe.id")
	p.version = filepath.Join(root, "version.txt")
	p.token = filepath.Join(root, "cleanup.token")
	p.settings = filepath.Join(root, "settings.json")
	p.status = filepath.Join(root, "status.json")
	p.miner = filepath.Join(p.bin, "core-miner.exe")
	p.minerLog = filepath.Join(p.logs, "miner.log")
	p.uiData = filepath.Join(root, "ui")
	return p
}

// runningFromInstallDir reports whether this process is the installed copy.
func (p paths) runningFromInstallDir() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	return strings.EqualFold(filepath.Clean(self), filepath.Clean(p.exe))
}

// Pipe names are visible to every user on the PC, so the random id is not a
// secret; it only keeps runs apart. Trust comes from checking the pipe server's
// process id (dialNode).
func pipeName(id string) string { return "core-starter-" + id + ".ipc" }

// newPipeName creates the id for a new gocore run.
func newPipeName(p paths) (string, error) {
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(p.pipeFile, []byte(id)); err != nil {
		return "", err
	}
	return pipeName(id), nil
}

// currentPipeName reads the id of the running gocore; it never creates one.
func currentPipeName(p paths) (string, error) {
	raw, err := os.ReadFile(p.pipeFile)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(raw))
	if len(id) != 32 {
		return "", errors.New("invalid pipe id")
	}
	return pipeName(id), nil
}
