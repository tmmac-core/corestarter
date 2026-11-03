// Command core-miner mines XCB on the CPU with RandomY at the Catch That Rabbit
// pool. It is started and supervised by CoreStarter, which talks to it through
// stdin/stdout (internal/minercore/protocol.go); it has no window of its own.
//
//	core-miner --address <cb…> [--worker name] [--power 25|50|75|100]
//	core-miner --selftest      check the hash path and exit
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

const (
	statusInterval   = 5 * time.Second
	hashrateWindow   = 12 // samples of statusInterval: average over one minute
	reportInterval   = 30 * time.Second
	batteryInterval  = 30 * time.Second
	coolDown         = 5 * time.Minute // shortest heat pause, so mining does not flap
	earningsFirst    = 30 * time.Second
	earningsInterval = 10 * time.Minute
	fullMemNeeds     = 1536 << 20 // dataset ~264 MiB plus scratchpads and headroom for the PC
)

type poolList []minercore.Endpoint

func (p *poolList) String() string { return fmt.Sprint(*p) }
func (p *poolList) Set(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return err
	}
	*p = append(*p, minercore.Endpoint{Host: host, Port: n})
	return nil
}

// out writes protocol lines; if the tray is gone (broken pipe) the miner ends.
type out struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (o *out) line(l minercore.MinerLine) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.enc.Encode(l); err != nil {
		var uv *json.UnsupportedValueError
		if errors.As(err, &uv) {
			return // a value JSON cannot carry (NaN); Encode wrote nothing
		}
		os.Exit(minercore.ExitOK) // the tray is gone
	}
}

func (o *out) logf(format string, args ...any) {
	o.line(minercore.MinerLine{Type: "log", Msg: fmt.Sprintf(format, args...)})
}

func main() {
	address := flag.String("address", "", "Core address that receives the payouts")
	worker := flag.String("worker", "pc", "worker name shown in the pool statistics")
	power := flag.Int("power", 50, "CPU share: 25, 50, 75 or 100")
	api := flag.Bool("api", true, "fetch earnings from the pool API")
	light := flag.Bool("light", false, "light mode (no 256 MiB dataset, slower)")
	selftestOnly := flag.Bool("selftest", false, "check the hash path and exit")
	bench := flag.Int("bench", 0, "measure the hash rate for this many seconds per variant (no pool) and exit")
	benchThreads := flag.Int("threads", 0, "threads for --bench (default: all)")
	var pools poolList
	flag.Var(&pools, "pool", "pool server host:port (repeatable; default: Catch That Rabbit)")
	flag.Parse()

	o := &out{enc: json.NewEncoder(os.Stdout)}
	if *selftestOnly {
		os.Exit(runSelfTest(o))
	}
	if *bench > 0 {
		os.Exit(runBench(*bench, *benchThreads))
	}
	addr, err := minercore.ParseICAN(*address)
	if err != nil {
		o.line(minercore.MinerLine{Type: "status", Status: &minercore.MinerStatus{State: "error", Error: err.Error()}})
		os.Exit(minercore.ExitBadArgs)
	}
	minerPriority()
	go watchParent()
	m := &miner{
		out: o, address: addr.Address, worker: minercore.SanitizeWorker(*worker), api: *api,
		maxThreads: maxThreads(), pools: pools,
	}
	if m.worker == "" {
		m.worker = "pc"
	}
	m.power.Store(int32(*power))
	fullMem := !*light && availableMemory() >= fullMemNeeds
	os.Exit(m.run(fullMem))
}

func runSelfTest(o *out) int {
	for _, jit := range []bool{true, false} {
		ctx, err := newContext(false, 1, jit)
		if err != nil {
			o.logf("init (jit=%v): %v", jit, err)
			return minercore.ExitInitFailed
		}
		vm, usedJIT := ctx.newVM()
		if vm == nil {
			ctx.release()
			o.logf("no VM (jit=%v)", jit)
			return minercore.ExitInitFailed
		}
		msg := selfTest(vm)
		vm.destroy()
		ctx.release()
		if msg != "" {
			o.logf("jit=%v: %s", usedJIT, msg)
			return minercore.ExitSelfTest
		}
		o.logf("self-test ok (jit=%v, %d vectors)", usedJIT, len(selfTestVectors))
	}
	return minercore.ExitOK
}

