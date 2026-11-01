// Command peimports checks, at build time, that every DLL import of the staged
// Windows binaries is satisfied: either by a DLL in the stage directory (and
// that DLL really exports each imported symbol) or by a known Windows system DLL.
//
//	go run ./tools/peimports <stage-dir> <exe> [<exe> ...]
//	go run ./tools/peimports -known <exe>
//
// -known is for an exe that is started from an untrusted folder (Downloads):
// it may only import KnownDLLs, which Windows always loads from System32, so a
// DLL planted next to it cannot be picked up before its own code runs.
package main

import (
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// systemDLLs ship with every supported Windows 10/11; api-ms-win-* are API sets.
var systemDLLs = map[string]bool{
	"kernel32.dll": true, "advapi32.dll": true, "ntdll.dll": true, "user32.dll": true,
	"ws2_32.dll": true, "winmm.dll": true, "shell32.dll": true, "ole32.dll": true,
	"bcrypt.dll": true, "crypt32.dll": true, "iphlpapi.dll": true, "dnsapi.dll": true,
	"userenv.dll": true, "secur32.dll": true, "ucrtbase.dll": true, "msvcrt.dll": true,
	"powrprof.dll": true, "psapi.dll": true, "dbghelp.dll": true,
}

// knownDLLs is a subset of HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\KnownDLLs.
var knownDLLs = map[string]bool{
	"kernel32.dll": true, "ntdll.dll": true, "advapi32.dll": true, "user32.dll": true,
	"gdi32.dll": true, "shell32.dll": true, "ole32.dll": true, "ws2_32.dll": true,
	"msvcrt.dll": true, "combase.dll": true, "rpcrt4.dll": true, "sechost.dll": true,
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-known" {
		checkKnown(os.Args[2])
		return
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: peimports <stage-dir> <exe> [<exe> ...] | peimports -known <exe>")
		os.Exit(2)
	}
	stage := os.Args[1]
	staged := map[string]string{} // lower-case name -> path
	entries, err := os.ReadDir(stage)
	check(err)
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".dll") {
			staged[strings.ToLower(e.Name())] = filepath.Join(stage, e.Name())
		}
	}
	queue := os.Args[2:]
	for _, dll := range staged {
		queue = append(queue, dll)
	}
	failed := false
	for _, bin := range queue {
		f, err := pe.Open(bin)
		check(err)
		syms, err := f.ImportedSymbols()
		check(err)
		libs, err := f.ImportedLibraries()
		check(err)
		need := map[string][]string{}
		for _, s := range syms { // "symbol:dll"
			i := strings.LastIndex(s, ":")
			dll := strings.ToLower(s[i+1:])
			need[dll] = append(need[dll], s[:i])
		}
		for _, l := range libs {
			if _, ok := need[strings.ToLower(l)]; !ok {
				need[strings.ToLower(l)] = nil
			}
		}
		f.Close()
		for dll, names := range need {
			switch {
			case strings.HasPrefix(dll, "api-ms-win-") || systemDLLs[dll]:
			case staged[dll] != "":
				exports := exportedNames(staged[dll])
				for _, n := range names {
					if !exports[n] {
						fmt.Printf("MISSING %s: %s does not export %s\n", filepath.Base(bin), dll, n)
						failed = true
					}
				}
			default:
				fmt.Printf("MISSING %s: needs %s, which is neither staged nor a known system DLL\n", filepath.Base(bin), dll)
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("imports ok: %d binaries, %d staged DLLs\n", len(queue), len(staged))
}

func checkKnown(bin string) {
	f, err := pe.Open(bin)
	check(err)
	syms, err := f.ImportedSymbols() // "symbol:dll"; ImportedLibraries is not implemented in debug/pe
	check(err)
	f.Close()
	seen := map[string]bool{}
	var libs []string
	for _, sym := range syms {
		dll := strings.ToLower(sym[strings.LastIndex(sym, ":")+1:])
		if !seen[dll] {
			seen[dll] = true
			libs = append(libs, dll)
		}
	}
	if len(libs) == 0 {
		fmt.Printf("UNSAFE %s: no imports found, the check would prove nothing\n", filepath.Base(bin))
		os.Exit(1)
	}
	for _, l := range libs {
		if !knownDLLs[strings.ToLower(l)] {
			fmt.Printf("UNSAFE %s: imports %s, which is not a KnownDLL (DLL planting)\n", filepath.Base(bin), l)
			os.Exit(1)
		}
	}
	fmt.Printf("imports ok: %s only uses KnownDLLs %v\n", filepath.Base(bin), libs)
}

// exportedNames reads the export name table of a 64-bit PE DLL.
func exportedNames(path string) map[string]bool {
	f, err := pe.Open(path)
	check(err)
	defer f.Close()
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || len(oh.DataDirectory) == 0 {
		check(fmt.Errorf("%s: not a 64-bit PE with data directories", path))
	}
	dir := oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_EXPORT]
	read := func(rva uint32, n uint32) []byte {
		for _, s := range f.Sections {
			if rva >= s.VirtualAddress && rva < s.VirtualAddress+s.VirtualSize {
				data, err := s.Data()
				check(err)
				off := rva - s.VirtualAddress
				end := min(off+n, uint32(len(data)))
				return data[off:end]
			}
		}
		check(fmt.Errorf("%s: rva %#x outside sections", path, rva))
		return nil
	}
	out := map[string]bool{}
	if dir.VirtualAddress == 0 {
		return out
	}
	hdr := read(dir.VirtualAddress, 40)
	count := binary.LittleEndian.Uint32(hdr[24:])
	namesRVA := binary.LittleEndian.Uint32(hdr[32:])
	names := read(namesRVA, 4*count)
	for i := uint32(0); i < count; i++ {
		b := read(binary.LittleEndian.Uint32(names[4*i:]), 512)
		if j := strings.IndexByte(string(b), 0); j >= 0 {
			b = b[:j]
		}
		out[string(b)] = true
	}
	return out
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "peimports:", err)
		os.Exit(1)
	}
}
