package minercore

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Endpoint is one pool server.
type Endpoint struct {
	Host string
	Port int
}

func (e Endpoint) String() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }

// Region is the first label of the host ("de" for de.catchthatrabbit.com).
func (e Endpoint) Region() string { return strings.SplitN(e.Host, ".", 2)[0] }

// CTRPools are the Catch That Rabbit servers (coreminer README, port 8008, ETHPROXY).
var CTRPools = []Endpoint{
	{"de.catchthatrabbit.com", 8008},
	{"fi.catchthatrabbit.com", 8008},
	{"sg.catchthatrabbit.com", 8008},
	{"hk.catchthatrabbit.com", 8008},
}

// Job is one work package: header hash and share target (32 bytes each). The
// seed the pool also sends is not needed: RandomY uses the fixed cache key.
type Job struct {
	Header   [32]byte
	Boundary [32]byte
	Hashes   atomic.Int64 // hashes computed on this job (evidence for the support share)
	// FromSupport marks a job of the support connection. It is set before the
	// job reaches the hashing threads and never changes, so a share found on
	// it can never be sent with the user's address, however late it is found.
	FromSupport bool
}

// HeaderHex is the header as lower-case hex without 0x.
func (j *Job) HeaderHex() string { return hex.EncodeToString(j.Header[:]) }

// Difficulty is roughly 2^256 / boundary, for display and logs only.
func (j *Job) Difficulty() float64 {
	b, _ := new(big.Float).SetInt(new(big.Int).SetBytes(j.Boundary[:])).Float64()
	if b == 0 {
		return 0
	}
	return math.Pow(2, 256) / b
}

// Events are called from the client's goroutines; nil callbacks are skipped.
type Events struct {
	Connected     func(Endpoint)
	Disconnected  func(reason string)
	LoginRejected func(reason string) // the client has stopped itself
	Job           func(*Job)
	ShareResult   func(accepted bool, err string)
	Log           func(msg string)
}

// Client speaks ETHPROXY like coreminer with Catch That Rabbit
// (stratum1+tcp://WALLET.WORKER@host:8008, XcbStratumClient.cpp):
//
//	-> eth_submitLogin [wallet] + "worker"   <- result true
//	-> eth_getWork []                         <- result [header, seed, target, ...]
//	<- the pool pushes new jobs as {"id":0,"result":[...]}
//	-> eth_submitWork [nonce, header, hash]   <- result true/false
//
// With RankByLatency the fastest server becomes "home" for the whole run: every
// CTR region is a separate pool with its own balance and only pays out from
// 0.1 XCB, so each switch would leave a remainder behind. After a disconnect the
// same server is tried again, after two failures in a row the next one; while
// on a fallback server the client checks every RecheckInterval whether home is
// back. A login the pool rejects ends the client: retrying changes nothing.
type Client struct {
	Endpoints       []Endpoint
	Wallet, Worker  string
	Events          Events
	ReadTimeout     time.Duration // the pool pushes jobs every few seconds; silence = dead connection
	InitialBackoff  time.Duration
	ConnectTimeout  time.Duration
	RankByLatency   bool
	RecheckInterval time.Duration
	Rank            func([]Endpoint) []Measurement // default RankEndpoints

	running    atomic.Bool
	stopCh     chan struct{}
	sendCh     chan []byte
	nextID     atomic.Int64
	lastAnswer atomic.Int64 // ns, see LastShareAnswer

	mu         sync.Mutex // guards the fields below
	conn       net.Conn
	loggedIn   bool
	loginAt    time.Time
	current    *Endpoint
	home       *Endpoint
	pendingOrd []Endpoint
	pending    map[int64]time.Time  // share id -> when it was queued
	lastLog    map[string]time.Time // rate limit for messages a broken pool can trigger per hash or line
}

// maxPending caps shares awaiting an answer: a pool that sends jobs but never
// answers must not grow memory without bound.
const maxPending = 1024

// healthySession: a session counts as good if the login held this long, so a
// pool that drops us right after login is not hammered every 2 s.
const healthySession = time.Minute

func (c *Client) defaults() {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = time.Minute
	}
	if c.InitialBackoff == 0 {
		c.InitialBackoff = 2 * time.Second
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.RecheckInterval == 0 {
		c.RecheckInterval = 15 * time.Minute
	}
	if c.Rank == nil {
		c.Rank = func(e []Endpoint) []Measurement { return RankEndpoints(e, 3*time.Second, nil) }
	}
}

// Start connects in the background.
func (c *Client) Start() {
	if !c.running.CompareAndSwap(false, true) {
		return
	}
	c.defaults()
	c.stopCh = make(chan struct{})
	c.sendCh = make(chan []byte, 64)
	c.pending = map[int64]time.Time{}
	c.nextID.Store(100)
	go c.runLoop()
	go c.sender()
	if c.RankByLatency && len(c.Endpoints) > 1 {
		go c.recheckLoop()
	}
}

