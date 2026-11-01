package minercore

import (
	"bufio"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePool answers line-based JSON on loopback; no real network, no mining.
type fakePool struct {
	ln       net.Listener
	ep       Endpoint
	mu       sync.Mutex
	received []string
	conns    []net.Conn
}

func newFakePool(t *testing.T, reply func(line string) []string) *fakePool {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePool{ln: ln, ep: Endpoint{"127.0.0.1", ln.Addr().(*net.TCPAddr).Port}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.conns = append(p.conns, c)
			p.mu.Unlock()
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					p.mu.Lock()
					p.received = append(p.received, line)
					p.mu.Unlock()
					for _, out := range reply(line) {
						c.Write([]byte(out + "\n"))
					}
				}
			}()
		}
	}()
	t.Cleanup(p.close)
	return p
}

func (p *fakePool) close() {
	p.ln.Close()
	p.mu.Lock()
	for _, c := range p.conns {
		c.Close()
	}
	p.mu.Unlock()
}

func (p *fakePool) lines(substr string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, l := range p.received {
		if strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return out
}

const testHeader = "1111111111111111111111111111111111111111111111111111111111111111"

var idRe = regexp.MustCompile(`"id":(\d+)`)

func loginOK(line string) []string {
	if strings.Contains(line, "eth_submitLogin") {
		return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
	}
	return nil
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoginJobAndAcceptedShare(t *testing.T) {
	target := "0x000000" + strings.Repeat("ff", 29)
	pool := newFakePool(t, func(line string) []string {
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
		case strings.Contains(line, "eth_getWork"):
			return []string{`{"id":5,"jsonrpc":"2.0","result":["0x` + testHeader + `","0x` + strings.Repeat("22", 32) + `","` + target + `"]}`}
		case strings.Contains(line, "eth_submitWork"):
			return []string{`{"id":` + idRe.FindStringSubmatch(line)[1] + `,"jsonrpc":"2.0","result":true}`}
		}
		return nil
	})
	var job atomic.Pointer[Job]
	var results []bool
	var mu sync.Mutex
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "test-worker", Events: Events{
		Job: func(j *Job) { job.Store(j) },
		ShareResult: func(ok bool, _ string) {
			mu.Lock()
			results = append(results, ok)
			mu.Unlock()
		},
	}}
	c.Start()
	defer c.Stop()
	waitFor(t, "job", 5*time.Second, func() bool { return job.Load() != nil })
	login := pool.lines("eth_submitLogin")[0]
	if !strings.Contains(login, `"worker":"test-worker"`) || !strings.Contains(login, "cb00") {
		t.Fatalf("login: %s", login)
	}
	j := job.Load()
	if j.HeaderHex() != testHeader || j.Boundary[2] != 0 || j.Boundary[3] != 0xff {
		t.Fatalf("job parsed wrong: %x %x", j.Header, j.Boundary)
	}
	c.Submit(j, 0x1234, [32]byte{})
	waitFor(t, "share answer", 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(results) == 1
	})
	if !results[0] {
		t.Fatal("share not accepted")
	}
	if s := pool.lines("eth_submitWork")[0]; !strings.Contains(s, `"0x0000000000001234"`) {
		t.Fatalf("submit: %s", s)
	}
}

func TestRejectedLoginStopsClient(t *testing.T) {
	pool := newFakePool(t, func(line string) []string {
		if strings.Contains(line, "eth_submitLogin") {
			return []string{`{"id":1,"jsonrpc":"2.0","result":null,"error":{"code":-1,"message":"invalid address"}}`}
		}
		return nil
	})
	var reason atomic.Value
	var connected, disconnected atomic.Int32
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "w", InitialBackoff: 50 * time.Millisecond,
		Events: Events{
			LoginRejected: func(r string) { reason.Store(r) },
			Connected:     func(Endpoint) { connected.Add(1) },
			Disconnected:  func(string) { disconnected.Add(1) },
		}}
	c.Start()
	defer c.Stop()
	waitFor(t, "rejection", 5*time.Second, func() bool { return reason.Load() != nil })
	if reason.Load() != "invalid address" {
		t.Fatalf("reason %v", reason.Load())
	}
	time.Sleep(300 * time.Millisecond) // a regression would reconnect within these 6 backoff rounds
	if n := len(pool.lines("eth_submitLogin")); n != 1 || connected.Load() != 0 || disconnected.Load() != 0 || c.Running() {
		t.Fatalf("after rejection: logins=%d connected=%d disconnects=%d running=%v", n, connected.Load(), disconnected.Load(), c.Running())
	}
}

