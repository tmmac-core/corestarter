package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	nodeCacheMB      = 1024      // gocore would bump mainnet to 4096 MB; 1 GB keeps RAM near 2-3 GB
	maxNodeLogBytes  = 50 << 20  // rotate gocore.log at start once it passes 50 MB
	hardNodeLogBytes = 200 << 20 // truncate a running node's log beyond this (long uptimes)
	minFreeBytes     = 2 << 30   // below this the node is not (re)started
	stopFreeBytes    = 1 << 30   // below this a running node is stopped before the disk is full
	diskCheckEvery   = 30 * time.Second
	readyWait        = 10 * time.Minute // max wait for gocore to install its signal handler
	restartDelay     = 10 * time.Second // pause before restarting a crashed node
	crashWindow      = 10 * time.Minute
	maxCrashes       = 5                // crashes inside crashWindow before pausing
	failedRetry      = 30 * time.Minute // pause after maxCrashes before trying again
)

// hurryStop: the session ends and the service manager waits only so long
// before it kills the helper (launchd ExitTimeOut 120 s). gocore runs in its
// own process group and would get no signal at all, so the stop must not
// wait minutes for its signal handler.
var hurryStop atomic.Bool

// nodeProc is a running gocore, either started by us or adopted after our own restart.
type nodeProc struct {
	pid    uint32
	ipc    string
	handle nodeHandle // what the platform needs to wait for, signal and end the process
	exited chan struct{}
	// ready is closed once gocore serves its IPC endpoint. gocore installs its
	// interrupt handler right after that (cmd/utils/cmd.go StartNode); an
	// interrupt before it would kill gocore while it opens its database.
	ready chan struct{}
}

// extraNodeArgs is only set by the integration tests (offline node).
var extraNodeArgs []string

func nodeArgs(p paths, ipc string, port int) []string {
	args := []string{
		"--datadir", p.data,
		"--syncmode", "fast",
		"--cache", strconv.Itoa(nodeCacheMB),
		"--ipcpath", ipc,
	}
	if port != defaultP2PPort {
		args = append(args, "--port", strconv.Itoa(port))
	}
	return append(args, extraNodeArgs...)
}

const defaultP2PPort = 30300

const startMarker = "=== CoreStarter: starting gocore"

// portInUse reports whether gocore's last run ended because its P2P port was
// taken (another user's CoreStarter, a hand-started gocore). Read from the
// log instead of probing the port ourselves: listening on all interfaces would
// make Windows ask for a firewall exception for CoreStarter itself.
func portInUse(nodeLog string) bool {
	f, err := os.Open(nodeLog)
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 8192 {
		f.Seek(-8192, io.SeekEnd)
	}
	tail, _ := io.ReadAll(f)
	t := string(tail)
	if i := strings.LastIndex(t, startMarker); i >= 0 {
		t = t[i:] // only the last run, not a bind error from an earlier one
	}
	t = strings.ToLower(t)
	return strings.Contains(t, "only one usage of each socket address") ||
		(strings.Contains(t, "bind") && strings.Contains(t, "address already in use"))
}

