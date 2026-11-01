//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// gracefulStopWait: as on Windows. At the end of the session systemd may cut
// this short (user@.service stop timeout), which a quick flush stays under.
const gracefulStopWait = 5 * time.Minute

// hurryReadyWait: readyWait while the session ends (unit TimeoutStopSec).
const hurryReadyWait = 30 * time.Second

// nodeHandle follows one gocore process. A child of ours is reaped by
// cmd.Wait; an adopted one (left by an earlier tray) by a pidfd, which also
// makes signals safe against pid reuse.
type nodeHandle struct {
	cmd   *exec.Cmd // our child, nil when adopted
	pidfd int       // adopted: pidfd, or -1 (kernel < 5.3: polling); guarded by mu
	start uint64    // start time from /proc/<pid>/stat, against pid reuse
	mu    sync.Mutex
	code  int
	// released: supervision ended; the watcher goroutine, which owns the
	// pidfd, closes it when the process ends and no longer reaps it.
	released bool
}

var errForeignNode = errors.New("another gocore uses the data folder (started by hand or by another program?)")

// gocoreCommand is how the official gocore is started: directly, or through
// the bundled glibc runtime if the system's glibc is older than the one the
// release was built against (GLIBC_2.38: Ubuntu 22.04, Debian 12). The bundled
// runtime is only used when needed: on a newer system its libc would load the
// system's NSS modules, which may need a newer glibc than the bundled one.
func gocoreCommand(p paths) []string {
	gocoreModeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, p.gocore, "version").CombinedOutput()
		gocoreViaRuntime = err != nil && bytes.Contains(out, []byte("GLIBC_")) && exists(p.glibc)
		if err != nil {
			log.Printf("gocore version: %v: %s", err, bytes.TrimSpace(out))
		}
		if gocoreViaRuntime {
			log.Printf("system glibc too old for gocore: using the bundled runtime")
		}
	})
	if !gocoreViaRuntime {
		return []string{p.gocore}
	}
	// The loader file is named gocore, so the process shows as "gocore".
	return []string{filepath.Join(p.glibc, gocoreName), "--argv0", gocoreName, "--library-path", p.glibc, p.gocore}
}

var (
	gocoreModeOnce   sync.Once
	gocoreViaRuntime bool
)

