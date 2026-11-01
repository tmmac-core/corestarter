// Command elfimports checks, at build time, that the staged Linux binaries run
// on the oldest supported systems: no glibc symbol version newer than -max-glibc
// and only the shared libraries listed in -allow (sonames, comma-separated).
//
//	go run ./tools/elfimports -max-glibc 2.35 -allow libc.so.6,libm.so.6 <elf> [<elf> ...]
//
// The official gocore is not checked: it is unmodified and gets the bundled
// runtime when the system's glibc is too old (node_linux.go gocoreCommand).
package main

import (
	"debug/elf"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	maxGlibc := flag.String("max-glibc", "2.35", "newest GLIBC_ symbol version allowed")
	allow := flag.String("allow", "", "allowed NEEDED sonames, comma-separated")
	flag.Parse()
	allowed := map[string]bool{}
	for _, s := range strings.Split(*allow, ",") {
		if s = strings.TrimSpace(s); s != "" {
			allowed[s] = true
		}
	}
	limit, err := parseVersion(*maxGlibc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad -max-glibc:", err)
		os.Exit(2)
	}
	failed := false
	for _, path := range flag.Args() {
		if err := check(path, limit, allowed); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func check(path string, limit []int, allowed map[string]bool) error {
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	libs, err := f.ImportedLibraries()
	if err != nil {
		return err
	}
	var problems []string
	for _, l := range libs {
		if !allowed[l] {
			problems = append(problems, "needs "+l)
		}
	}
	syms, err := f.ImportedSymbols()
	if err != nil {
		return err
	}
	newest, newestSym := []int{0}, ""
	for _, s := range syms {
		v, ok := strings.CutPrefix(s.Version, "GLIBC_")
		if !ok {
			continue
		}
		n, err := parseVersion(v)
		if err != nil {
			continue // GLIBC_PRIVATE and the like
		}
		if less(newest, n) {
			newest, newestSym = n, s.Name+"@"+s.Version
		}
		if less(limit, n) {
			problems = append(problems, fmt.Sprintf("%s@%s is newer than GLIBC_%s", s.Name, s.Version, join(limit)))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	fmt.Printf("%s: ok (%d libraries, newest %s)\n", path, len(libs), newestSym)
	return nil
}

func parseVersion(s string) ([]int, error) {
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// less reports a < b for dotted versions.
func less(a, b []int) bool {
	for i := 0; i < max(len(a), len(b)); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func join(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ".")
}