type miner struct {
	out        *out
	address    string
	worker     string
	api        bool
	maxThreads int
	pools      []minercore.Endpoint

	power        atomic.Int32
	eng          *engine
	client       *minercore.Client
	share        *minercore.SupportShare
	accepted     atomic.Int64
	rejected     atomic.Int64
	lastReject   atomic.Pointer[string] // the pool's reason for the last rejected share, shortened
	battery      atomic.Bool
	hot          atomic.Bool
	suppOn       atomic.Bool
	hasJob       atomic.Bool
	rate         atomic.Uint64 // float64 bits
	shareDiff    atomic.Uint64 // float64 bits, of the user's current job
	answer       atomic.Int64  // ns the pool took for the last accepted share
	threadMu     sync.Mutex
	threadRates  []float64 // H/s per thread, smoothed; guarded by threadMu
	earnings     atomic.Pointer[minercore.Earnings]
	stopOnce     sync.Once
	stopped      chan struct{}
	exitCode     atomic.Int32
	ready        atomic.Bool  // engine running; before that, stop/EOF just exits
	hadJob       atomic.Bool  // a job came from the pool since the start
	offlineSince atomic.Int64 // monotonic ns (since t0) when work from the pool stopped; 0 = has a job
	t0           time.Time
}

func (m *miner) run(fullMem bool) int {
	m.stopped = make(chan struct{})
	m.status("starting", "")
	go m.readCommands() // also during the RandomY init, which can take a minute
	t0 := time.Now()
	ctx, err := initVerified(fullMem, runtime.NumCPU(), true, m.out.logf)
	if err != nil {
		m.status("error", err.Error())
		if errors.As(err, &errSelfTest{}) {
			return minercore.ExitSelfTest
		}
		return minercore.ExitInitFailed
	}
	m.out.logf("RandomY ready in %.1f s (jit=%v, secure=%v, full memory=%v, hard AES=%v), up to %d threads",
		time.Since(t0).Seconds(), ctx.Flags()&flagJIT != 0, ctx.Flags()&flagSecure != 0, ctx.Flags()&flagFullMem != 0, ctx.Flags()&flagHardAES != 0, m.maxThreads)
	if powerNote != "" {
		m.out.logf("%s", powerNote)
	}

	endpoints := m.pools
	rank := false
	if len(endpoints) == 0 {
		endpoints, rank = minercore.CTRPools, true
	}
	// Fixed 0.42 %, not switchable (decided 19.10.2026, as in the mobile miner).
	m.share = &minercore.SupportShare{
		Connect: func(ev minercore.Events) *minercore.Client {
			// Same server as the user's session, so the TMMAC share lands in the same region.
			eps := endpoints
			if cur := m.client.CurrentEndpoint(); cur != nil {
				eps = append([]minercore.Endpoint{*cur}, without(endpoints, *cur)...)
			}
			return &minercore.Client{Endpoints: eps, Wallet: minercore.SupportAddress, Worker: minercore.SupportWorker, Events: ev}
		},
		SetJob:  func(j *minercore.Job) { m.eng.job.Store(j) },
		OnSlice: func(active bool) { m.suppOn.Store(active) },
		Log:     func(msg string) { m.out.logf("%s", msg) },
	}
	m.client = &minercore.Client{
		Endpoints: endpoints, Wallet: m.address, Worker: m.worker, RankByLatency: rank,
		Events: minercore.Events{
			Connected: func(e minercore.Endpoint) {
				m.out.logf("connected to %s as %s", e, m.worker)
			},
			Disconnected: func(reason string) {
				m.offlineSince.CompareAndSwap(0, m.mono())
				m.hasJob.Store(false)
				m.out.logf("pool connection lost: %s", reason)
				if m.share != nil {
					m.share.OnUserDisconnected()
				} else {
					m.eng.job.Store(nil)
				}
			},
			LoginRejected: func(reason string) {
				m.status("error", "pool rejected the login: "+reason)
				m.exitCode.Store(minercore.ExitLoginRejected)
				go m.stop()
			},
			Job: func(j *minercore.Job) {
				m.hasJob.Store(true)
				m.hadJob.Store(true)
				m.shareDiff.Store(floatBits(j.Difficulty()))
				m.offlineSince.Store(0) // only real work counts: a pool that takes the login and drops us stays "offline"
				if m.share != nil {
					m.share.OnUserJob(j)
				} else {
					m.eng.job.Store(j)
				}
			},
			ShareResult: func(ok bool, e string) {
				if ok {
					m.answer.Store(int64(m.client.LastShareAnswer()))
					m.accepted.Add(1) // logged by the sampler, at most once per tick
				} else {
					if r := []rune(e); len(r) > 200 {
						e = string(r[:200]) + "..."
					}
					m.lastReject.Store(&e)
					m.rejected.Add(1) // logged by the sampler, like accepted shares
				}
			},
			Log: func(msg string) { m.out.logf("%s", msg) },
		},
	}
	// The engine starts after client and support share exist: a thread that
	// finds a share, or onDead stopping everything, may use them right away.
	m.eng = &engine{ctx: ctx, threads: m.maxThreads, logf: m.out.logf, onSolution: m.onSolution}
	m.eng.onDead = func(wrongHashes bool) {
		if wrongHashes {
			m.status("error", "self-test failed on every hashing thread")
			m.exitCode.Store(minercore.ExitSelfTest)
		} else {
			m.status("error", "no hashing thread could start (memory?)")
			m.exitCode.Store(minercore.ExitInitFailed) // may work next time: retried
		}
		go m.stop()
	}
	m.eng.setActive(minercore.ThreadsFor(int(m.power.Load()), m.maxThreads))
	m.eng.start()
	if m.share != nil {
		m.share.Start() // before the client, so no job reaches the threads before the support share is set up
	}
	m.t0 = time.Now()
	m.offlineSince.Store(m.mono()) // no job yet
	m.client.Start()
	m.ready.Store(true)                                                      // from now on stop() has everything to wind down
	m.eng.setActive(minercore.ThreadsFor(int(m.power.Load()), m.maxThreads)) // a power command during init
	go m.sampler()
	go m.batteryWatch()
	if m.api {
		go m.earningsLoop()
	}
	<-m.stopped
	return int(m.exitCode.Load())
}

