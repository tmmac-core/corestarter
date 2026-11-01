//go:build windows

package main

import (
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The payload is produced by scripts/build.sh: the unmodified official gocore.exe
// plus the MinGW runtime DLLs it needs, each gzip-compressed. payloadFiles (in the
// generated payload_gen_windows.go) pins the SHA-256 of every uncompressed file.
//
//go:embed payload/*.gz
var payloadFS embed.FS

type payloadFile struct {
	Name   string
	SHA256 string
}

// ensurePayload writes every payload file to dir unless an identical copy is
// there. Not core-miner.exe: an antivirus that blocks or quarantines it must
// not stop the node (install, every tray start). minerIntact extracts it
// before each miner start and reports a block as "av".
func ensurePayload(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range payloadFiles {
		if f.Name == "core-miner.exe" {
			continue
		}
		dst := filepath.Join(dir, f.Name)
		if sum, err := fileSHA256(dst); err == nil && sum == f.SHA256 {
			continue
		}
		if err := extractOne(f, dst); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

func extractOne(f payloadFile, dst string) error {
	src, err := payloadFS.Open("payload/" + f.Name + ".gz")
	if err != nil {
		return err
	}
	defer src.Close()
	zr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), zr)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = zr.Close()
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		err = fmt.Errorf("checksum mismatch after extraction")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return renameRetry(tmp, dst) // a virus scanner may still be reading the fresh file
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
