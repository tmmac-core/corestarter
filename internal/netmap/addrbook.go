package netmap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

// The node's address book is a LevelDB folder that gocore keeps locked while
// it runs. Read copies its files into memory and opens them there, read only:
// no lock is taken and nothing is written to the node's folder.

// maxBookBytes bounds what Read loads; gocore's book stays well under 1 MB.
const maxBookBytes = 32 << 20

// Windows of the summary: a node counts as active if it answered the local
// node's discovery ping within activeWindow, as seen within seenWindow.
// gocore itself drops entries about a day after their last answer.
const (
	activeWindow = time.Hour
	seenWindow   = 24 * time.Hour
)

// CountryCount is the number of nodes in one country.
type CountryCount struct {
	Code   string `json:"code"`
	Active int    `json:"active"`
	Seen   int    `json:"seen"`
}

// Summary is all the window gets: counts, no addresses, no node IDs.
type Summary struct {
	Active    int            `json:"active"`   // answered within the last hour
	Seen      int            `json:"seen"`     // answered within the last 24 hours
	Unplaced  int            `json:"unplaced"` // seen, but without a country (IPv6, unknown range)
	Countries []CountryCount `json:"countries"`
	// AsOf is the moment the counts refer to (Unix seconds): now while the
	// node writes its book, else the book's last write (a stopped node).
	AsOf int64 `json:"asOf"`
	Idle bool  `json:"idle"` // the book has not changed for idleAfter
}

// idleAfter: a running gocore records answers to its pings several times a
// minute; a book untouched for this long belongs to a node that is not running.
const idleAfter = 15 * time.Minute

type entry struct {
	addr netip.Addr
	pong time.Time
}

// Read summarises the address book in dir (gocore: <datadir>/gocore/nodes).
// A missing folder is an empty book. The files can change while they are
// copied; a copy that does not open is taken again.
func Read(dir string, now time.Time) (Summary, error) {
	var last error
	for try := 0; try < 3; try++ {
		entries, err := readOnce(dir)
		if err == nil {
			ref, idle := now, false
			if t, ok := lastWrite(dir); ok && now.Sub(t) > idleAfter {
				ref, idle = t, true
			}
			s := summarise(entries, ref)
			s.AsOf, s.Idle = ref.Unix(), idle
			return s, nil
		}
		if errors.Is(err, errNoBook) {
			return Summary{Countries: []CountryCount{}, AsOf: now.Unix()}, nil
		}
		last = err
		time.Sleep(150 * time.Millisecond)
	}
	return Summary{}, last
}