// openNodeLog rotates gocore.log and opens it for a new run, marked so that
// portInUse only looks at the output of this run.
func openNodeLog(p paths, port int) (*os.File, error) {
	if err := os.MkdirAll(p.logs, 0o700); err != nil {
		return nil, err
	}
	rotateLog(p.nodeLog)
	logf, err := os.OpenFile(p.nodeLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(logf, "%s port %d\n", startMarker, port)
	return logf, nil
}

func (np *nodeProc) awaitReady() {
	for {
		if np.ipc != "" {
			// A full request rather than connect-and-close: a client that hangs
			// up before the server accepted can upset the pipe listener.
			if _, err := queryStatus(np.ipc, np.pid); err == nil {
				// signal.Notify follows stack.Start() in a goroutine; give it a moment.
				time.Sleep(2 * time.Second)
				select {
				case <-np.ready:
				default:
					close(np.ready)
				}
				return
			}
		}
		select {
		case <-np.exited:
			return
		case <-np.ready:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func queryStatus(ipc string, pid uint32) (*Snapshot, error) {
	conn, err := dialNode(ipc, pid)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	s, err := queryNode(conn)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func stopReadyWait() time.Duration {
	if hurryStop.Load() {
		return hurryReadyWait
	}
	return readyWait
}

// stop asks gocore to shut down like Ctrl+C in a terminal would, and only
// terminates it hard if that does not finish in time.
func (np *nodeProc) stop() {
	select {
	case <-np.exited:
		return
	case <-np.ready:
	case <-time.After(stopReadyWait()):
		log.Printf("gocore pid %d not ready after %v; stopping anyway", np.pid, stopReadyWait())
	}
	wait := gracefulStopWait
	if err := np.interrupt(); err != nil {
		// gocore may already be shutting down on its own (end of the session),
		// so keep waiting the full time before terminating.
		log.Printf("interrupt to pid %d failed: %v", np.pid, err)
	}
	select {
	case <-np.exited:
		log.Printf("gocore pid %d stopped", np.pid)
	case <-time.After(wait):
		log.Printf("gocore pid %d did not stop in %v; terminating", np.pid, wait)
		np.kill()
		<-np.exited
	}
}

func rotateLog(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxNodeLogBytes {
		old := strings.TrimSuffix(path, ".log") + ".old.log"
		os.Remove(old)
		os.Rename(path, old)
	}
}

// capLog empties a log that grew past limit. Writers use O_APPEND and simply
// continue at the new end.
func capLog(path string, limit int64) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > limit {
		if err := os.Truncate(path, 0); err != nil {
			log.Printf("truncate %s: %v", path, err)
		}
	}
}

var errDiskFull = errors.New("not enough free disk space")

// supervisor keeps exactly one gocore running until stopped. Start, Restart and
// Shutdown are serialised by ctrl, so a second click while a stop is still
// running simply waits for it.
type supervisor struct {
	p paths

	ctrl   sync.Mutex // held for the whole of Start/Restart/Shutdown
	closed bool       // set by Shutdown: no further starts

	mu       sync.Mutex // guards the fields below
	state    NodeState
	port     int // P2P port; moves up when another program holds the default
	proc     *nodeProc
	diskFull bool
	stopReq  chan struct{}
	done     chan struct{}
}

func newSupervisor(p paths) *supervisor {
	return &supervisor{p: p, state: NodeStopped}
}

// Current returns what the tray needs: state, running node (pid + pipe) and disk status.
func (s *supervisor) Current() (st NodeState, pid uint32, ipc string, diskFull bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc != nil {
		pid, ipc = s.proc.pid, s.proc.ipc
	}
	return s.state, pid, ipc, s.diskFull
}

func (s *supervisor) setState(st NodeState) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *supervisor) Start() {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.startLocked()
}

// Restart restarts a supervised node; it never starts one that is switched off.
func (s *supervisor) Restart() {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.mu.Lock()
	supervised := s.done != nil
	s.mu.Unlock()
	if !supervised {
		return
	}
	s.stopLocked()
	s.startLocked()
}

// Stop stops the node; a later Start runs it again (node switched off in the settings).
func (s *supervisor) Stop() {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.stopLocked()
}

// Shutdown stops the node for good; later Start/Restart calls do nothing.
func (s *supervisor) Shutdown() {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.closed = true
	s.stopLocked()
}

func (s *supervisor) startLocked() {
	if s.closed || s.done != nil {
		return
	}
	s.mu.Lock()
	s.stopReq = make(chan struct{})
	s.done = make(chan struct{})
	s.state = NodeStarting
	stopReq, done := s.stopReq, s.done
	s.mu.Unlock()
	go s.loop(stopReq, done)
}

func (s *supervisor) stopLocked() {
	s.mu.Lock()
	stopReq, done := s.stopReq, s.done
	s.mu.Unlock()
	if done == nil {
		// Not supervising, but a node left by a killed tray may still run
		// (node switched off, or settings missing): stop it too.
		if np, _ := adoptNode(s.p); np != nil {
			log.Printf("stopping unsupervised gocore pid %d", np.pid)
			s.setState(NodeStopping)
			np.stop()
			np.release()
			os.Remove(s.p.pidFile)
			s.setState(NodeStopped)
		}
		return
	}
	s.setState(NodeStopping)
	close(stopReq)
	<-done
	s.mu.Lock()
	s.stopReq, s.done = nil, nil
	s.state = NodeStopped
	s.mu.Unlock()
}

func (s *supervisor) loop(stopReq, done chan struct{}) {
	defer close(done)
	var crashes []time.Time
	for {
		s.setState(NodeStarting)
		np, err := adoptNode(s.p)
		if np == nil {
			// Adopting can take minutes (Linux: an outdated gocore is stopped
			// first); a stop requested meanwhile must not start a new one.
			select {
			case <-stopReq:
				return
			default:
			}
		}
		if err != nil {
			log.Printf("adopt gocore: %v", err)
			crashes = append(crashes, time.Now())
		} else if np == nil {
			if s.port == 0 {
				s.port = defaultP2PPort
			}
			np, err = startNode(s.p, s.port)
			s.mu.Lock()
			s.diskFull = errors.Is(err, errDiskFull)
			s.mu.Unlock()
			if err != nil {
				log.Printf("start gocore: %v", err)
				crashes = append(crashes, time.Now())
				// Retrying in 10 s cannot free disk space: show "disk full" now.
				for errors.Is(err, errDiskFull) && len(crashes) < maxCrashes {
					crashes = append(crashes, time.Now())
				}
			}
		}
		if np != nil {
			s.mu.Lock()
			s.proc, s.state = np, NodeRunning
			s.mu.Unlock()
			lowDisk := false
			tick := time.NewTicker(diskCheckEvery)
		watching:
			for {
				select {
				case <-stopReq:
					tick.Stop()
					s.setState(NodeStopping)
					np.stop()
					s.finish(np)
					return
				case <-tick.C:
					// gocore itself would write until the disk is full, breaking
					// system updates and other programs.
					if free, err := freeBytes(s.p.root); err == nil && free < stopFreeBytes {
						log.Printf("only %d MB free: stopping gocore", free>>20)
						s.setState(NodeStopping)
						np.stop()
						lowDisk = true
						break watching
					}
				case <-np.exited:
					break watching
				}
			}
			tick.Stop()
			if lowDisk {
				s.finish(np)
				s.mu.Lock()
				s.diskFull = true
				s.mu.Unlock()
				crashes = crashes[:0]
				s.setState(NodeFailed)
				select {
				case <-stopReq:
					return
				case <-time.After(failedRetry):
				}
				continue
			}
			log.Printf("gocore pid %d exited with code %d", np.pid, np.exitCode())
			if portInUse(s.p.nodeLog) && s.port < defaultP2PPort+20 {
				s.port++
				log.Printf("P2P port in use, next start uses port %d", s.port)
			}
			s.finish(np)
			crashes = append(crashes, time.Now())
		}
		delay := restartDelay
		if crashes = recent(crashes, crashWindow); len(crashes) >= maxCrashes {
			if free, err := freeBytes(s.p.root); err == nil && free < minFreeBytes {
				s.mu.Lock()
				s.diskFull = true
				s.mu.Unlock()
			}
			s.setState(NodeFailed)
			delay, crashes = failedRetry, nil
		} else {
			s.setState(NodeStarting)
		}
		select {
		case <-stopReq:
			return
		case <-time.After(delay):
		}
	}
}

func (s *supervisor) finish(np *nodeProc) {
	s.mu.Lock()
	s.proc = nil
	s.mu.Unlock()
	np.release()
	os.Remove(s.p.pidFile)
}

func recent(ts []time.Time, window time.Duration) []time.Time {
	cut := time.Now().Add(-window)
	out := ts[:0]
	for _, t := range ts {
		if t.After(cut) {
			out = append(out, t)
		}
	}
	return out
}
