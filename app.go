package main

import (
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	appVersion    = "1.0.0"
	needGB        = 15 // ~12 GB peak while fast-syncing (measured 2026-10-17) plus headroom
	maxAppLog     = 5 << 20
	flagUI        = "--ui"         // open the settings window (from the tray or the Start menu)
	flagViewMiner = "--view=miner" // with --ui: open on the live miner view
)

func removeChildrenExcept(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), keep) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// dirHasFiles errs on the safe side: unreadable counts as "has files".
func dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return len(entries) > 0
}

func setupLog(p paths) {
	if err := os.MkdirAll(p.logs, 0o700); err != nil {
		return
	}
	path := filepath.Join(p.logs, "starter.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxAppLog {
		os.Remove(path)
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(f)
	}
}

func copyFile(src, dst string) error {
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return renameRetry(tmp, dst) // a virus scanner may still be reading the fresh file
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func formatGB(t texts, n uint64) string {
	s := strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64)
	if t.thousandsSep == "." {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

func dirSize(dir string) uint64 {
	var n uint64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += uint64(fi.Size())
			}
		}
		return nil
	})
	return n
}
