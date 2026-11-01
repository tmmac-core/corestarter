package minercore

import (
	"errors"
	"net"
	"sort"
	"time"
)

// Measurement is the TCP connect time to one server in ms, -1 = not reachable.
type Measurement struct {
	Endpoint Endpoint
	Millis   int64
}

// RankEndpoints measures in parallel how fast a TCP connection to each server
// can be opened, and sorts by that. Nothing is sent; the servers all belong to
// the same pool, which sees the IP when mining anyway. Unreachable servers go
// last in their original order, so failover still tries them in case the
// measurement failed only by chance.
func RankEndpoints(eps []Endpoint, timeout time.Duration, connect func(Endpoint, time.Duration) (int64, error)) []Measurement {
	out := make([]Measurement, len(eps))
	if len(eps) <= 1 {
		for i, e := range eps {
			out[i] = Measurement{e, -1}
		}
		return out
	}
	if connect == nil {
		connect = tcpConnectMillis
	}
	type result struct {
		i  int
		ms int64
	}
	done := make(chan result, len(eps))
	for i, e := range eps {
		out[i] = Measurement{e, -1}
		go func() {
			ms, err := connect(e, timeout)
			if err != nil {
				ms = -1
			}
			done <- result{i, ms}
		}()
	}
	// A filter in the network stack can hold a connect past its timeout; such a
	// server counts as unreachable instead of stalling every connection.
	limit := time.NewTimer(timeout + rankGrace)
	defer limit.Stop()
	for range eps {
		select {
		case r := <-done:
			out[r.i].Millis = r.ms
			continue
		case <-limit.C:
		}
		break
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a].Millis, out[b].Millis
		if x < 0 {
			return false
		}
		return y < 0 || x < y
	})
	return out
}

func tcpConnectMillis(e Endpoint, timeout time.Duration) (int64, error) {
	t0 := time.Now()
	c, err := dialGuarded(e.String(), timeout)
	if err != nil {
		return 0, err
	}
	c.Close()
	return time.Since(t0).Milliseconds(), nil
}

// rankGrace: how long past its own timeout a connect may take before it is
// given up (see dialGuarded).
const rankGrace = 2 * time.Second

var errDialStuck = errors.New("connect did not return in time (blocked by a filter?)")

// dialGuarded is net.DialTimeout that returns even when the connect itself
// hangs past its timeout, as seen with network filters of some antivirus
// programs on Windows. A late connection is closed.
func dialGuarded(addr string, timeout time.Duration) (net.Conn, error) {
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := net.DialTimeout("tcp", addr, timeout)
		ch <- res{c, err}
	}()
	t := time.NewTimer(timeout + rankGrace)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-t.C:
		go func() {
			if r := <-ch; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, errDialStuck
	}
}