func without(eps []minercore.Endpoint, e minercore.Endpoint) []minercore.Endpoint {
	var out []minercore.Endpoint
	for _, x := range eps {
		if x != e {
			out = append(out, x)
		}
	}
	return out
}

func (m *miner) onSolution(j *minercore.Job, nonce uint64, hash [32]byte) {
	if m.share != nil && m.share.SubmitIfSupport(j, nonce, hash) {
		return
	}
	m.client.Submit(j, nonce, hash)
}

func (m *miner) stop() {
	m.stopOnce.Do(func() {
		m.client.Stop()
		if m.share != nil {
			m.share.Stop()
		}
		m.eng.stop()
		close(m.stopped)
	})
}

// readCommands: EOF on stdin means the tray is gone or wants us to stop.
func (m *miner) readCommands() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var c minercore.Command
		if json.Unmarshal(sc.Bytes(), &c) != nil {
			continue
		}
		switch c.Cmd {
		case "power":
			m.power.Store(int32(c.Percent))
			if !m.ready.Load() {
				continue // run applies m.power once the engine exists
			}
			n := minercore.ThreadsFor(c.Percent, m.maxThreads)
			m.eng.setActive(n)
			m.out.logf("power %d %% = %d thread(s)", c.Percent, n)
		case "stop":
			m.stopOrExit()
			return
		}
	}
	m.stopOrExit()
}

// stopOrExit: during init there is nothing to wind down yet.
func (m *miner) stopOrExit() {
	if !m.ready.Load() {
		os.Exit(minercore.ExitOK)
	}
	m.stop()
}

