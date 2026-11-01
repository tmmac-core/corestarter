package minercore

import (
	"errors"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const donation = "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963"

func TestICAN(t *testing.T) {
	for _, in := range []string{
		donation,
		"  CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963 ",
		"payto://xcb/" + donation + "?donate=1&item=Test",
		"corepass:" + donation,
		"payto://xcb/" + donation + ":12.5",
		strings.ReplaceAll("CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963", " ", " "),
		strings.ReplaceAll("CB29 2120 D3AB C3B8 330A 26DA F4BE B94B 1F6A 9C7B 4963", " ", "\t"),
	} {
		a, err := ParseICAN(in)
		if err != nil || a.Address != donation || a.Network != Mainnet {
			t.Errorf("%q: %+v %v", in, a, err)
		}
	}
	if a, err := ParseICAN("ab472120d3abc3b8330a26daf4beb94b1f6a9c7b4963"); err != nil || a.Network != Testnet {
		t.Errorf("testnet: %+v %v", a, err)
	}
	for in, want := range map[string]error{
		donation[:len(donation)-1] + "4":               ErrAddressChecksum,
		donation[:10] + "ba" + donation[12:]:           ErrAddressChecksum,
		"0x12ab":                                       ErrAddressFormat,
		"xy292120d3abc3b8330a26daf4beb94b1f6a9c7b4963": ErrAddressFormat,
		donation + "0":                                 ErrAddressFormat,
		"":                                             ErrAddressFormat,
	} {
		if _, err := ParseICAN(in); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", in, err, want)
		}
	}
}

func TestWorkerName(t *testing.T) {
	for in, want := range map[string]string{
		"My PC_1":                 "my-pc-1",
		"äöü!!":                   "",
		"---":                     "",
		"a-very-long-worker-name": "a-very-long-wor",
	} {
		if got := SanitizeWorker(in); got != want {
			t.Errorf("SanitizeWorker(%q) = %q, want %q", in, got, want)
		}
	}
	s := SuggestWorker("pc")
	if len(s) != 7 || !strings.HasPrefix(s, "pc-") || SanitizeWorker(s) != s {
		t.Errorf("suggestion %q", s)
	}
}

func TestThreadsFor(t *testing.T) {
	for _, c := range []struct{ pct, max, want int }{
		{25, 8, 2}, {50, 8, 4}, {75, 8, 6}, {100, 8, 8}, {50, 6, 3}, {75, 6, 5}, {25, 1, 1}, {100, 0, 1},
	} {
		if got := ThreadsFor(c.pct, c.max); got != c.want {
			t.Errorf("ThreadsFor(%d, %d) = %d, want %d", c.pct, c.max, got, c.want)
		}
	}
}

func TestRanking(t *testing.T) {
	a, b, c := Endpoint{"a.x", 1}, Endpoint{"b.x", 1}, Endpoint{"c.x", 1}
	ms := map[Endpoint]int64{a: 80, b: -1, c: 20}
	got := RankEndpoints([]Endpoint{a, b, c}, time.Second, func(e Endpoint, _ time.Duration) (int64, error) {
		if ms[e] < 0 {
			return 0, errors.New("down")
		}
		return ms[e], nil
	})
	if got[0].Endpoint != c || got[1].Endpoint != a || got[2].Endpoint != b || got[2].Millis != -1 {
		t.Fatalf("%v", got)
	}
}

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile("testdata/ctr/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPoolAPIParsing(t *testing.T) {
	s, err := ParsePoolSettings(fixture(t, "settings.json"))
	if err != nil || math.Abs(s.PayoutThresholdXCB-0.1) > 1e-12 || s.PayoutIntervalMin != 240 || math.Abs(s.FeePercent-0.5) > 1e-9 {
		t.Errorf("settings %+v %v", s, err)
	}
	n, err := ParsePoolStats(fixture(t, "stats.json"))
	if err != nil || math.Abs(n.BlockRewardXCB-5) > 1e-9 || n.Difficulty < 1e8 || n.BlockTimeS < 5 || n.BlockTimeS > 12 || n.PoolHashrate <= 0 {
		t.Errorf("stats %+v %v", n, err)
	}
	a, err := ParseAccount(fixture(t, "account.json"))
	if err != nil || a.BalanceXCB < 0 || a.BalanceXCB > 10 {
		t.Errorf("account %+v %v", a, err)
	}
	net := PoolNetwork{PoolHashrate: 6.4e6, Difficulty: 534_722_549, BlockTimeS: 7.6367, BlockRewardXCB: 5}
	if d := XCBPerDay(1540, net, 0.5); math.Abs(d-1.2379) > 0.001 {
		t.Errorf("XCBPerDay = %v", d)
	}
	if APIBaseURL("fi.catchthatrabbit.com") != "https://fi-api.catchthatrabbit.com" || APIBaseURL("127.0.0.1") != "https://de-api.catchthatrabbit.com" {
		t.Error("APIBaseURL")
	}
}

