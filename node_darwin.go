//go:build darwin

package main

import (
	"bytes"
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

// gracefulStopWait: as on Windows. At logout launchd may cut this short (the
// job's ExitTimeOut), which a quick flush stays under.
const gracefulStopWait = 5 * time.Minute

// hurryReadyWait: readyWait while the session ends (launchd ExitTimeOut 120 s).
const hurryReadyWait = 30 * time.Second

// sunPathMax: macOS keeps socket paths shorter than Linux (sockaddr_un).
const sunPathMax = 104

// nodeHandle follows one gocore process. A child of ours is reaped by
// cmd.Wait; an adopted one (left by an earlier helper) is watched with
// kqueue. macOS has no pidfd: signals to an adopted node first check its
// start time, against pid reuse.
type nodeHandle struct {
	cmd   *exec.Cmd // our child, nil when adopted
	start uint64    // start time (µs since 1970) from the kernel's process table
	mu    sync.Mutex
	code  int
	// released: supervision ended; whoever has it now reaps the process.
	released bool
}

var errForeignNode = errors.New("another gocore uses the data folder (started by hand or by another program?)")

func startNode(p paths, port int) (*nodeProc, error) {
	if free, err := freeBytes(p.root); err == nil && free < minFreeBytes {
		return nil, errDiskFull
	}
	if err := os.MkdirAll(p.data, 0o700); err != nil {
		return nil, err
	}
	os.Chmod(p.data, 0o700) // holds the keystore
	excludeFromBackup(p.data)
	ipc := ipcPath(p)
	if len(ipc) >= sunPathMax {
		return nil, fmt.Errorf("IPC path too long: %s", ipc)
	}
	logf, err := openNodeLog(p, port)
	if err != nil {
		return nil, err
	}
	defer logf.Close() // the child keeps its own copy
	args := nodeArgs(p, ipc, port)
	cmd := exec.Command(p.gocore, args...)
	cmd.Dir = p.root
	cmd.Env = childEnv()
	cmd.Stdout, cmd.Stderr = logf, logf
	// Own process group: a signal to the helper's group does not reach
	// gocore; we stop it ourselves.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	ino := fileID(p.gocore)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	// Lower priority: the first sync should not make the Mac sluggish.
	if err := unix.Setpriority(unix.PRIO_PROCESS, pid, 10); err != nil {
		log.Printf("gocore priority: %v", err)
	}
	start, _ := procStartTime(pid)
	np := &nodeProc{pid: uint32(pid), ipc: ipc, handle: nodeHandle{cmd: cmd, start: start},
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
	// The program file's identity goes with the pid: after an app update the
	// running gocore is the old one (see adoptNode).
	if err := writeFileAtomic(p.pidFile, []byte(fmt.Sprintf("%d %d %s", pid, start, ino))); err != nil {
		// Without a pid file a later run could not adopt it and would start a second node.
		np.stop()
		return nil, fmt.Errorf("write pid file: %w", err)
	}
	log.Printf("started gocore pid %d", pid)
	return np, nil
}

// fileID: device and inode of a file, "" if it cannot be read. Replacing the
// app gives gocore a new inode at the same path.
func fileID(path string) string {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}

// adoptNode picks up a gocore left running by an earlier helper (after a crash
// or an update), so we never start a second one on the same data.
func adoptNode(p paths) (*nodeProc, error) {
	pid, start, ino, err := findNode(p)
	if err != nil || pid == 0 {
		return nil, err
	}
	kq, err := watchExit(pid)
	if err != nil {
		return nil, nil // ended in between
	}
	if !sameProcess(pid, start) || !isOurNode(p, pid) {
		unix.Close(kq) // the pid was reused between the check and the watch
		return nil, nil
	}
	np := &nodeProc{pid: uint32(pid), ipc: ipcPath(p), handle: nodeHandle{start: start, code: -1},
		exited: make(chan struct{}), ready: make(chan struct{})}
	go func() {
		ev := make([]unix.Kevent_t, 1)
		for {
			if _, err := unix.Kevent(kq, nil, ev, nil); err == nil || !errors.Is(err, unix.EINTR) {
				break
			}
		}
		unix.Close(kq)
		h := &np.handle
		h.mu.Lock()
		released := h.released
		h.mu.Unlock()
		// Still our child after the helper re-executed itself: reap it.
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
	if ino != "" && ino != fileID(p.gocore) {
		// The app was replaced while gocore ran: stop the old one, the caller
		// then starts the new version.
		log.Printf("gocore pid %d runs a replaced program file: restarting it", pid)
		np.stop()
		np.release()
		os.Remove(p.pidFile)
		return nil, nil
	}
	return np, nil
}

// watchExit returns a kqueue that reports when pid ends.
func watchExit(pid int) (int, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(kq)
	ev := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(kq)
		return -1, err
	}
	return kq, nil
}

// findNode returns a running gocore of this user on our data folder: first via
// the pid file, else from the process table (the pid file can be missing after
// a crash between start and writing it). A gocore of someone else holding the
// data folder's lock is reported as errForeignNode.
func findNode(p paths) (pid int, start uint64, ino string, err error) {
	if raw, err := os.ReadFile(p.pidFile); err == nil {
		f := strings.Fields(string(raw))
		if len(f) >= 2 {
			pid, _ := strconv.Atoi(f[0])
			start, _ := strconv.ParseUint(f[1], 10, 64)
			ino := ""
			if len(f) == 3 {
				ino = f[2]
			}
			if pid > 0 && ownProcess(pid) && sameProcess(pid, start) && isOurNode(p, pid) {
				return pid, start, ino, nil
			}
		}
	}
	procs, _ := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	for _, kp := range procs {
		pid := int(kp.Proc.P_pid)
		if pid == os.Getpid() || !isOurNode(p, pid) {
			continue
		}
		if start, err := procStartTime(pid); err == nil {
			return pid, start, "", nil
		}
	}
	if dataDirLocked(p) {
		return 0, 0, "", errForeignNode
	}
	return 0, 0, "", nil
}

// procArgs: the program path and arguments of pid (kern.procargs2: argc,
// executable path, padding, then argv; only for own processes).
func procArgs(pid int) (exe string, args []string, err error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", nil, err
	}
	if len(raw) < 4 {
		return "", nil, errors.New("short procargs")
	}
	argc := int(uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24)
	rest := raw[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return "", nil, errors.New("bad procargs")
	}
	exe, rest = string(rest[:i]), rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for len(args) < argc && len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			j = len(rest)
		}
		args = append(args, string(rest[:j]))
		rest = rest[min(j+1, len(rest)):]
	}
	return exe, args, nil
}