func (m *miner) sampler() {
	id := make([]byte, 32)
	rand.Read(id)
	clientID := "0x" + hex.EncodeToString(id)
	var samples []float64
	last, lastT := m.eng.hashes.Load(), time.Now()
	lastWall := lastT.Round(0) // wall clock: also counts sleep on macOS
	lastReport, lastSummary := time.Now(), time.Now()
	var loggedAccepted, loggedRejected int64
	warnedOffline := false // "pool unreachable" is in miner.log for this offline spell
	threadLast := make([]int64, m.eng.threads)
	tick := time.NewTicker(statusInterval)
	defer tick.Stop()
	for {
		select {
		case <-m.stopped:
			return
		case now := <-tick.C:
			// 90 s: an idle-priority process can starve for a while on a busy PC
			// without any standby; a needless reconnect would cost share answers.
			gap := now.Sub(lastT)
			if wall := now.Round(0).Sub(lastWall); wall > gap {
				gap = wall
			}
			lastWall = now.Round(0)
			if gap > 90*time.Second {
				// The PC slept (the monotonic clock counts sleep on Windows and
				// Linux, on macOS only the wall clock does): fresh connection,
				// fresh hashrate average.
				m.out.logf("resumed after %v: reconnecting to the pool", gap.Round(time.Second))
				m.client.Reconnect()
				if m.share != nil {
					m.share.Reconnect()
				}
				samples = samples[:0]
				last, lastT = m.eng.hashes.Load(), now
				for i := range threadLast {
					threadLast[i] = m.eng.perThread[i].Load()
				}
				continue
			}
			h := m.eng.hashes.Load()
			m.sampleThreads(now.Sub(lastT).Seconds(), threadLast)
			samples = append(samples, float64(h-last)/now.Sub(lastT).Seconds())
			if len(samples) > hashrateWindow {
				samples = samples[1:]
			}
			last, lastT = h, now
			var sum float64
			for _, s := range samples {
				sum += s
			}
			rate := sum / float64(len(samples))
			m.rate.Store(floatBits(rate))
			if now.Sub(lastReport) >= reportInterval {
				m.client.ReportHashrate(uint64(rate), clientID)
				lastReport = now
			}
			// The window's live log shows the miner at work: accepted shares,
			// summed per tick, so even a pool with a very easy target cannot
			// flood miner.log (at most one line per statusInterval).
			if acc := m.accepted.Load(); acc > loggedAccepted {
				ms := time.Duration(m.answer.Load()).Milliseconds()
				if n := acc - loggedAccepted; n == 1 {
					m.out.logf("share accepted in %d ms (%d total)", ms, acc)
				} else {
					m.out.logf("%d shares accepted, last in %d ms (%d total)", n, ms, acc)
				}
				loggedAccepted = acc
			}
			if rej := m.rejected.Load(); rej > loggedRejected {
				why := ""
				if p := m.lastReject.Load(); p != nil {
					why = *p
				}
				if n := rej - loggedRejected; n == 1 {
					m.out.logf("share rejected: %s", why)
				} else {
					m.out.logf("%d shares rejected, last: %s", n, why)
				}
				loggedRejected = rej
			}
			if now.Sub(lastSummary) >= time.Minute {
				m.out.logf("%.0f H/s, %d thread(s), %d accepted, %d rejected",
					rate, m.eng.active.Load(), m.accepted.Load(), m.rejected.Load())
				lastSummary = now
			}
			// One line per offline spell, so miner.log says why nothing happens.
			if since := m.offlineSince.Load(); since == 0 {
				warnedOffline = false
			} else if off := time.Duration(m.mono() - since); !warnedOffline && !m.battery.Load() && !m.hot.Load() &&
				off >= minercore.UnreachableAfter(!m.hadJob.Load()) {
				m.out.logf("no work from the pool for %d min: an antivirus, firewall or network filter may block catchthatrabbit.com", int(off/time.Minute))
				warnedOffline = true
			}
			m.status("", "")
		}
	}
}

func (m *miner) batteryWatch() {
	var hotSince time.Time
	for {
		b := onBattery()
		if b != m.battery.Load() {
			m.battery.Store(b)
			if b {
				m.out.logf("running on battery: mining paused")
			} else if !m.hot.Load() { // else "cooled down" says when it resumes
				m.out.logf("power connected: mining resumes")
			}
		}
		switch hot := tooHot(); {
		case hot && !m.hot.Load():
			m.hot.Store(true)
			hotSince = time.Now()
			m.out.logf("too hot: mining paused")
		case !hot && m.hot.Load() && time.Since(hotSince) >= coolDown && !m.battery.Load():
			m.hot.Store(false)
			m.out.logf("cooled down: mining resumes")
		}
		m.eng.paused.Store(m.battery.Load() || m.hot.Load())
		select {
		case <-m.stopped:
			return
		case <-time.After(batteryInterval):
		}
	}
}

