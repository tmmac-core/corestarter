package netmap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"golang.org/x/sys/windows"
)

// openShared opens a file the way another process might hold it.
func openShared(t *testing.T, name string, share uint32) windows.Handle {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(name)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// The reader must never open CURRENT: on Windows, gocore replaces it with a
// rename onto it, which fails while anyone holds it open, even with
// FILE_SHARE_DELETE. With CURRENT locked exclusively the book still reads.
func TestReadNeverOpensCurrent(t *testing.T) {
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
	h := openShared(t, filepath.Join(dir, "CURRENT"), 0) // no sharing at all
	defer windows.CloseHandle(h)
	if sum, err := Read(dir, now); err != nil || sum.Active != 1 {
		t.Fatalf("Read with CURRENT locked = %+v, %v", sum, err)
	}
}

// What the rename onto an open file does on Windows, documented: the reason
// the reader stays away from CURRENT. Deleting an open table still works.
func TestWindowsRenameOntoOpenFileFails(t *testing.T) {
	dir := t.TempDir()
	cur, tab := filepath.Join(dir, "CURRENT"), filepath.Join(dir, "000005.ldb")
	for _, f := range []string{cur, tab, cur + ".7"} {
		if err := os.WriteFile(f, []byte("MANIFEST-000007\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	all := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	hc, ht := openShared(t, cur, all), openShared(t, tab, all)
	defer windows.CloseHandle(hc)
	defer windows.CloseHandle(ht)
	if err := os.Rename(cur+".7", cur); err == nil {
		t.Log("rename onto an open file worked on this Windows version")
	}
	if err := os.Remove(tab); err != nil {
		t.Errorf("delete a table while it is being read: %v", err)
	}
	// readShared itself must also work on a file the owner holds open for writing.
	f, err := os.OpenFile(filepath.Join(dir, "000006.log"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write([]byte("journal"))
	if b, err := readShared(f.Name()); err != nil || string(b) != "journal" {
		t.Errorf("readShared on an open journal: %q %v", b, err)
	}
}