// isOurNode: our bundle's gocore on our data folder.
func isOurNode(p paths, pid int) bool {
	exe, args, err := procArgs(pid)
	if err != nil || exe != p.gocore {
		return false
	}
	for i, a := range args {
		if a == "--datadir" && i+1 < len(args) && args[i+1] == p.data {
			return true
		}
	}
	return false
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

// procStartTime: the process's start time in µs since 1970.
func procStartTime(pid int) (uint64, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	if int(kp.Proc.P_pid) != pid {
		return 0, errors.New("no such process")
	}
	t := kp.Proc.P_starttime
	return uint64(t.Sec)*1e6 + uint64(t.Usec), nil
}

func sameProcess(pid int, start uint64) bool {
	st, err := procStartTime(pid)
	return err == nil && st == start
}

func ownProcess(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && int(kp.Proc.P_pid) == pid && int(kp.Eproc.Ucred.Uid) == os.Getuid()
}

// dialNode connects to gocore's IPC socket and checks that the server is the
// expected gocore process (LOCAL_PEERPID).
func dialNode(ipc string, pid uint32) (net.Conn, error) {
	if ipc == "" || pid == 0 {
		return nil, errors.New("node not running")
	}
	conn, err := net.DialTimeout("unix", ipc, 2*time.Second)
	if err != nil {
		return nil, err
	}
	peer, err := sockopt(conn, unix.LOCAL_PEERPID)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if uint32(peer) != pid {
		conn.Close()
		return nil, fmt.Errorf("socket served by pid %d, expected gocore pid %d", peer, pid)
	}
	return conn, nil
}

// peerUID: the user id of the other end of a unix socket (LOCAL_PEERCRED).
func peerUID(conn net.Conn) (int, error) {
	raw, err := rawConn(conn)
	if err != nil {
		return -1, err
	}
	uid, cerr := -1, error(nil)
	if err := raw.Control(func(fd uintptr) {
		var x *unix.Xucred
		if x, cerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED); cerr == nil {
			uid = int(x.Uid)
		}
	}); err != nil {
		return -1, err
	}
	return uid, cerr
}

func sockopt(conn net.Conn, opt int) (int, error) {
	raw, err := rawConn(conn)
	if err != nil {
		return 0, err
	}
	v, cerr := 0, error(nil)
	if err := raw.Control(func(fd uintptr) { v, cerr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, opt) }); err != nil {
		return 0, err
	}
	return v, cerr
}

func rawConn(conn net.Conn) (syscall.RawConn, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("not a unix socket")
	}
	return uc.SyscallConn()
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
	if sameProcess(int(np.pid), h.start) {
		return unix.Kill(int(np.pid), sig)
	}
	return nil
}

func (np *nodeProc) release() {
	np.handle.mu.Lock()
	np.handle.released = true
	np.handle.mu.Unlock()
}

// killProcessesOf ends every process of this user whose program is image
// (name is unused on macOS; the path is exact).
func killProcessesOf(image, _ string) {
	procs, _ := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	for _, kp := range procs {
		pid := int(kp.Proc.P_pid)
		if pid == os.Getpid() {
			continue
		}
		if exe, _, err := procArgs(pid); err == nil && exe == image {
			log.Printf("ending leftover %s pid %d", filepath.Base(image), pid)
			unix.Kill(pid, unix.SIGKILL)
		}
	}
}