// Stop ends the client; it can not be started again.
func (c *Client) Stop() {
	if !c.running.CompareAndSwap(true, false) {
		return
	}
	close(c.stopCh)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

// Running reports whether the client is still active (false after Stop or a rejected login).
func (c *Client) Running() bool { return c.running.Load() }

// Reconnect drops the current session; the run loop connects again after its
// usual short backoff.
// After standby or a network change the old TCP connection is dead without
// anyone noticing (no RST) until the read timeout, while the threads keep
// hashing a stale job.
func (c *Client) Reconnect() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// CurrentEndpoint is the server of the running session, nil while disconnected.
func (c *Client) CurrentEndpoint() *Endpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil || !c.loggedIn {
		return nil
	}
	e := *c.current
	return &e
}

// Submit queues a found share; it never blocks a hashing thread.
func (c *Client) Submit(job *Job, nonce uint64, hash [32]byte) {
	if !c.running.Load() {
		return
	}
	id := c.nextID.Add(1)
	msg := map[string]any{
		"id": id, "jsonrpc": "2.0", "method": "eth_submitWork", "worker": c.Worker,
		"params": []string{fmt.Sprintf("0x%016x", nonce), "0x" + job.HeaderHex(), "0x" + hex.EncodeToString(hash[:])},
	}
	b, _ := json.Marshal(msg)
	c.mu.Lock()
	ok := c.loggedIn
	full := len(c.pending) >= maxPending
	if ok && !full {
		c.pending[id] = time.Now()
	}
	c.mu.Unlock()
	switch {
	case !ok:
		c.logLimited("noconn", "share dropped: no pool connection (nonce %d)", nonce)
		return
	case full:
		c.logLimited("pending", "share dropped: the pool has not answered %d shares", maxPending)
		return
	}
	select {
	case c.sendCh <- b:
	default:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		c.logLimited("queue", "share dropped: send queue full")
	}
}

// ReportHashrate shows our hashrate in the pool dashboard, format as coreminer.
func (c *Client) ReportHashrate(hps uint64, clientID string) {
	c.mu.Lock()
	ok := c.loggedIn
	c.mu.Unlock()
	if !ok {
		return
	}
	b, _ := json.Marshal(map[string]any{
		"id": 9, "jsonrpc": "2.0", "method": "eth_submitHashrate", "worker": c.Worker,
		"params": []string{fmt.Sprintf("0x%064x", hps), clientID},
	})
	select {
	case c.sendCh <- b:
	default:
	}
}

func (c *Client) sender() {
	for {
		select {
		case <-c.stopCh:
			return
		case b := <-c.sendCh:
			c.write(b)
		}
	}
}

func (c *Client) write(b []byte) bool {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return false
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(append(b, '\n')); err != nil {
		conn.Close()
		return false
	}
	return true
}

func (c *Client) log(format string, args ...any) {
	if c.Events.Log != nil {
		c.Events.Log(fmt.Sprintf(format, args...))
	}
}

// logLimited logs a message of kind key at most once a minute.
func (c *Client) logLimited(key, format string, args ...any) {
	c.mu.Lock()
	if c.lastLog == nil {
		c.lastLog = map[string]time.Time{}
	}
	if time.Since(c.lastLog[key]) < time.Minute {
		c.mu.Unlock()
		return
	}
	c.lastLog[key] = time.Now()
	c.mu.Unlock()
	c.log(format, args...)
}

func (c *Client) sleep(d time.Duration) bool {
	select {
	case <-c.stopCh:
		return false
	case <-time.After(d):
		return true
	}
}