func TestEarningsAcrossRegions(t *testing.T) {
	account := func(bal, imm, paid string) []byte {
		return []byte(`{"stats":{"balance":` + bal + `,"immature":` + imm + `,"paid":` + paid + `}}`)
	}
	e, err := FetchEarnings("fi.catchthatrabbit.com", "cb00", func(url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, "/api/settings"):
			return fixture(t, "settings.json"), nil
		case strings.HasSuffix(url, "/api/stats"):
			return fixture(t, "stats.json"), nil
		case strings.HasPrefix(url, "https://de-api"):
			return account("81871310", "5734431", "368299984"), nil
		case strings.HasPrefix(url, "https://fi-api"):
			return account("91198202", "0", "0"), nil
		case strings.HasPrefix(url, "https://sg-api"):
			return account(`"3886285"`, "0", "0"), nil
		}
		return nil, HTTPError{404}
	})
	if err != nil {
		t.Fatal(err)
	}
	tot := e.Total()
	if math.Abs(tot.BalanceXCB-0.176955797) > 1e-9 || math.Abs(tot.ImmatureXCB-0.005734431) > 1e-9 || math.Abs(tot.PaidXCB-0.368299984) > 1e-9 {
		t.Errorf("total %+v", tot)
	}
	open := e.OpenRegions()
	if len(open) != 3 || open[0].Region != "fi" || open[1].Region != "de" || open[2].Region != "sg" || e.Incomplete() {
		t.Errorf("open %+v incomplete %v", open, e.Incomplete())
	}

	e, _ = FetchEarnings("", "cb00", func(url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, "/api/settings"):
			return fixture(t, "settings.json"), nil
		case strings.HasSuffix(url, "/api/stats"):
			return fixture(t, "stats.json"), nil
		case strings.HasPrefix(url, "https://hk-api"):
			return nil, HTTPError{503}
		}
		return account("100000000", "0", "0"), nil
	})
	if math.Abs(e.Total().BalanceXCB-0.3) > 1e-9 || !e.Incomplete() {
		t.Errorf("missing region: %+v", e.Total())
	}

	e, _ = FetchEarnings("", "", func(url string) ([]byte, error) {
		if strings.Contains(url, "/accounts/") {
			t.Errorf("account fetched without address: %s", url)
		}
		if strings.HasSuffix(url, "/api/settings") {
			return fixture(t, "settings.json"), nil
		}
		return fixture(t, "stats.json"), nil
	})
	if e.Total() != nil {
		t.Error("total without address")
	}
}

// The support connection is a fake pool on loopback that pushes one job after login.
func TestSupportShareSlice(t *testing.T) {
	target := "0x" + strings.Repeat("ff", 32)
	supportPool := newFakePool(t, func(line string) []string {
		switch {
		case strings.Contains(line, "eth_submitLogin"):
			return []string{`{"id":1,"jsonrpc":"2.0","result":true}`}
		case strings.Contains(line, "eth_getWork"):
			return []string{`{"id":5,"jsonrpc":"2.0","result":["0x` + strings.Repeat("aa", 32) + `","0x00","` + target + `"]}`}
		case strings.Contains(line, "eth_submitWork"):
			return []string{`{"id":` + idRe.FindStringSubmatch(line)[1] + `,"jsonrpc":"2.0","result":true}`}
		}
		return nil
	})
	var mu sync.Mutex
	var current *Job
	var slices []bool
	var accepted atomic.Int32
	userJob := &Job{Header: [32]byte{1}}
	s := &SupportShare{
		Connect: func(ev Events) *Client {
			return &Client{Endpoints: []Endpoint{supportPool.ep}, Wallet: SupportAddress, Worker: SupportWorker, Events: ev}
		},
		SetJob:   func(j *Job) { mu.Lock(); current = j; mu.Unlock() },
		OnSlice:  func(a bool) { mu.Lock(); slices = append(slices, a); mu.Unlock() },
		OnResult: func(ok bool, _ string) { accepted.Add(1) },
		Window:   2 * time.Second, Slice: 400 * time.Millisecond, Drain: 100 * time.Millisecond,
		RandFrac: func() float64 { return 0 },
	}
	s.Start()
	defer s.Stop()
	s.OnUserJob(userJob)

	var supportJob *Job
	waitFor(t, "support job", 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		if current != nil && current != userJob {
			supportJob = current
			return true
		}
		return false
	})
	if supportJob.Header[0] != 0xaa {
		t.Fatalf("unexpected job %x", supportJob.Header)
	}
	// A user job arriving during the slice is only remembered.
	newer := &Job{Header: [32]byte{2}}
	s.OnUserJob(newer)
	mu.Lock()
	if current != supportJob {
		t.Error("user job replaced the support job during the slice")
	}
	mu.Unlock()
	supportJob.Hashes.Add(1000)
	if !s.SubmitIfSupport(supportJob, 7, [32]byte{}) || s.SubmitIfSupport(newer, 8, [32]byte{}) {
		t.Fatal("share routing wrong")
	}
	waitFor(t, "accepted support share", 5*time.Second, func() bool { return accepted.Load() == 1 })
	waitFor(t, "end of slice", 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return current == newer
	})
	waitFor(t, "hash count", 5*time.Second, func() bool { return s.Hashes() == 1000 })
	mu.Lock()
	if len(slices) < 2 || !slices[0] || slices[1] {
		t.Errorf("slice events %v", slices)
	}
	mu.Unlock()
	if s.Duration() < 300*time.Millisecond {
		t.Errorf("duration %v", s.Duration())
	}
}