func TestSkipsDeadServer(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := Endpoint{"127.0.0.1", ln.Addr().(*net.TCPAddr).Port}
	ln.Close()
	pool := newFakePool(t, loginOK)
	var to atomic.Value
	c := &Client{Endpoints: []Endpoint{dead, pool.ep}, Wallet: "cb00", Worker: "w",
		InitialBackoff: 50 * time.Millisecond, ConnectTimeout: 500 * time.Millisecond,
		Events: Events{Connected: func(e Endpoint) { to.Store(e) }}}
	c.Start()
	defer c.Stop()
	waitFor(t, "failover", 10*time.Second, func() bool { return to.Load() != nil })
	if to.Load() != pool.ep {
		t.Fatalf("connected to %v", to.Load())
	}
}

func TestStaysHomeEvenIfAnotherGetsFaster(t *testing.T) {
	home, other := newFakePool(t, loginOK), newFakePool(t, loginOK)
	var calls atomic.Int32
	rank := func([]Endpoint) []Measurement {
		if calls.Add(1) == 1 {
			return []Measurement{{home.ep, 50}, {other.ep, -1}}
		}
		return []Measurement{{other.ep, 20}, {home.ep, 250}}
	}
	var mu sync.Mutex
	var connected []Endpoint
	c := &Client{Endpoints: []Endpoint{home.ep, other.ep}, Wallet: "cb00", Worker: "w",
		InitialBackoff: 50 * time.Millisecond, RankByLatency: true, RecheckInterval: 200 * time.Millisecond, Rank: rank,
		Events: Events{Connected: func(e Endpoint) { mu.Lock(); connected = append(connected, e); mu.Unlock() }}}
	c.Start()
	defer c.Stop()
	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(connected) != 1 || connected[0] != home.ep {
		t.Fatalf("connected %v, want only home", connected)
	}
}

func TestReturnsHomeAfterOutage(t *testing.T) {
	// Home drops the first two connections at once (outage), then logs in.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	home := Endpoint{"127.0.0.1", ln.Addr().(*net.TCPAddr).Port}
	var accepted atomic.Int32
	go func() {
		for {
			s, err := ln.Accept()
			if err != nil {
				return
			}
			if accepted.Add(1) <= 2 {
				s.Close()
				continue
			}
			go func() {
				defer s.Close()
				r := bufio.NewReader(s)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					for _, out := range loginOK(line) {
						s.Write([]byte(out + "\n"))
					}
				}
			}()
		}
	}()
	backup := newFakePool(t, loginOK)
	var calls atomic.Int32
	rank := func([]Endpoint) []Measurement {
		if calls.Add(1) == 1 {
			return []Measurement{{home, 20}, {backup.ep, 50}}
		}
		return []Measurement{{backup.ep, 10}, {home, 30}}
	}
	var mu sync.Mutex
	var connected []Endpoint
	c := &Client{Endpoints: []Endpoint{home, backup.ep}, Wallet: "cb00", Worker: "w",
		InitialBackoff: 50 * time.Millisecond, RankByLatency: true, RecheckInterval: 300 * time.Millisecond, Rank: rank,
		Events: Events{Connected: func(e Endpoint) { mu.Lock(); connected = append(connected, e); mu.Unlock() }}}
	c.Start()
	defer c.Stop()
	waitFor(t, "return to home", 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(connected) >= 2
	})
	mu.Lock()
	defer mu.Unlock()
	if connected[0] != backup.ep || connected[1] != home {
		t.Fatalf("connected %v, want backup then home", connected)
	}
}

func TestHexTo32PadsShortTargets(t *testing.T) {
	b, err := HexTo32("0xff")
	if err != nil || b[31] != 0xff || b[0] != 0 {
		t.Fatalf("%x %v", b, err)
	}
	if _, err := HexTo32("0x" + strings.Repeat("0", 66)); err == nil {
		t.Fatal("too long accepted")
	}
}

func TestOverlongLineDropsConnection(t *testing.T) {
	pool := newFakePool(t, func(line string) []string {
		if strings.Contains(line, "eth_submitLogin") {
			return []string{strings.Repeat("x", 70<<10)}
		}
		return nil
	})
	var reason atomic.Value
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "w", InitialBackoff: time.Hour,
		Events: Events{Disconnected: func(r string) { reason.Store(r) }}}
	c.Start()
	defer c.Stop()
	waitFor(t, "disconnect", 5*time.Second, func() bool { return reason.Load() != nil })
	if !strings.Contains(reason.Load().(string), "64 KiB") {
		t.Fatalf("reason %v", reason.Load())
	}
}
