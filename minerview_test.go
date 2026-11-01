package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

func TestReadLogChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miner.log")
	if _, err := readLogChunk(path, -1); err == nil {
		t.Error("missing file: want an error")
	}
	os.WriteFile(path, []byte("a\nb\nhalf"), 0o644)
	c, err := readLogChunk(path, -1)
	if err != nil || !c.Reset || strings.Join(c.Lines, ",") != "a,b" || c.Next != 4 {
		t.Fatalf("first read = %+v, %v", c, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(" line\r\nc\n")
	f.Close()
	c, _ = readLogChunk(path, c.Next)
	if c.Reset || strings.Join(c.Lines, ",") != "half line,c" {
		t.Fatalf("follow-up = %+v", c)
	}
	same, _ := readLogChunk(path, c.Next)
	if len(same.Lines) != 0 || same.Next != c.Next {
		t.Fatalf("nothing new = %+v", same)
	}
	// Capped log (shorter than our offset): start again at the tail.
	os.WriteFile(path, []byte("new\n"), 0o644)
	c, _ = readLogChunk(path, c.Next)
	if !c.Reset || strings.Join(c.Lines, ",") != "new" {
		t.Fatalf("after rotation = %+v", c)
	}
	// A long log: the tail starts at a line boundary.
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("0123456789 line\n")
	}
	os.WriteFile(path, []byte(b.String()), 0o644)
	c, _ = readLogChunk(path, -1)
	if len(c.Lines) == 0 || len(c.Lines) > logTailBytes/16+1 {
		t.Fatalf("tail has %d lines", len(c.Lines))
	}
	for _, l := range c.Lines {
		if l != "0123456789 line" {
			t.Fatalf("partial line %q in the tail", l)
		}
	}
}

func TestMinerTrack(t *testing.T) {
	var m minerTrack
	t0 := time.Unix(1_700_000_000, 0)
	st := func(state string, rate float64, acc int64) *minercore.MinerStatus {
		return &minercore.MinerStatus{State: state, Hashrate: rate, Accepted: acc}
	}
	h, last := m.update(true, st("connecting", 0, 0), t0)
	if len(h) != 1 || h[0] != 0 || last != 0 {
		t.Fatalf("connecting: %v %d", h, last)
	}
	h, last = m.update(true, st("mining", 3200, 1), t0.Add(5*time.Second))
	if h[1] != 3200 || last != t0.Add(5*time.Second).Unix() {
		t.Fatalf("first share: %v %d", h, last)
	}
	_, last = m.update(true, st("mining", 3100, 1), t0.Add(10*time.Second))
	if last != t0.Add(5*time.Second).Unix() {
		t.Fatalf("no new share moved the time: %d", last)
	}
	// A restarted miner counts from 0 again: no phantom share, and "last
	// share" starts over with the count the view shows next to it.
	_, last = m.update(true, st("mining", 3000, 0), t0.Add(15*time.Second))
	if last != 0 {
		t.Fatalf("restart kept the old share time: %d", last)
	}
	_, last = m.update(true, st("mining", 3000, 1), t0.Add(20*time.Second))
	if last != t0.Add(20*time.Second).Unix() {
		t.Fatalf("first share after restart: %d", last)
	}
	for i := 0; i < historyLen+10; i++ {
		h, _ = m.update(true, st("mining", float64(i), 0), t0)
	}
	if len(h) != historyLen || h[len(h)-1] != historyLen+9 {
		t.Fatalf("history len %d, last %v", len(h), h[len(h)-1])
	}
	h, last = m.update(false, nil, t0)
	if h != nil || last != 0 || len(m.history) != 0 {
		t.Fatal("miner off must clear the history")
	}
}

func TestReadLogChunkLongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miner.log")
	os.WriteFile(path, append([]byte("start\n"), bytes.Repeat([]byte("ä"), logChunkBytes)...), 0o644)
	c, err := readLogChunk(path, 6) // after "start\n"
	if err != nil || len(c.Lines) != 1 || c.Next <= 6 {
		t.Fatalf("long line: next %d, %d lines, %v", c.Next, len(c.Lines), err)
	}
	if !utf8.ValidString(c.Lines[0]) {
		t.Fatal("long line cut inside a character")
	}
	next, _ := readLogChunk(path, c.Next)
	if next.Next <= c.Next {
		t.Fatal("live log stands still on a long line")
	}
}