func (c *Client) runLoop() {
	index, failuresHere := 0, 0
	backoff := c.InitialBackoff
	order := c.Endpoints
	for c.running.Load() {
		c.mu.Lock()
		switchTo := c.pendingOrd
		c.pendingOrd = nil
		c.mu.Unlock()
		if switchTo != nil {
			order, index, failuresHere = switchTo, 0, 0
		} else if c.RankByLatency && index%len(c.Endpoints) == 0 && failuresHere == 0 {
			// Measure at the start and after every full round (network changed, server back).
			ranked := c.Rank(c.Endpoints)
			if !c.running.Load() {
				return
			}
			c.mu.Lock()
			// Home is the fastest server that answered. If none did (no network
			// yet, e.g. autostart before Wi-Fi), there is no home and the next
			// round measures again.
			first := c.home == nil
			if first {
				for _, m := range ranked {
					if m.Millis >= 0 {
						h := m.Endpoint
						c.home = &h
						break
					}
				}
			}
			haveHome := c.home != nil
			var home Endpoint
			if haveHome {
				home = *c.home
			}
			c.mu.Unlock()
			if haveHome {
				order = homeFirst(ranked, home)
			} else {
				order = make([]Endpoint, 0, len(ranked)) // never reuse c.Endpoints' array
				for _, m := range ranked {
					order = append(order, m.Endpoint)
				}
				first = false
			}
			if first && haveHome {
				c.log("servers: %s; home: %s", describeRanking(ranked), home.Region())
			} else {
				c.log("servers: %s", describeRanking(ranked))
			}
		}
		ep := order[index%len(order)]
		err := c.session(ep)
		c.mu.Lock()
		healthy := !c.loginAt.IsZero() && time.Since(c.loginAt) >= healthySession
		lost := c.endSessionLocked()
		c.mu.Unlock()
		if lost > 0 {
			c.log("%d share answer(s) lost with the connection", lost)
		}
		var rejected *loginRejected
		if errors.As(err, &rejected) {
			if c.running.CompareAndSwap(true, false) {
				close(c.stopCh)
			}
			if c.Events.LoginRejected != nil {
				c.Events.LoginRejected(rejected.reason)
			}
			return
		}
		if !c.running.Load() {
			return
		}
		if c.Events.Disconnected != nil {
			c.Events.Disconnected(fmt.Sprintf("%s: %v", ep, err))
		}
		if healthy {
			backoff, failuresHere = c.InitialBackoff, 0
		} else if failuresHere++; failuresHere >= 2 {
			index++
			failuresHere = 0
		}
		if !c.sleep(backoff) {
			return
		}
		if !healthy {
			backoff = min(backoff*2, time.Minute)
		}
	}
}

func homeFirst(ranked []Measurement, home Endpoint) []Endpoint {
	all := make([]Endpoint, 0, len(ranked))
	homeUp := false
	for _, m := range ranked {
		if m.Endpoint == home && m.Millis >= 0 {
			homeUp = true
		}
	}
	if homeUp {
		all = append(all, home)
	}
	for _, m := range ranked {
		if !homeUp || m.Endpoint != home {
			all = append(all, m.Endpoint)
		}
	}
	return all
}

func describeRanking(ranked []Measurement) string {
	parts := make([]string, len(ranked))
	for i, m := range ranked {
		if m.Millis >= 0 {
			parts[i] = fmt.Sprintf("%s %d ms", m.Endpoint.Region(), m.Millis)
		} else {
			parts[i] = m.Endpoint.Region() + " –"
		}
	}
	return strings.Join(parts, ", ")
}

// recheckLoop returns to home once it is reachable again.
func (c *Client) recheckLoop() {
	for c.sleep(c.RecheckInterval) {
		c.mu.Lock()
		cur, home, ok := c.current, c.home, c.loggedIn
		c.mu.Unlock()
		if !ok || cur == nil || home == nil || *cur == *home {
			continue
		}
		curEp := *cur
		ranked := c.Rank(c.Endpoints)
		var homeMs int64 = -1
		for _, m := range ranked {
			if m.Endpoint == *home {
				homeMs = m.Millis
			}
		}
		if homeMs < 0 {
			continue
		}
		// Do not cut into a share answer (wait at most 10 s).
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			c.mu.Lock()
			n := len(c.pending)
			c.mu.Unlock()
			if n == 0 || !c.sleep(200*time.Millisecond) {
				break
			}
		}
		c.mu.Lock()
		if !c.running.Load() || c.current == nil || *c.current != curEp {
			c.mu.Unlock()
			continue
		}
		c.pendingOrd = homeFirst(ranked, *home)
		conn := c.conn
		c.mu.Unlock()
		c.log("back to home server %s (%d ms) from %s", home.Region(), homeMs, curEp.Region())
		if conn != nil {
			conn.Close()
		}
	}
}

// endSessionLocked resets the session and returns how many share answers were still open.
func (c *Client) endSessionLocked() int {
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn, c.current, c.loggedIn, c.loginAt = nil, nil, false, time.Time{}
	n := len(c.pending)
	c.pending = map[int64]time.Time{}
	return n
}

type loginRejected struct{ reason string }

func (e *loginRejected) Error() string { return "login rejected: " + e.reason }

