package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountries(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.csv"), filepath.Join(dir, "out.bin.gz")
	csv := strings.Join([]string{
		"1.0.0.0,1.0.0.255,AU",
		"1.0.1.0,1.0.1.255,AU", // same country: merged with the row above
		"1.0.4.0,1.0.4.255,ZZ", // after a gap; ZZ means unknown
		"1.0.5.0,1.0.5.255,CN",
		"::,::ffff,US", // IPv6 rows are skipped
	}, "\n") + "\n"
	os.WriteFile(in, []byte(csv), 0o644)
	if err := countries(in, out); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(out)
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	type rec struct {
		start uint32
		cc    string
	}
	var got []rec
	for i := 0; i+6 <= len(b); i += 6 {
		got = append(got, rec{binary.BigEndian.Uint32(b[i:]), string(bytes.TrimRight(b[i+4:i+6], "\x00"))})
	}
	ip := func(a, b, c, d uint32) uint32 { return a<<24 | b<<16 | c<<8 | d }
	want := []rec{
		{0, ""}, {ip(1, 0, 0, 0), "AU"}, {ip(1, 0, 2, 0), ""}, {ip(1, 0, 5, 0), "CN"}, {ip(1, 0, 6, 0), ""},
	}
	if len(got) != len(want) {
		t.Fatalf("records %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d: %v, want %v", i, got[i], want[i])
		}
	}
	// Unsorted input and a reversed range are errors.
	for _, bad := range []string{"1.0.5.0,1.0.5.255,CN\n1.0.0.0,1.0.0.255,AU\n", "1.0.5.0,1.0.4.0,CN\n"} {
		os.WriteFile(in, []byte(bad), 0o644)
		if err := countries(in, out); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
