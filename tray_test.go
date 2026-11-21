package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

// icoImages decodes every entry of an .ico (all PNG-coded).
func icoImages(t *testing.T, name string, ico []byte) map[int]image.Image {
	t.Helper()
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		t.Fatalf("%s: not an icon", name)
	}
	out := map[int]image.Image{}
	for i := 0; i < int(binary.LittleEndian.Uint16(ico[4:])); i++ {
		e := ico[6+16*i:]
		size, off := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
		if uint64(off)+uint64(size) > uint64(len(ico)) {
			t.Fatalf("%s: entry %d outside the file", name, i)
		}
		img, err := png.Decode(bytes.NewReader(ico[off : off+size]))
		if err != nil {
			t.Fatalf("%s: entry %d: %v", name, i, err)
		}
		out[img.Bounds().Dx()] = img
	}
	return out
}

func TestTrayIcons(t *testing.T) {
	names := trayICONames
	var big [len(names)]image.Image
	for look, name := range names {
		b, err := trayICOs.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		imgs := icoImages(t, name, b)
		for _, s := range []int{16, 20, 24, 32, 40, 48, 64, 256} {
			if imgs[s] == nil {
				t.Errorf("%s: no %d px image", name, s)
			}
		}
		big[look] = imgs[256]
	}
	if t.Failed() {
		return
	}
	// The Core mark in the middle is the same in every state (it is never
	// recoloured); ring and stroke make each state different. Mark: centre
	// (54, 58) of 108 units, radius 19; the circle checked has radius 21 and
	// stays clear of the ring (inner edge 33) and the stroke (ends at y 35).
	const scale = 256.0 / 108
	cx, cy, r := 54*scale, 58*scale, 21*scale
	same := func(a, b image.Image, inMark bool) bool {
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
				if (dx*dx+dy*dy <= r*r) != inMark {
					continue
				}
				if a.At(x, y) != b.At(x, y) {
					return false
				}
			}
		}
		return true
	}
	for i := range big {
		if !same(big[0], big[i], true) {
			t.Errorf("%s: the Core mark differs from tray.ico", names[i])
		}
		for j := i + 1; j < len(big); j++ {
			if same(big[i], big[j], false) {
				t.Errorf("%s and %s look the same", names[i], names[j])
			}
		}
	}
}

func TestTrayLookFor(t *testing.T) {
	for _, c := range []struct {
		st     NodeState
		mining bool
		want   trayLook
	}{
		{NodeStopped, false, lookOff},
		{NodeRunning, false, lookNode},
		{NodeStopped, true, lookMiner},
		{NodeRunning, true, lookNodeMiner},
		{NodeFailed, false, lookError},
		{NodeFailed, true, lookErrorMiner},
	} {
		if got := trayLookFor(c.st, c.mining); got != c.want {
			t.Errorf("trayLookFor(%v, %v) = %v, want %v", c.st, c.mining, got, c.want)
		}
	}
	for look, b := range trayIcons {
		if len(b) == 0 {
			t.Errorf("look %d: no icon", look)
		}
	}
}

func TestSettledNode(t *testing.T) {
	// A restart (running, stopping, starting, running) keeps the stroke green;
	// a retry after a crash keeps it orange until the node runs again.
	last := NodeStopped
	for _, c := range []struct{ st, want NodeState }{
		{NodeStarting, NodeStopped},
		{NodeRunning, NodeRunning},
		{NodeStopping, NodeRunning},
		{NodeStarting, NodeRunning},
		{NodeFailed, NodeFailed},
		{NodeStarting, NodeFailed},
		{NodeRunning, NodeRunning},
		{NodeStopping, NodeRunning},
		{NodeStopped, NodeStopped},
	} {
		last = settledNode(last, c.st)
		if last != c.want {
			t.Errorf("after %v: %v, want %v", c.st, last, c.want)
		}
	}
}

// A failed node names its cause; disk space wins, a plain crash is the rest.
func TestFailCause(t *testing.T) {
	cases := []struct {
		st       NodeState
		disk     bool
		cause    string
		want     string
		wantHint string
	}{
		{NodeRunning, false, "foreign", "", ""},
		{NodeFailed, true, "foreign", "disk", textsDE.diskFullHint},
		{NodeFailed, false, "foreign", "foreign", textsDE.foreignHint},
		{NodeFailed, false, "port", "port", textsDE.portHint},
		{NodeFailed, false, "", "crash", textsDE.crashHint},
	}
	for _, c := range cases {
		got, hint := failCause(textsDE, c.st, c.disk, c.cause)
		if got != c.want || hint != c.wantHint {
			t.Errorf("failCause(%v, disk=%v, %q) = %q, %q", c.st, c.disk, c.cause, got, hint)
		}
	}
}

// Counted from the first imported block of the run; the fast sync (head 0) counts nothing.
func TestBlocksChecked(t *testing.T) {
	start, n := blocksChecked(0, 0, &Snapshot{Head: 0})
	if start != 0 || n != 0 {
		t.Fatalf("fast sync: %d %d", start, n)
	}
	start, n = blocksChecked(start, n, &Snapshot{Head: 1000})
	if start != 1000 || n != 0 {
		t.Fatalf("first head: %d %d", start, n)
	}
	start, n = blocksChecked(start, n, &Snapshot{Head: 1250})
	if start != 1000 || n != 250 {
		t.Fatalf("later: %d %d", start, n)
	}
	if s2, n2 := blocksChecked(start, n, nil); s2 != 1000 || n2 != 250 {
		t.Fatalf("missed poll must keep the count: %d %d", s2, n2)
	}
	if start, n = blocksChecked(start, n, &Snapshot{Head: 900}); start != 900 || n != 0 {
		t.Fatalf("head below start: %d %d", start, n)
	}
}

func TestNextPort(t *testing.T) {
	if next, moved, cause := nextPort(defaultP2PPort, false); next != defaultP2PPort || moved || cause != "" {
		t.Fatalf("port free: %d %v %q", next, moved, cause)
	}
	if next, moved, cause := nextPort(defaultP2PPort, true); next != defaultP2PPort+1 || !moved || cause != "" {
		t.Fatalf("in use: %d %v %q", next, moved, cause)
	}
	if next, moved, cause := nextPort(defaultP2PPort+19, true); next != defaultP2PPort+20 || !moved || cause != "" {
		t.Fatalf("last move: %d %v %q", next, moved, cause)
	}
	if next, moved, cause := nextPort(defaultP2PPort+20, true); next != defaultP2PPort+20 || moved || cause != "port" {
		t.Fatalf("range used up: %d %v %q", next, moved, cause)
	}
}