func TestSupportShareSkippedWithoutJob(t *testing.T) {
	ln, _ := newDeadEndpoint()
	var current atomic.Pointer[Job]
	user := &Job{}
	s := &SupportShare{
		Connect: func(ev Events) *Client {
			return &Client{Endpoints: []Endpoint{ln}, Wallet: SupportAddress, Worker: SupportWorker, Events: ev,
				ConnectTimeout: 200 * time.Millisecond, InitialBackoff: 50 * time.Millisecond}
		},
		SetJob: func(j *Job) { current.Store(j) },
		Window: 2 * time.Second, Slice: 200 * time.Millisecond, JobWait: 300 * time.Millisecond, Drain: 10 * time.Millisecond,
		RandFrac: func() float64 { return 0 },
	}
	s.Start()
	s.OnUserJob(user)
	time.Sleep(700 * time.Millisecond)
	s.Stop()
	if current.Load() != user || s.Hashes() != 0 || s.Duration() != 0 {
		t.Errorf("skipped slice changed state: job=%v hashes=%d", current.Load() == user, s.Hashes())
	}
}

func newDeadEndpoint() (Endpoint, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Endpoint{}, err
	}
	ep := Endpoint{"127.0.0.1", l.Addr().(*net.TCPAddr).Port}
	l.Close()
	return ep, nil
}

func TestPoolUnreachable(t *testing.T) {
	for _, c := range []struct {
		st   MinerStatus
		want bool
	}{
		{MinerStatus{State: "connecting", PoolNever: true, OfflineSecs: 119}, false},
		{MinerStatus{State: "connecting", PoolNever: true, OfflineSecs: 120}, true},
		{MinerStatus{State: "connecting", OfflineSecs: 600}, false}, // had a session: a reconnect may take a while
		{MinerStatus{State: "connecting", OfflineSecs: 900}, true},
		{MinerStatus{State: "mining", PoolNever: true, OfflineSecs: 900}, false},
	} {
		if got := c.st.PoolUnreachable(); got != c.want {
			t.Errorf("%+v: %v, want %v", c.st, got, c.want)
		}
	}
	if (*MinerStatus)(nil).PoolUnreachable() {
		t.Error("nil status counts as unreachable")
	}
}

// A connect that hangs past its timeout (seen with antivirus network filters)
// must not stall the ranking, and so the whole pool connection.
func TestRankingStuckConnect(t *testing.T) {
	a, b := Endpoint{"a.x", 1}, Endpoint{"b.x", 1}
	release := make(chan struct{})
	defer close(release)
	t0 := time.Now()
	got := RankEndpoints([]Endpoint{a, b}, 50*time.Millisecond, func(e Endpoint, _ time.Duration) (int64, error) {
		if e == a {
			<-release
		}
		return 10, nil
	})
	if took := time.Since(t0); took > 50*time.Millisecond+rankGrace+time.Second {
		t.Fatalf("ranking took %v", took)
	}
	if got[0].Endpoint != b || got[0].Millis != 10 || got[1].Endpoint != a || got[1].Millis != -1 {
		t.Fatalf("%v", got)
	}
}
