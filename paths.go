package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// paths are all per user, so no administrator rights are needed: under
// %LOCALAPPDATA% on Windows, the XDG folders on Linux (pathsAt per system).
type paths struct {
	root     string // Windows: %LOCALAPPDATA%\CoreStarter; Linux: ~/.local/share/core-starter
	exe      string // this program (Windows: the installed copy)
	bin      string // gocore + what it needs (Windows: runtime DLLs; Linux: the package folder)
	data     string // blockchain data (gocore --datadir)
	keystore string // account keys gocore may keep inside data; never deleted
	logs     string
	gocore   string
	nodeLog  string
	pidFile  string // the running gocore ("<pid>"; Linux adds its start time)
	pipeFile string // random id of gocore's IPC endpoint for the current run
	version  string // installed starter version (Windows)
	token    string // one-time secret that authorises the uninstall helper (Windows)
	settings string // the user's choices (written only by the window)
	status   string // current state for the window (written only by the tray)
	miner    string // core-miner
	minerLog string
	uiData   string // WebView2 user data folder (Windows)
	run      string // Linux: private folder for sockets and locks ($XDG_RUNTIME_DIR)
	glibc    string // Linux: bundled glibc runtime for an official gocore that needs a newer one
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return renameRetry(tmp, path)
}