// lastWrite is the newest modification time of the book's files.
func lastWrite(dir string) (time.Time, bool) {
	names, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var t time.Time
	for _, e := range names {
		if _, ok := fileDesc(e.Name()); !ok && e.Name() != "CURRENT" {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t, !t.IsZero()
}

// errNoBook: gocore has not written an address book yet (no folder, no
// manifest). Any other missing file means the book changed while it was
// copied, which Read retries.
var errNoBook = errors.New("no address book yet")

func readOnce(dir string) (map[[32]byte]entry, error) {
	st, err := loadFiles(dir)
	if err != nil {
		return nil, err
	}
	// A small write buffer: the in-memory copy never takes writes.
	db, err := leveldb.Open(st, &opt.Options{ReadOnly: true, ErrorIfMissing: true, WriteBuffer: 64 << 10})
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := map[[32]byte]entry{}
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		id, addr, ok := pongKey(it.Key())
		if !ok {
			continue
		}
		sec, n := binary.Varint(it.Value())
		if n <= 0 {
			continue
		}
		t := time.Unix(sec, 0)
		// One node can have answered from several addresses: the latest counts.
		if e, seen := out[id]; !seen || t.After(e.pong) {
			out[id] = entry{addr: addr, pong: t}
		}
	}
	return out, it.Error()
}

// pongKey parses "n:" <32-byte id> ":v4:" <16-byte ip> ":lastpong", the key
// under which gocore's p2p/enode keeps the time of a node's last answer.
func pongKey(k []byte) (id [32]byte, addr netip.Addr, ok bool) {
	const pre, mid, suf = "n:", ":v4:", ":lastpong"
	if len(k) != len(pre)+32+len(mid)+16+len(suf) ||
		!bytes.HasPrefix(k, []byte(pre)) || !bytes.HasSuffix(k, []byte(suf)) ||
		string(k[len(pre)+32:len(pre)+32+len(mid)]) != mid {
		return id, addr, false
	}
	copy(id[:], k[len(pre):])
	var ip [16]byte
	copy(ip[:], k[len(pre)+32+len(mid):])
	return id, netip.AddrFrom16(ip).Unmap(), true
}

// CURRENT is never opened: on Windows, goleveldb replaces it by renaming a
// temporary file onto it (MoveFileEx REPLACE_EXISTING), and that rename fails
// with "Access is denied" while any other process has CURRENT open, even with
// FILE_SHARE_DELETE (seen on the CI's Windows runner, 20.11.2026). The current
// manifest is the one with the highest number instead: goleveldb writes a new
// manifest under a higher number before it switches CURRENT, and deletes the
// old one afterwards. A new manifest caught half-written reads as corrupt,
// which Read retries.
func loadFiles(dir string) (storage.Storage, error) {
	names, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoBook
	}
	if err != nil {
		return nil, err
	}
	var manifest storage.FileDesc
	for _, e := range names {
		if fd, ok := fileDesc(e.Name()); ok && fd.Type == storage.TypeManifest && fd.Num >= manifest.Num {
			manifest = fd
		}
	}
	if manifest.Type != storage.TypeManifest {
		return nil, errNoBook
	}
	st := storage.NewMemStorage()
	total, haveManifest := 0, false
	for _, e := range names {
		fd, ok := fileDesc(e.Name())
		if !ok {
			continue
		}
		b, err := readShared(filepath.Join(dir, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue // removed by a compaction since the listing
		}
		if err != nil {
			return nil, err
		}
		if total += len(b); total > maxBookBytes {
			return nil, fmt.Errorf("address book larger than %d MB", maxBookBytes>>20)
		}
		w, err := st.Create(fd)
		if err != nil {
			return nil, err
		}
		_, err = w.Write(b)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		if fd == manifest {
			if err := st.SetMeta(fd); err != nil {
				return nil, err
			}
			haveManifest = true
		}
	}
	if !haveManifest {
		return nil, errors.New("address book: manifest changed while reading")
	}
	return st, nil
}

func fileDesc(name string) (storage.FileDesc, bool) {
	var t storage.FileType
	num := name
	switch {
	case strings.HasPrefix(name, "MANIFEST-"):
		t, num = storage.TypeManifest, name[len("MANIFEST-"):]
	case strings.HasSuffix(name, ".ldb"), strings.HasSuffix(name, ".sst"):
		t, num = storage.TypeTable, name[:len(name)-4]
	case strings.HasSuffix(name, ".log"):
		t, num = storage.TypeJournal, name[:len(name)-4]
	default:
		return storage.FileDesc{}, false
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return storage.FileDesc{}, false
	}
	return storage.FileDesc{Type: t, Num: n}, true
}

func summarise(entries map[[32]byte]entry, now time.Time) Summary {
	s := Summary{}
	per := map[string]*CountryCount{}
	for _, e := range entries {
		age := now.Sub(e.pong)
		if age > seenWindow || age < -time.Hour { // a clock far ahead is not an answer
			continue
		}
		s.Seen++
		active := age <= activeWindow
		if active {
			s.Active++
		}
		cc := Country(e.addr)
		if cc == "" {
			s.Unplaced++
			continue
		}
		c := per[cc]
		if c == nil {
			c = &CountryCount{Code: cc}
			per[cc] = c
		}
		c.Seen++
		if active {
			c.Active++
		}
	}
	s.Countries = make([]CountryCount, 0, len(per))
	for _, c := range per {
		s.Countries = append(s.Countries, *c)
	}
	sort.Slice(s.Countries, func(i, j int) bool {
		a, b := s.Countries[i], s.Countries[j]
		if a.Seen != b.Seen {
			return a.Seen > b.Seen
		}
		return a.Code < b.Code
	})
	return s
}