func startNode(p paths, port int) (*nodeProc, error) {
	if free, err := freeBytes(p.root); err == nil && free < minFreeBytes {
		return nil, errDiskFull
	}
	if err := os.MkdirAll(p.data, 0o700); err != nil {
		return nil, err
	}
	os.Chmod(p.data, 0o700) // also an older folder made with 0755 (holds the keystore)
	ipc := ipcPath(p)
	if len(ipc) >= 108 { // sun_path
		return nil, fmt.Errorf("IPC path too long: %s", ipc)
	}
	logf, err := openNodeLog(p, port)
	if err != nil {
		return nil, err
	}
	defer logf.Close() // the child keeps its own copy
	argv := append(gocoreCommand(p), nodeArgs(p, ipc, port)...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = p.root
	// Go's own DNS resolver: no system NSS modules in gocore (see gocoreCommand).
	cmd.Env = append(childEnv(), godebugNetdns())
	cmd.Stdout, cmd.Stderr = logf, logf
	// Own process group: Ctrl+C in a terminal that started the tray, or a
	// signal to the tray's group, does not reach gocore; we stop it ourselves.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	// Lower priority: the first sync should not make the PC sluggish.
	if err := unix.Setpriority(unix.PRIO_PROCESS, pid, 10); err != nil {
		log.Printf("gocore priority: %v", err)
	}
	start, _ := procStartTime(pid)
	np := &nodeProc{pid: uint32(pid), ipc: ipc, handle: nodeHandle{cmd: cmd, pidfd: -1, start: start},
		exited: make(chan struct{}), ready: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		np.handle.mu.Lock()
		np.handle.code = cmd.ProcessState.ExitCode()
		np.handle.mu.Unlock()
		if err != nil && cmd.ProcessState == nil {
			log.Printf("wait gocore: %v", err)
		}
		close(np.exited)
	}()
	go np.awaitReady()
	if err := writeFileAtomic(p.pidFile, []byte(fmt.Sprintf("%d %d", pid, start))); err != nil {
		// Without a pid file a later run could not adopt it and would start a second node.
		np.stop()
		return nil, fmt.Errorf("write pid file: %w", err)
	}
	log.Printf("started gocore pid %d (%s)", pid, strings.Join(argv[:len(argv)-len(nodeArgs(p, ipc, port))], " "))
	return np, nil
}

// adoptNode picks up a gocore left running by an earlier instance of this
// program (e.g. after the tray was killed), so we never start a second one on
// the same data.
func adoptNode(p paths) (*nodeProc, error) {
	pid, start, err := findNode(p)
	if err != nil || pid == 0 {
		return nil, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		fd = -1 // old kernel: poll /proc instead
	} else if !sameProcess(pid, start) || !isOurNode(p, pid) {
		// The pid was reused between the check and the open: not our node.
		unix.Close(fd)
		return nil, nil
	}
	np := &nodeProc{pid: uint32(pid), ipc: ipcPath(p), handle: nodeHandle{pidfd: fd, start: start, code: -1},
		exited: make(chan struct{}), ready: make(chan struct{})}
	go func() {
		if fd >= 0 {
			for {
				fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
				if _, err := unix.Poll(fds, -1); err == nil || !errors.Is(err, unix.EINTR) {
					break
				}
			}
		} else {
			for sameProcess(pid, start) {
				time.Sleep(time.Second)
			}
		}
		h := &np.handle
		h.mu.Lock()
		if fd >= 0 {
			unix.Close(fd) // under mu: signal never uses a closed (or reused) number
			h.pidfd = -1
		}
		released := h.released
		h.mu.Unlock()
		// Still our child after the tray restarted itself for an update: reap
		// it, unless supervision was handed back (whoever has it now reaps).
		var ws unix.WaitStatus
		if !released {
			if wpid, _ := unix.Wait4(pid, &ws, unix.WNOHANG, nil); wpid == pid && ws.Exited() {
				h.mu.Lock()
				h.code = ws.ExitStatus()
				h.mu.Unlock()
			}
		}
		close(np.exited)
	}()
	go np.awaitReady()
	log.Printf("adopted running gocore pid %d", pid)
	if nodeOutdated(pid) {
		// A package update replaced gocore while it ran: stop the old one, the
		// caller then starts the new version.
		log.Printf("gocore pid %d runs a replaced program file: restarting it", pid)
		np.stop()
		np.release()
		os.Remove(p.pidFile)
		return nil, nil
	}
	return np, nil
}

// nodeOutdated: the program file of the running gocore (or of the runtime's
// loader that runs it) was replaced or removed by the package manager.
func nodeOutdated(pid int) bool {
	exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	return err == nil && strings.HasSuffix(exe, " (deleted)")
}

// findNode returns a running gocore of this user on our data folder: first via
// the pid file, else by scanning /proc (the pid file can be missing after a
// crash between start and writing it). A gocore of someone else holding the
// data folder's lock is reported as errForeignNode, so we never start a second
// node next to it.
func findNode(p paths) (pid int, start uint64, err error) {
	if raw, err := os.ReadFile(p.pidFile); err == nil {
		f := strings.Fields(string(raw))
		if len(f) == 2 {
			pid, _ := strconv.Atoi(f[0])
			start, _ := strconv.ParseUint(f[1], 10, 64)
			if pid > 0 && ownProcess(pid) && sameProcess(pid, start) && isOurNode(p, pid) {
				return pid, start, nil
			}
		}
	}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !ownProcess(pid) || !isOurNode(p, pid) {
			continue
		}
		if start, err := procStartTime(pid); err == nil {
			return pid, start, nil
		}
	}
	if dataDirLocked(p) {
		return 0, 0, errForeignNode
	}
	return 0, 0, nil
}

