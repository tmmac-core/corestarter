//go:build !nominer

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

const minerAvailable = true

// extraMinerArgs is only set by the integration tests (local fake pool).
var extraMinerArgs []string

const (
	minerStopWait    = 10 * time.Second
	minerThrottlePct = 25 // while the node does its first sync
	maxMinerLog      = 5 << 20
)

// minerConfig is what the miner needs from the settings; nil = mining off.
type minerConfig struct {
	Address, Worker string
	Power           int
}

func minerConfigFrom(s Settings) *minerConfig {
	if !s.Mine || s.Address == "" || !s.mineConsented() {
		return nil
	}
	return &minerConfig{Address: s.Address, Worker: s.Worker, Power: s.Power}
}

// minerSup keeps one core-miner running while mining is switched on. The
// miner dies with the tray process (Windows: job object, Linux: parent-death
// signal and stdin EOF); it has no state worth keeping, unlike gocore's
// database, so it is never adopted and a new tray simply starts a fresh one.
// Without that guard there is no mining: a miner that outlives the tray would
// run unseen.
type minerSup struct {
	p    paths
	job  minerGuard
	fast atomic.Bool // session ending: kill instead of asking the miner to stop

	mu        sync.Mutex
	closed    bool
	cfg       *minerConfig
	throttled bool
	run       *minerRun
	last      *minercore.MinerStatus
	failure   string         // shown until the settings change or the miner runs again
	halting   sync.WaitGroup // Apply calls still stopping an old run
}

type minerRun struct {
	cfg      minerConfig
	cmds     chan []byte // latest power command; written by one goroutine, never under mu
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

func newMinerSup(p paths) *minerSup {
	// A tray killed between starting a miner and guarding it leaves it running;
	// it is never adopted, so end it.
	killProcessesOf(p.miner, minerExeName)
	m := &minerSup{p: p, throttled: true} // throttled until the node's sync state is known
	job, err := newMinerGuard()
	if err != nil {
		log.Printf("miner guard: %v", err)
	}
	m.job = job
	return m
}

// Apply switches mining on/off or restarts it with new settings. A pure power
// change is sent to the running miner without a restart. It returns once an
// old miner has stopped.
func (m *minerSup) Apply(cfg *minerConfig) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	old := m.run
	m.cfg = cfg
	if old != nil && cfg != nil && m.failure == "" && !old.finished() && sameIdentity(old.cfg, *cfg) {
		old.cfg.Power = cfg.Power
		m.sendPowerLocked()
		m.mu.Unlock()
		return
	}
	m.failure, m.last, m.run = "", nil, nil
	if cfg != nil {
		if !m.job.ok() {
			m.failure = "job"
			log.Printf("mining stays off: no miner guard")
		} else {
			r := &minerRun{cfg: *cfg, cmds: make(chan []byte, 1), stop: make(chan struct{}), done: make(chan struct{})}
			m.run = r
			go m.loop(r, old) // waits for old to stop first
		}
	}
	if old != nil {
		m.halting.Add(1)
	}
	m.mu.Unlock()
	if old != nil {
		old.halt()
		m.halting.Done()
	}
}

func sameIdentity(a, b minerConfig) bool {
	return a.Address == b.Address && a.Worker == b.Worker
}

// SetThrottle caps the miner at 25 % while the node does its first sync.
func (m *minerSup) SetThrottle(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.throttled == on {
		return
	}
	m.throttled = on
	m.sendPowerLocked()
}

func (m *minerSup) effectivePowerLocked(cfgPower int) int {
	if m.throttled && cfgPower > minerThrottlePct {
		return minerThrottlePct
	}
	return cfgPower
}

// sendPowerLocked queues the current power for the run's writer; an older
// queued value is replaced, so this never blocks.
func (m *minerSup) sendPowerLocked() {
	r := m.run
	if r == nil {
		return
	}
	b, _ := json.Marshal(minercore.Command{Cmd: "power", Percent: m.effectivePowerLocked(r.cfg.Power)})
	b = append(b, '\n')
	for {
		select {
		case r.cmds <- b:
			return
		default:
		}
		select {
		case <-r.cmds:
		default:
		}
	}
}

// Shutdown stops the miner for good (tray quitting, uninstall).
func (m *minerSup) Shutdown() {
	m.mu.Lock()
	m.closed = true
	r := m.run
	m.run, m.cfg = nil, nil
	m.mu.Unlock()
	if r != nil {
		r.halt()
	}
	m.halting.Wait()
}

// ShutdownNow is Shutdown at the end of the session: the miner is
// killed right away, so the node gets the whole time Windows grants.
func (m *minerSup) ShutdownNow() {
	m.fast.Store(true)
	m.Shutdown()
}

// View is what the status file and menu show.
func (m *minerSup) View() (on bool, throttled bool, st *minercore.MinerStatus, failure string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg != nil, m.throttled, m.last, m.failure
}

// halt asks the run to stop and waits for its loop to end.
func (r *minerRun) halt() {
	r.stopOnce.Do(func() { close(r.stop) })
	<-r.done
}

