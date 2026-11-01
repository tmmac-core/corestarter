package minercore

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A share found on a support job after the slice ended (slow last batch) must
// never go to the user's connection.
func TestLateSupportShareNeverGoesToUser(t *testing.T) {
	s := &SupportShare{}
	s.mu.Lock()
	s.jobs = map[*Job]bool{}
	s.mu.Unlock()
	late := &Job{FromSupport: true} // no longer in s.jobs, no client
	if !s.SubmitIfSupport(late, 1, [32]byte{}) {
		t.Fatal("late support share was handed to the user's connection")
	}
	if s.SubmitIfSupport(&Job{}, 1, [32]byte{}) {
		t.Fatal("user share was taken by the support share")
	}
}

// Over many windows the support share never takes more than Slice/Window.
func TestSupportShareNeverMoreThanStated(t *testing.T) {
	var mu sync.Mutex
	var slices int
	var frac atomic.Uint64
	vals := []float64{0, 0.5, 0.999, 0.9999} // the last two would overrun the window
	var i atomic.Int32
	s := &SupportShare{
		Window: 40 * time.Millisecond, Slice: 10 * time.Millisecond, JobWait: 5 * time.Millisecond, Drain: time.Millisecond,
		RandFrac: func() float64 {
			v := vals[int(i.Add(1)-1)%len(vals)]
			frac.Store(math.Float64bits(v))
			return v
		},
		Connect: func(Events) *Client {
			mu.Lock()
			slices++
			mu.Unlock()
			return &Client{Endpoints: []Endpoint{{"127.0.0.1", 1}}}
		},
		SetJob: func(*Job) {},
	}
	s.Start()
	time.Sleep(330 * time.Millisecond)
	s.Stop()
	mu.Lock()
	defer mu.Unlock()
	windows := int(i.Load())
	// 0.999 and 0.9999 do not fit (offset + slice > window): only half the windows get a slice.
	if windows < 4 || slices > (windows+1)/2+1 {
		t.Fatalf("%d slices in %d windows", slices, windows)
	}
}

func TestPoolAPIRejectsNonFiniteNumbers(t *testing.T) {
	for _, v := range []string{`"NaN"`, `"Inf"`, `"-Inf"`, `"1e400"`, `-5`} {
		m := map[string]json.RawMessage{"x": json.RawMessage(v)}
		if got := num(m, "x"); got != 0 {
			t.Fatalf("num(%s) = %v, want 0", v, got)
		}
	}
	if XCBPerDay(1e3, PoolNetwork{Difficulty: math.Inf(1), BlockTimeS: 7, BlockRewardXCB: 1}, 1) != 0 {
		t.Fatal("XCBPerDay must stay finite")
	}
}

// After the login, a message with id 1 is not a login answer and must not stop the client.
func TestLateIDOneIsNotALoginRejection(t *testing.T) {
	pool := newFakePool(t, func(line string) []string {
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
		case strings.Contains(line, "eth_getWork"):
			return []string{
				`{"id":5,"jsonrpc":"2.0","result":["0x` + testHeader + `","0x00","0x00ff"]}`,
				`{"id":1,"jsonrpc":"2.0","result":false}`,
			}
		}
		return nil
	})
	var rejected atomic.Bool
	var job atomic.Pointer[Job]
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "w", Events: Events{
		Job:           func(j *Job) { job.Store(j) },
		LoginRejected: func(string) { rejected.Store(true) },
	}}
	c.Start()
	defer c.Stop()
	waitFor(t, "job", 5*time.Second, func() bool { return job.Load() != nil })
	time.Sleep(200 * time.Millisecond)
	if rejected.Load() || !c.Running() {
		t.Fatal("a late id-1 message stopped the client")
	}
}

func TestShortHeaderIsNotAJob(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`"0x1234"`), json.RawMessage(`"0x00"`), json.RawMessage(`"0x00ff"`)}
	if _, err := parseJob(raw); err == nil {
		t.Fatal("2-byte header accepted")
	}
}

// A pool that sends jobs but never answers shares cannot grow memory without bound.
func TestPendingSharesAreCapped(t *testing.T) {
	pool := newFakePool(t, func(line string) []string {
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
		case strings.Contains(line, "eth_getWork"):
			return []string{`{"id":5,"jsonrpc":"2.0","result":["0x` + testHeader + `","0x00","0x00ff"]}`}
		}
		return nil // never answers shares
	})
	var job atomic.Pointer[Job]
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "w", Events: Events{
		Job: func(j *Job) { job.Store(j) },
	}}
	c.Start()
	defer c.Stop()
	waitFor(t, "job", 5*time.Second, func() bool { return job.Load() != nil })
	for n := uint64(0); n < 3*maxPending; n++ {
		c.Submit(job.Load(), n, [32]byte{})
	}
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n > maxPending {
		t.Fatalf("%d pending shares, cap %d", n, maxPending)
	}
}

// Without any reachable server at the start there is no home yet; it is chosen
// once a server answers, not fixed to the first list entry.
func TestNoHomeWithoutNetwork(t *testing.T) {
	a, b := newFakePool(t, loginOK), newFakePool(t, loginOK)
	var calls atomic.Int32
	rank := func([]Endpoint) []Measurement {
		if calls.Add(1) == 1 {
			return []Measurement{{a.ep, -1}, {b.ep, -1}}
		}
		return []Measurement{{b.ep, 20}, {a.ep, 90}}
	}
	c := &Client{Endpoints: []Endpoint{a.ep, b.ep}, Wallet: "cb00", Worker: "w",
		InitialBackoff: 20 * time.Millisecond, RankByLatency: true, Rank: rank}
	c.mu.Lock()
	c.home = nil
	c.mu.Unlock()
	c.Start()
	defer c.Stop()
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.home != nil && *c.home == a.ep && calls.Load() == 1 {
		t.Fatal("home fixed to the first list entry without any measurement")
	}
}

// After standby the miner drops the session; the client logs in again and gets a fresh job.
func TestReconnectGivesFreshSession(t *testing.T) {
	pool := newFakePool(t, func(line string) []string {
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
		case strings.Contains(line, "eth_getWork"):
			return []string{`{"id":5,"jsonrpc":"2.0","result":["0x` + testHeader + `","0x00","0x00ff"]}`}
		}
		return nil
	})
	var jobs, disconnects atomic.Int32
	c := &Client{Endpoints: []Endpoint{pool.ep}, Wallet: "cb00", Worker: "w", InitialBackoff: 20 * time.Millisecond, Events: Events{
		Job:          func(*Job) { jobs.Add(1) },
		Disconnected: func(string) { disconnects.Add(1) },
	}}
	c.Start()
	defer c.Stop()
	waitFor(t, "first job", 5*time.Second, func() bool { return jobs.Load() >= 1 })
	c.Reconnect()
	waitFor(t, "second login", 5*time.Second, func() bool { return len(pool.lines("eth_submitLogin")) >= 2 && jobs.Load() >= 2 })
	if disconnects.Load() < 1 {
		t.Fatal("no disconnect reported, threads would keep the stale job")
	}
}
