package minercore

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

// SupportAddress is the TMMAC address that receives the fixed support share.
const SupportAddress = "cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963"

// SupportWorker names the support connection in the pool statistics of SupportAddress.
const SupportWorker = "app-support"

const (
	SupportWindow = 100 * time.Minute
	SupportSlice  = 25 * time.Second // 25 s of every 100 min = 0.42 %
)

// SupportFraction is the share of mining time that goes to TMMAC (0.42 %).
const SupportFraction = float64(SupportSlice) / float64(SupportWindow)

// SupportShare (decided 16.10.2026): 0.42 % of mining time goes to the
// TMMAC support address and funds further development; fixed since 19.10.2026
// (no switch, as in the mobile miner). In every 100-minute window the miner works 25 s for TMMAC at a random
// point: it opens a short second pool connection with the TMMAC address, hands
// its jobs to the hashing threads and afterwards the user's jobs again. A found
// share goes to the connection its job came from (SubmitIfSupport).
//
// Why a random point instead of a fixed minute: whoever mines for less than
// 100 min still contributes 0.42 % on average, not 0 % or far more. Money never
// passes through the app; the pool pays both addresses separately. If no job
// arrives for TMMAC (pool unreachable, login rejected), the slice is skipped, at
// TMMAC's expense, never the user's.
type SupportShare struct {
	Connect  func(Events) *Client // builds the support connection; called per slice
	SetJob   func(*Job)           // job for the hashing threads; nil = nothing to do
	OnSlice  func(active bool)
	OnResult func(accepted bool, err string)
	Log      func(msg string)
	Window   time.Duration
	Slice    time.Duration
	JobWait  time.Duration // a slice is skipped if the first job takes longer
	Drain    time.Duration // stays open after the slice for answers to just-sent shares
	RandFrac func() float64

	running    atomic.Bool
	stopCh     chan struct{}
	done       chan struct{}
	doneHashes atomic.Int64
	doneNanos  atomic.Int64

	mu         sync.Mutex // guards the fields below
	userJob    *Job
	supportJob *Job
	sliceOpen  bool
	client     *Client
	jobs       map[*Job]bool // jobs of the support connection (identity, not content)
}

// Hashes computed for TMMAC so far (completed slices).
func (s *SupportShare) Hashes() int64 { return s.doneHashes.Load() }

// Duration of the support slices so far.
func (s *SupportShare) Duration() time.Duration { return time.Duration(s.doneNanos.Load()) }

func (s *SupportShare) Start() {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	if s.Window == 0 {
		s.Window = SupportWindow
	}
	if s.Slice == 0 {
		s.Slice = SupportSlice
	}
	if s.JobWait == 0 {
		s.JobWait = 15 * time.Second
	}
	if s.Drain == 0 {
		s.Drain = 3 * time.Second
	}
	if s.RandFrac == nil {
		s.RandFrac = randFrac
	}
	s.mu.Lock()
	s.jobs = map[*Job]bool{}
	s.mu.Unlock()
	s.stopCh = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop()
}

// Stop ends a running slice cleanly (its hashes are counted) and waits for it.
func (s *SupportShare) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return
	}
	close(s.stopCh)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	s.mu.Lock()
	c := s.client
	s.client, s.sliceOpen, s.supportJob = nil, false, nil
	s.jobs = map[*Job]bool{}
	s.mu.Unlock()
	if c != nil {
		c.Stop()
	}
}

// Reconnect drops a running support connection (after standby).
func (s *SupportShare) Reconnect() {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		c.Reconnect()
	}
}

// OnUserJob takes a new job from the user's connection; during a support slice it is only remembered.
func (s *SupportShare) OnUserJob(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userJob = j
	if s.supportJob == nil {
		s.SetJob(j)
	}
}

// OnUserDisconnected: the user's job is no longer valid. A running support slice continues.
func (s *SupportShare) OnUserDisconnected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userJob = nil
	if s.supportJob == nil {
		s.SetJob(nil)
	}
}