func (r *minerRun) finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (m *minerSup) loop(r *minerRun, prev *minerRun) {
	defer close(r.done)
	if prev != nil {
		prev.halt()
	}
	var crashes []time.Time
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		code, err := m.runOnce(r)
		select {
		case <-r.stop:
			return
		default:
		}
		// Failure codes are translated for menu and window (texts.minerFailed).
		reason := ""
		switch {
		case errors.Is(err, errNoJob):
			reason = "job"
		case errors.Is(err, errBlocked):
			reason = "av"
		case err != nil:
			// Could not start or watch the process: a crash, retried below.
		case code == minercore.ExitSelfTest:
			reason = "selftest"
		case code == minercore.ExitLoginRejected:
			reason = "login"
		case code == minercore.ExitBadArgs:
			reason = "address"
		}
		if reason != "" {
			// Retrying changes nothing; wait for new settings.
			log.Printf("mining stopped: %s (code %d, %v)", reason, code, err)
			m.setFailure(r, reason)
			return
		}
		log.Printf("core-miner exited (code %d, %v)", code, err)
		crashes = append(recent(crashes, crashWindow), time.Now())
		delay := restartDelay
		if len(crashes) >= maxCrashes {
			m.setFailure(r, "crashloop")
			delay, crashes = failedRetry, nil
		}
		select {
		case <-r.stop:
			return
		case <-time.After(delay):
		}
		m.setFailure(r, "")
	}
}

func (m *minerSup) setFailure(r *minerRun, reason string) {
	m.mu.Lock()
	if m.run == r {
		m.failure = reason
		if reason != "" {
			m.last = nil
		}
	}
	m.mu.Unlock()
}

var (
	errNoJob = errors.New("could not attach the miner to CoreStarter (job object)")
	// errBlocked: the antivirus removed, changed or refused core-miner.exe.
	// Retrying would only raise the same alarm again every few seconds.
	errBlocked = errors.New("core-miner.exe was removed or blocked (antivirus?)")
)

// runOnce starts core-miner.exe and blocks until it exits or r.stop closes.
func (m *minerSup) runOnce(r *minerRun) (int, error) {
	m.mu.Lock()
	power := m.effectivePowerLocked(r.cfg.Power)
	m.mu.Unlock()
	args := append([]string{
		"--address", r.cfg.Address, "--worker", r.cfg.Worker,
		"--power", strconv.Itoa(power),
	}, extraMinerArgs...)
	if err := os.MkdirAll(m.p.logs, 0o700); err != nil {
		return -1, err
	}
	capLog(m.p.minerLog, maxMinerLog)
	logf, err := os.OpenFile(m.p.minerLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return -1, err
	}
	defer logf.Close()
	if !minerIntact(m.p) {
		return -1, errBlocked
	}
	cmd := exec.Command(m.p.miner, args...)
	cmd.Dir = m.p.bin
	cmd.Stderr = logf
	cmd.SysProcAttr = minerProcAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return -1, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	// The miner's self-test takes seconds before it computes anything, so it
	// is guarded long before it does real work.
	if err := startGuarded(m.job, cmd); err != nil {
		return -1, err
	}
	log.Printf("started core-miner pid %d (power %d %%)", cmd.Process.Pid, power)
	m.mu.Lock()
	if m.run == r {
		m.sendPowerLocked() // throttle may have changed while starting
	}
	m.mu.Unlock()

	// One writer per process, so a slow pipe never blocks a caller holding mu.
	writerDone := make(chan struct{})
	writerStop := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case b := <-r.cmds:
				if _, err := stdin.Write(b); err != nil {
					return
				}
			case <-writerStop:
				return
			}
		}
	}()

	lines := make(chan struct{})
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var l minercore.MinerLine
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				continue
			}
			switch {
			case l.Status != nil:
				m.mu.Lock()
				if m.run == r {
					m.last = l.Status
					if l.Status.State != "error" {
						m.failure = "" // runs again after a crash pause
					}
				}
				m.mu.Unlock()
			case l.Msg != "":
				fmt.Fprintf(logf, "%s %s\n", time.Now().Format("2006/01/02 15:04:05"), l.Msg)
			}
		}
		io.Copy(io.Discard, stdout)
	}()
	exited := make(chan error, 1)
	go func() {
		<-lines // stdout closes when the process ends; Wait must come after reading
		exited <- cmd.Wait()
	}()
	finish := func() {
		close(writerStop)
		<-writerDone
		stdin.Close()
	}

	select {
	case err := <-exited:
		finish()
		return exitCode(err)
	case <-r.stop:
		// The kill timer runs before anything that could block on a full pipe
		// of a hung miner (the writer, the stop command).
		wait := minerStopWait
		if m.fast.Load() {
			wait = 0
		}
		kill := time.AfterFunc(wait, func() { cmd.Process.Kill() })
		defer kill.Stop()
		close(writerStop)
		<-writerDone
		if wait > 0 {
			b, _ := json.Marshal(minercore.Command{Cmd: "stop"})
			stdin.Write(append(b, '\n'))
		}
		stdin.Close()
		return exitCode(<-exited)
	}
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}
