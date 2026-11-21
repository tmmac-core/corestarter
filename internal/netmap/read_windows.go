package netmap

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// readShared opens a file of the node's address book like os.ReadFile, but
// also allows deleting and renaming it meanwhile (FILE_SHARE_DELETE). Go's
// own Open leaves that out; gocore replaces CURRENT by a rename when it opens
// its database, and a read at that moment must not make that rename fail.
func readShared(name string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
		}
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	f := os.NewFile(uintptr(h), name)
	defer f.Close()
	if st, err := f.Stat(); err != nil {
		return nil, err
	} else if st.Size() > maxBookBytes {
		return nil, fmt.Errorf("%s: larger than %d MB", name, maxBookBytes>>20)
	}
	return io.ReadAll(f)
}
