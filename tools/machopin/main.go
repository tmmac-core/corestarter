// Command machopin checks, at build time, that signing an official gocore left
// its code and data alone: after codesign --remove-signature the copy must be
// the original byte for byte, except the vmsize of the __LINKEDIT segment
// (codesign grows the segment for the signature and does not shrink it back;
// filesize it restores). A plain file hash cannot prove the pin once the file
// is signed.
//
//	go run ./tools/machopin <original> <unsigned-again>
//
// scripts/build-mac.sh checks the original's SHA-256 before and makes the
// second file with codesign --remove-signature from the signed copy. Only for
// an unsigned original (the Intel gocore): the arm64 one carries its linker
// signature and goes into the app unchanged.
package main

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: machopin <original> <unsigned-again>")
		os.Exit(2)
	}
	if err := check(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "machopin:", err)
		os.Exit(1)
	}
	fmt.Println("machopin: code and data unchanged")
}

func check(origPath, againPath string) error {
	orig, err := os.ReadFile(origPath)
	if err != nil {
		return err
	}
	again, err := os.ReadFile(againPath)
	if err != nil {
		return err
	}
	if len(orig) != len(again) {
		return fmt.Errorf("size %d, original %d", len(again), len(orig))
	}
	seg, err := linkedit(orig)
	if err != nil {
		return err
	}
	// segment_command_64: vmsize at +32, 8 bytes. It may only grow.
	bo := binary.LittleEndian
	if bo.Uint64(again[seg+32:]) < bo.Uint64(orig[seg+32:]) {
		return fmt.Errorf("__LINKEDIT vmsize shrank")
	}
	sizeField := func(i int) bool { return i >= seg+32 && i < seg+40 }
	for i := range orig {
		if orig[i] != again[i] && !sizeField(i) {
			return fmt.Errorf("byte %d differs outside the vmsize of __LINKEDIT", i)
		}
	}
	return nil
}

// linkedit returns the file offset of the __LINKEDIT segment command.
func linkedit(b []byte) (int, error) {
	f, err := macho.NewFile(bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	if f.Magic != macho.Magic64 || f.ByteOrder != binary.LittleEndian {
		return 0, fmt.Errorf("not a thin little-endian 64-bit Mach-O")
	}
	for _, l := range f.Loads {
		if lc := macho.LoadCmd(f.ByteOrder.Uint32(l.Raw())); lc == 0x1d { // LC_CODE_SIGNATURE
			return 0, fmt.Errorf("original is signed: compare only unsigned originals")
		}
	}
	bo := f.ByteOrder
	off := 32 // mach_header_64
	for i := uint32(0); i < f.Ncmd; i++ {
		if off+8 > len(b) {
			return 0, fmt.Errorf("load commands truncated")
		}
		cmd, size := bo.Uint32(b[off:]), int(bo.Uint32(b[off+4:]))
		if size < 8 || off+size > len(b) {
			return 0, fmt.Errorf("bad load command size")
		}
		if macho.LoadCmd(cmd) == macho.LoadCmdSegment64 && size >= 56 &&
			string(bytes.TrimRight(b[off+8:off+24], "\x00")) == "__LINKEDIT" {
			return off, nil
		}
		off += size
	}
	return 0, fmt.Errorf("no __LINKEDIT segment")
}
