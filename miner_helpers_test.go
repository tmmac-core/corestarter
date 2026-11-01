//go:build (windows || linux || darwin) && !nominer

package main

import (
	"bufio"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A pool on loopback that accepts every share (the hash check itself is
// covered by cmd/core-miner's tests).
func acceptAllPool(t *testing.T) (addr string, accepted *atomic.Int32) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accepted = &atomic.Int32{}
	idRe := regexp.MustCompile(`"id":(\d+)`)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.Contains(line, "eth_submitLogin"):
						c.Write([]byte(`{"id":1,"jsonrpc":"2.0","result":true}` + "\n"))
					case strings.Contains(line, "eth_getWork"):
						c.Write([]byte(`{"id":5,"jsonrpc":"2.0","result":["0x` + strings.Repeat("ab", 32) + `","0x00","0x0f` + strings.Repeat("ff", 31) + `"]}` + "\n"))
					case strings.Contains(line, "eth_submitWork"):
						accepted.Add(1)
						c.Write([]byte(`{"id":` + idRe.FindStringSubmatch(line)[1] + `,"jsonrpc":"2.0","result":true}` + "\n"))
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), accepted
}

func waitView(t *testing.T, m *minerSup, cond func(on, throttled bool, threads int) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		on, throttled, st, _ := m.View()
		if st != nil && cond(on, throttled, st.Threads) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	_, throttled, st, _ := m.View()
	t.Fatalf("condition not reached: throttled=%v status=%+v", throttled, st)
}

func waitFailure(t *testing.T, m *minerSup) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if _, _, _, f := m.View(); f != "" {
			t.Logf("failure: %s", f)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("bad address did not end in a failure state")
}