// SubmitIfSupport sends the share to the support connection if its job came
// from there (true). Otherwise false: the caller passes it to the user's connection.
func (s *SupportShare) SubmitIfSupport(j *Job, nonce uint64, hash [32]byte) bool {
	if !j.FromSupport {
		return false
	}
	s.mu.Lock()
	var c *Client
	if s.jobs[j] {
		c = s.client
	}
	s.mu.Unlock()
	// Slice over and connection closed: the share lapses (a late batch at the
	// end of a slice); it is never booked to the user.
	if c != nil {
		c.Submit(j, nonce, hash)
	}
	return true
}

func (s *SupportShare) sleep(d time.Duration) bool {
	select {
	case <-s.stopCh:
		return false
	case <-time.After(d):
		return true
	}
}

func (s *SupportShare) loop() {
	defer close(s.done)
	for s.running.Load() {
		// The start is uniform over the whole window and a slice that would not
		// fit is skipped: a run of any length then gives TMMAC at most
		// Slice/Window of its time on average, never more.
		start := time.Now()
		offset := time.Duration(s.RandFrac() * float64(s.Window))
		if offset+s.Slice <= s.Window {
			if !s.sleep(offset) {
				return
			}
			s.runSlice()
		}
		if rest := s.Window - time.Since(start); rest > 0 && !s.sleep(rest) {
			return
		}
	}
}

func (s *SupportShare) runSlice() {
	firstJob := make(chan struct{})
	var once sync.Once
	client := s.Connect(Events{
		Job: func(j *Job) {
			s.mu.Lock()
			if !s.sliceOpen {
				s.mu.Unlock()
				return
			}
			j.FromSupport = true // before any hashing thread sees it
			s.jobs[j] = true
			s.supportJob = j
			s.SetJob(j)
			s.mu.Unlock()
			once.Do(func() { close(firstJob) })
		},
		Disconnected: func(string) {
			// Back to the user; if the connection returns within the slice, it continues there.
			s.mu.Lock()
			if s.supportJob != nil {
				s.supportJob = nil
				s.SetJob(s.userJob)
			}
			s.mu.Unlock()
		},
		LoginRejected: func(reason string) { s.log("support login rejected: " + reason) },
		ShareResult: func(ok bool, err string) {
			if s.OnResult != nil {
				s.OnResult(ok, err)
			}
		},
	})
	s.mu.Lock()
	if !s.running.Load() {
		s.mu.Unlock()
		return
	}
	s.sliceOpen, s.client = true, client
	s.mu.Unlock()

	active := false
	var activeSince time.Time
	client.Start()
	select {
	case <-firstJob:
		active, activeSince = true, time.Now()
		if s.OnSlice != nil {
			s.OnSlice(true)
		}
		s.log("support share: 25 s for TMMAC")
		s.sleep(s.Slice)
	case <-time.After(s.JobWait):
		s.log("support share skipped: no job from the pool")
	case <-s.stopCh:
	}

	s.mu.Lock()
	s.sliceOpen = false
	if s.supportJob != nil {
		s.supportJob = nil
		s.SetJob(s.userJob)
	}
	s.mu.Unlock()
	if active {
		s.doneNanos.Add(int64(time.Since(activeSince)))
		if s.OnSlice != nil {
			s.OnSlice(false)
		}
	}
	// Wait for answers to shares just sent; count only afterwards, so the last
	// batches of the hashing threads are included.
	s.sleep(s.Drain)
	s.mu.Lock()
	var n int64
	for j := range s.jobs {
		n += j.Hashes.Load()
	}
	s.jobs = map[*Job]bool{}
	if s.client == client {
		s.client = nil
	}
	s.mu.Unlock()
	s.doneHashes.Add(n)
	client.Stop()
	if active {
		s.log("support share done")
	}
}

func (s *SupportShare) log(msg string) {
	if s.Log != nil {
		s.Log(msg)
	}
}

func randFrac() float64 {
	var b [8]byte
	rand.Read(b[:])
	return float64(binary.LittleEndian.Uint64(b[:])>>11) / (1 << 53)
}