// isOurNode: a gocore (directly or through the runtime) on our data folder.
func isOurNode(p paths, pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	hasGocore, hasData := false, false
	for i, a := range args {
		if a == p.gocore {
			hasGocore = true
		}
		if a == "--datadir" && i+1 < len(args) && args[i+1] == p.data {
			hasData = true
		}
	}
	return hasGocore && hasData
}

// dataDirLocked reports whether some gocore holds the lock on our data folder
// (gocore takes flock(LOCK_EX) on <datadir>/gocore/LOCK while it runs).
func dataDirLocked(p paths) bool {
	f, err := os.Open(filepath.Join(p.data, "gocore", "LOCK"))
	if err != nil {
		return false
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return errors.Is(err, unix.EWOULDBLOCK)
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false
}

// procStartTime is field 22 of /proc/<pid>/stat (clock ticks since boot).
func procStartTime(pid int) (uint64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name (field 2) is in parentheses and may contain spaces.
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, errors.New("bad stat")
	}
	f := strings.Fields(s[i+1:]) // f[0] is field 3
	if len(f) < 20 {
		return 0, errors.New("bad stat")
	}
	return strconv.ParseUint(f[19], 10, 64)
}

func sameProcess(pid int, start uint64) bool {
	st, err := procStartTime(pid)
	return err == nil && st == start
}

func ownProcess(pid int) bool {
	fi, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// dialNode connects to gocore's IPC socket and checks that the server is the
// expected gocore process.
func dialNode(ipc string, pid uint32) (net.Conn, error) {
	if ipc == "" || pid == 0 {
		return nil, errors.New("node not running")
	}
	conn, err := net.DialTimeout("unix", ipc, 2*time.Second)
	if err != nil {
		return nil, err
	}
	cred, err := peerCred(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if uint32(cred.Pid) != pid {
		conn.Close()
		return nil, fmt.Errorf("socket served by pid %d, expected gocore pid %d", cred.Pid, pid)
	}
	return conn, nil
}

func peerCred(conn net.Conn) (*unix.Ucred, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("not a unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *unix.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return cred, cerr
}

func (np *nodeProc) exitCode() int {
	np.handle.mu.Lock()
	defer np.handle.mu.Unlock()
	return np.handle.code
}

// interrupt asks gocore to stop like Ctrl+C would (gocore handles SIGINT).
func (np *nodeProc) interrupt() error { return np.signal(unix.SIGINT) }

func (np *nodeProc) kill() { np.signal(unix.SIGKILL) }

func (np *nodeProc) signal(sig unix.Signal) error {
	select {
	case <-np.exited:
		return nil
	default:
	}
	h := &np.handle
	if h.cmd != nil { // our child: its pid stays ours until Wait reaps it
		return h.cmd.Process.Signal(sig)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.pidfd >= 0:
		return unix.PidfdSendSignal(h.pidfd, sig, nil, 0)
	case sameProcess(int(np.pid), h.start):
		return unix.Kill(int(np.pid), sig)
	}
	return nil
}

func (np *nodeProc) release() {
	np.handle.mu.Lock()
	np.handle.released = true
	np.handle.mu.Unlock()
}

// killProcessesOf ends every process of this user whose executable is image
// (name is unused on Linux; the path is exact).
func killProcessesOf(image, _ string) {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() || !ownProcess(pid) {
			continue
		}
		if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil && exe == image {
			log.Printf("ending leftover %s pid %d", filepath.Base(image), pid)
			unix.Kill(pid, unix.SIGKILL)
		}
	}
}

// godebugNetdns adds netdns=go to the user's GODEBUG instead of replacing it:
// gocore then resolves names without the system's NSS modules.
func godebugNetdns() string {
	if g := os.Getenv("GODEBUG"); g != "" {
		return "GODEBUG=" + g + ",netdns=go"
	}
	return "GODEBUG=netdns=go"
}