func (c *Client) session(ep Endpoint) error {
	conn, err := dialGuarded(ep.String(), c.ConnectTimeout)
	if err != nil {
		return err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
	}
	c.mu.Lock()
	if !c.running.Load() {
		c.mu.Unlock()
		conn.Close()
		return errors.New("stopped")
	}
	c.conn, c.current = conn, &ep
	c.mu.Unlock()

	login, _ := json.Marshal(map[string]any{
		"id": 1, "jsonrpc": "2.0", "method": "eth_submitLogin", "params": []string{c.Wallet}, "worker": c.Worker,
	})
	if !c.write(login) {
		return errors.New("could not send login")
	}
	r := bufio.NewReaderSize(conn, 64<<10)
	silent := 0
	for c.running.Load() {
		conn.SetReadDeadline(time.Now().Add(c.ReadTimeout))
		// ReadSlice keeps a line within the 64 KiB buffer: a pool (or something in
		// between) that never sends a newline cannot grow memory without bound.
		raw, err := r.ReadSlice('\n')
		line := string(raw)
		if errors.Is(err, bufio.ErrBufferFull) {
			return errors.New("pool sent a line longer than 64 KiB")
		}
		if err != nil {
			var ne net.Error
			c.mu.Lock()
			loggedIn := c.loggedIn
			c.mu.Unlock()
			if errors.As(err, &ne) && ne.Timeout() && line == "" && loggedIn {
				// Long silence can be a long block: ask once with getWork, then give up.
				if silent++; silent < 2 {
					c.write([]byte(`{"id":5,"jsonrpc":"2.0","method":"eth_getWork","params":[]}`))
					continue
				}
			}
			if errors.Is(err, net.ErrClosed) || !c.running.Load() {
				return errors.New("connection closed")
			}
			return err
		}
		silent = 0
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := c.handle(line, ep); err != nil {
			return err
		}
	}
	return errors.New("stopped")
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (c *Client) handle(line string, ep Endpoint) error {
	var m rpcMessage
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		c.logLimited("unreadable", "unreadable pool message: %.200s", line)
		return nil
	}
	id, hasID := parseID(m.ID)
	hasError := len(m.Error) > 0 && string(m.Error) != "null"
	var arr []json.RawMessage
	isArray := json.Unmarshal(m.Result, &arr) == nil && len(m.Result) > 0 && m.Result[0] == '['
	var ok bool
	isBool := json.Unmarshal(m.Result, &ok) == nil

	c.mu.Lock()
	loggedIn := c.loggedIn
	c.mu.Unlock()

	switch {
	case hasID && id == 1 && !loggedIn: // only the answer to our login; later id-1 messages are not one
		if isBool && ok {
			c.mu.Lock()
			c.loggedIn, c.loginAt = true, time.Now()
			c.mu.Unlock()
			if c.Events.Connected != nil {
				c.Events.Connected(ep)
			}
			c.write([]byte(`{"id":5,"jsonrpc":"2.0","method":"eth_getWork","params":[]}`))
			return nil
		}
		reason := describe(m.Error)
		if !hasError {
			reason = describe(m.Result)
		}
		return &loginRejected{reason}
	case isArray && len(arr) >= 3:
		job, err := parseJob(arr)
		if err != nil {
			c.logLimited("badjob", "unreadable job: %.200s", line)
			return nil
		}
		if c.Events.Job != nil {
			c.Events.Job(job)
		}
	case hasID && c.takePending(id):
		// The answer time is read in ShareResult (same goroutine, before the next line).
		if c.Events.ShareResult != nil {
			if isBool && ok {
				c.Events.ShareResult(true, "")
			} else if hasError {
				c.Events.ShareResult(false, describe(m.Error))
			} else {
				c.Events.ShareResult(false, describe(m.Result))
			}
		}
	case hasError:
		c.logLimited("poolerr", "pool error (id %.20s): %.200s", string(m.ID), describe(m.Error))
	}
	return nil
}

func (c *Client) takePending(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.pending[id]; ok {
		delete(c.pending, id)
		c.lastAnswer.Store(int64(time.Since(t)))
		return true
	}
	return false
}

// LastShareAnswer: how long the pool took to answer the last share (from
// queuing it to the answer), 0 before the first answer.
func (c *Client) LastShareAnswer() time.Duration { return time.Duration(c.lastAnswer.Load()) }

func parseID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0, false
		}
		n = json.Number(s)
	}
	v, err := n.Int64()
	return v, err == nil
}

func describe(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "unknown"
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return string(raw)
}

func parseJob(arr []json.RawMessage) (*Job, error) {
	var header, target string
	if err := json.Unmarshal(arr[0], &header); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(arr[2], &target); err != nil {
		return nil, err
	}
	if h := strings.TrimPrefix(strings.TrimPrefix(header, "0x"), "0X"); len(h) != 64 {
		return nil, fmt.Errorf("header is not 32 bytes: %q", header)
	}
	j := &Job{}
	var err error
	if j.Header, err = HexTo32(header); err != nil {
		return nil, err
	}
	if j.Boundary, err = HexTo32(target); err != nil {
		return nil, err
	}
	return j, nil
}

// HexTo32 parses hex (with or without 0x) into 32 bytes, left-padded with
// zeros like coreminer does for short targets.
func HexTo32(s string) ([32]byte, error) {
	var out [32]byte
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s) > 64 {
		return out, fmt.Errorf("not a 32-byte value: %q", s)
	}
	s = strings.Repeat("0", 64-len(s)) + s
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	copy(out[:], b)
	return out, nil
}
