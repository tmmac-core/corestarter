package netmap

import (
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

var leveldbRangeAll = util.Range{}

func TestCountry(t *testing.T) {
	cases := map[string]string{
		"8.8.8.8":        "US",
		"::ffff:8.8.8.8": "US",
		"192.168.1.4":    "",
		"10.0.0.1":       "",
		"100.64.3.2":     "",
		"127.0.0.1":      "",
		"2001:db8::1":    "",
		"0.0.0.0":        "",
	}
	for in, want := range cases {
		if got := Country(netip.MustParseAddr(in)); got != want {
			t.Errorf("Country(%s) = %q, want %q", in, got, want)
		}
	}
	if n := len(countryTable()); n == 0 || n%6 != 0 {
		t.Fatalf("country table has %d bytes", n)
	}
}

func TestLookupEdges(t *testing.T) {
	rec := func(start uint32, cc string) []byte {
		b := make([]byte, 6)
		binary.BigEndian.PutUint32(b, start)
		copy(b[4:], cc)
		return b
	}
	tab := append(append(rec(10, "AA"), rec(20, "\x00\x00")...), rec(30, "BB")...)
	for ip, want := range map[uint32]string{0: "", 9: "", 10: "AA", 19: "AA", 20: "", 29: "", 30: "BB", 1 << 31: "BB"} {
		if got := lookup(tab, ip); got != want {
			t.Errorf("lookup(%d) = %q, want %q", ip, got, want)
		}
	}
}

func pongKeyFor(id byte, ip string) []byte {
	k := []byte("n:")
	for i := 0; i < 32; i++ {
		k = append(k, id)
	}
	k = append(k, ":v4:"...)
	a := netip.MustParseAddr(ip).As16()
	k = append(k, a[:]...)
	return append(k, ":lastpong"...)
}

func varint(v int64) []byte {
	b := make([]byte, binary.MaxVarintLen64)
	return b[:binary.PutVarint(b, v)]
}

// The book is read while its owner still has it open and locked, as gocore
// has; on Windows this is the case the file sharing must allow.
func TestReadWhileOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nodes")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Truncate(time.Second) // the files carry the real clock: an old "now" would read as an idle book
	put := func(id byte, ip string, age time.Duration) {
		t.Helper()
		if err := db.Put(pongKeyFor(id, ip), varint(now.Add(-age).Unix()), nil); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "8.8.8.8", 10*time.Minute)    // US, active
	put(2, "8.8.4.4", 5*time.Hour)       // US, seen
	put(3, "192.168.0.9", time.Minute)   // seen, no country
	put(4, "8.8.8.8", 30*time.Hour)      // too old
	put(5, "2001:db8::5", 2*time.Minute) // IPv6: seen, no country
	put(6, "8.8.8.8", 20*time.Hour)      // same node from two addresses: the later answer counts
	put(6, "192.168.0.6", 3*time.Minute)
	// Other keys of the same database must not count.
	db.Put([]byte("version"), varint(9), nil)
	db.Put(append(pongKeyFor(7, "8.8.8.8")[:len(pongKeyFor(7, "8.8.8.8"))-len("lastpong")], "lastping"...), varint(now.Unix()), nil)
	db.Put([]byte("local:seq"), varint(3), nil)

	// Half the entries in a table file, the rest still in the journal.
	if err := db.CompactRange(leveldbRangeAll); err != nil {
		t.Fatal(err)
	}
	put(8, "8.8.8.8", 59*time.Minute) // US, active, journal only

	s, err := Read(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Seen != 6 || s.Active != 5 || s.Unplaced != 3 {
		t.Fatalf("seen %d active %d unplaced %d, want 6 5 3", s.Seen, s.Active, s.Unplaced)
	}
	if len(s.Countries) != 1 || s.Countries[0] != (CountryCount{Code: "US", Active: 2, Seen: 3}) {
		t.Fatalf("countries %+v", s.Countries)
	}
	// The owner can still write: Read took no lock and left no file behind.
	put(9, "8.8.8.8", time.Minute)
}

func TestReadMissing(t *testing.T) {
	s, err := Read(filepath.Join(t.TempDir(), "nodes"), time.Now())
	if err != nil || s.Seen != 0 || s.Countries == nil {
		t.Fatalf("missing book: %+v, %v", s, err)
	}
}

func TestPongKeyRejects(t *testing.T) {
	good := pongKeyFor(1, "8.8.8.8")
	if _, a, ok := pongKey(good); !ok || a.String() != "8.8.8.8" {
		t.Fatalf("good key: %v %v", a, ok)
	}
	for _, k := range [][]byte{nil, good[:len(good)-1], append([]byte("x"), good[1:]...), append(append([]byte{}, good...), 'x')} {
		if _, _, ok := pongKey(k); ok {
			t.Errorf("accepted %q", k)
		}
	}
}

// A table the manifest names but that is gone (gocore compacted it while we
// copied) is an error to retry, never an empty book.
func TestReadMissingTableIsError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nodes")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	for i := byte(1); i < 30; i++ {
		db.Put(pongKeyFor(i, "8.8.8.8"), varint(now.Unix()), nil)
	}
	if err := db.CompactRange(leveldbRangeAll); err != nil {
		t.Fatal(err)
	}
	db.Close()
	tables, _ := filepath.Glob(filepath.Join(dir, "*.ldb"))
	if len(tables) == 0 {
		t.Fatal("no table written")
	}
	for _, f := range tables {
		os.Remove(f)
	}
	s, err := Read(dir, now)
	if err == nil {
		t.Fatalf("missing table read as %+v without error", s)
	}
}