func (m *miner) earningsLoop() {
	wait := earningsFirst
	for {
		select {
		case <-m.stopped:
			return
		case <-time.After(wait):
		}
		host := ""
		if cur := m.client.CurrentEndpoint(); cur != nil {
			host = cur.Host
		}
		e, err := minercore.FetchEarnings(host, m.address, nil)
		if err != nil {
			m.out.logf("pool API: %v", err)
			wait = earningsInterval
			continue
		}
		m.earnings.Store(&e)
		wait = earningsInterval
	}
}

// status emits the current state; state "" derives it.
func (m *miner) status(state, errMsg string) {
	s := &minercore.MinerStatus{State: state, Error: errMsg, MaxThreads: m.maxThreads}
	if m.eng != nil {
		s.Threads = int(m.eng.active.Load())
		s.JIT = m.eng.ctx.Flags()&flagJIT != 0 && m.eng.jitOff.Load() == 0
		s.FullMem = m.eng.ctx.Flags()&flagFullMem != 0
		s.TotalHashes = m.eng.hashes.Load()
		// Only once every thread is past its self-test: before that, live == 0
		// is just "starting", not a failure.
		if m.eng.live.Load() == 0 && int(m.eng.setup.Load()) >= m.eng.threads && state == "" {
			state, s.Error = "error", "no hashing thread is running (memory?)"
		}
	}
	if state == "" {
		switch {
		case m.battery.Load():
			state = "paused-battery"
		case m.hot.Load():
			state = "paused-heat"
		case m.hasJob.Load() && m.client.CurrentEndpoint() != nil:
			state = "mining"
		default:
			state = "connecting"
			if since := m.offlineSince.Load(); since > 0 {
				s.OfflineSecs = (m.mono() - since) / int64(time.Second)
			}
			s.PoolNever = !m.hadJob.Load()
		}
	}
	s.State = state
	s.Hashrate = floatFrom(m.rate.Load())
	s.Accepted, s.Rejected = m.accepted.Load(), m.rejected.Load()
	if m.client != nil {
		if cur := m.client.CurrentEndpoint(); cur != nil {
			s.Pool = cur.Region()
		}
	}
	s.ShareDiff = floatFrom(m.shareDiff.Load())
	s.AnswerMs = time.Duration(m.answer.Load()).Milliseconds()
	m.threadMu.Lock()
	s.ThreadRates = append([]float64(nil), m.threadRates...)
	m.threadMu.Unlock()
	if m.share != nil {
		s.SupportActive = m.suppOn.Load()
		s.SupportHashes = m.share.Hashes()
	}
	if e := m.earnings.Load(); e != nil {
		s.Earnings = e
		// After the pool fee and the support share, as the window says.
		s.XCBPerDay = minercore.XCBPerDay(s.Hashrate, e.Network, e.Settings.FeePercent) * (1 - minercore.SupportFraction)
	}
	m.out.line(minercore.MinerLine{Type: "status", Status: s})
}

// sampleThreads updates the per-thread rates from the hashes since the last
// tick. Smoothed like the total (about a minute), so the grid does not flicker;
// an inactive thread (power below 100 %) shows 0 right away.
func (m *miner) sampleThreads(secs float64, last []int64) {
	if secs <= 0 {
		return
	}
	active := int(m.eng.active.Load())
	m.threadMu.Lock()
	defer m.threadMu.Unlock()
	if len(m.threadRates) != len(last) {
		m.threadRates = make([]float64, len(last))
	}
	for i := range last {
		h := m.eng.perThread[i].Load()
		now := float64(h-last[i]) / secs
		last[i] = h
		switch {
		case i >= active || m.eng.paused.Load():
			m.threadRates[i] = 0
		case m.threadRates[i] == 0:
			m.threadRates[i] = now
		default:
			m.threadRates[i] += (now - m.threadRates[i]) / hashrateWindow
		}
	}
}

// mono is the monotonic time since start in ns (never 0 after start; immune to clock changes).
func (m *miner) mono() int64 { return int64(time.Since(m.t0)) + 1 }

func floatBits(f float64) uint64 { return math.Float64bits(f) }
func floatFrom(b uint64) float64 { return math.Float64frombits(b) }
