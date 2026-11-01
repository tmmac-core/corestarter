package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// logChunk is a piece of miner.log for the window's live log.
type logChunk struct {
	Next  int64    `json:"next"`  // offset to ask for next time
	Reset bool     `json:"reset"` // started over (first call or the log was rotated)
	Lines []string `json:"lines"`
}

const (
	logTailBytes  = 8 << 10  // first look: the last few dozen lines
	logChunkBytes = 64 << 10 // most per call
)

// readLogChunk returns the complete lines after offset from. A negative or
// stale offset (the log was capped and started anew) starts at the tail. A
// line still being written stays for the next call.
func readLogChunk(path string, from int64) (logChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return logChunk{Next: -1}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return logChunk{Next: -1}, err
	}
	size := fi.Size()
	c := logChunk{}
	start := from
	if from < 0 || from > size {
		c.Reset = true
		start = size - logTailBytes
		if start < 0 {
			start = 0
		}
	}
	n := size - start
	if n > logChunkBytes {
		n = logChunkBytes
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return logChunk{Next: -1}, err
	}
	skip := 0
	if c.Reset && start > 0 {
		// Started in the middle of a line: begin with the next one.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			skip = i + 1
		} else {
			skip = len(buf)
		}
	}
	end := bytes.LastIndexByte(buf, '\n') + 1
	if end < skip {
		end = skip
	}
	if end == skip && int64(len(buf)) == logChunkBytes {
		// A full chunk without a line break: hand it out as one line (cut at
		// a UTF-8 boundary), else the live log would stand still on it.
		end = len(buf)
		i := end - 1
		for i > skip && !utf8.RuneStart(buf[i]) {
			i--
		}
		if !utf8.FullRune(buf[i:end]) && i > skip {
			end = i // the last character continues in the next chunk
		}
	}
	for _, l := range strings.Split(string(buf[skip:end]), "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			c.Lines = append(c.Lines, l)
		}
	}
	c.Next = start + int64(end)
	return c, nil
}