func TestReadEmptyFolder(t *testing.T) {
	dir := t.TempDir() // exists, but gocore has not written CURRENT yet
	if s, err := Read(dir, time.Now()); err != nil || s.Seen != 0 {
		t.Fatalf("empty folder: %+v, %v", s, err)
	}
}

func TestSummariseWindows(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ip := netip.MustParseAddr("8.8.8.8")
	cases := []struct {
		age          time.Duration
		seen, active int
	}{
		{0, 1, 1},
		{time.Hour, 1, 1},
		{time.Hour + time.Second, 1, 0},
		{24 * time.Hour, 1, 0},
		{24*time.Hour + time.Second, 0, 0},
		{-30 * time.Minute, 1, 1}, // a clock slightly ahead still counts
		{-2 * time.Hour, 0, 0},    // far ahead does not
	}
	for _, c := range cases {
		s := summarise(map[[32]byte]entry{{1}: {addr: ip, pong: now.Add(-c.age)}}, now)
		if s.Seen != c.seen || s.Active != c.active {
			t.Errorf("age %v: seen %d active %d, want %d %d", c.age, s.Seen, s.Active, c.seen, c.active)
		}
	}
}

// A book nobody wrote to for a while belongs to a stopped node: the counts
// refer to its last write, so the map shows what the node knew then.
func TestReadIdleBook(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nodes")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	stopped := time.Now().Add(-30 * time.Hour).Truncate(time.Second)
	db.Put(pongKeyFor(1, "8.8.8.8"), varint(stopped.Add(-10*time.Minute).Unix()), nil)
	db.Close()
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		os.Chtimes(filepath.Join(dir, f.Name()), stopped, stopped)
	}
	s, err := Read(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Idle || s.AsOf != stopped.Unix() || s.Seen != 1 || s.Active != 1 {
		t.Fatalf("idle book: %+v, want idle, asOf %d, seen 1 active 1", s, stopped.Unix())
	}
	// A book written just now is read against now.
	s, _ = Read(dir, stopped.Add(time.Minute))
	if s.Idle {
		t.Fatalf("fresh book marked idle: %+v", s)
	}
}

// The reader never needs CURRENT (on Windows, holding it open would block
// gocore's rename): a book without it reads like one with it.
func TestReadWithoutCurrent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nodes")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	if err := db.Put(pongKeyFor(1, "8.8.8.8"), varint(now.Add(-time.Minute).Unix()), nil); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.Remove(filepath.Join(dir, "CURRENT")); err != nil {
		t.Fatal(err)
	}
	sum, err := Read(dir, now)
	if err != nil || sum.Active != 1 || len(sum.Countries) != 1 || sum.Countries[0].Code != "US" {
		t.Fatalf("Read without CURRENT = %+v, %v", sum, err)
	}
}
