//go:build windows

package main

import (
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
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const (
	gracefulStopWait = 5 * time.Minute // LevelDB flush on a slow disk can take a while
	// readyWait while the session ends: well inside sessionEndWait (25 s), so
	// gocore gets CTRL_BREAK before Windows ends it.
	hurryReadyWait = 12 * time.Second
	stillActive    = 259 // STILL_ACTIVE exit code
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole         = kernel32.NewProc("AttachConsole")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
	procGenerateConsoleCtrlEv = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

// nodeHandle is the process handle of a supervised gocore.
type nodeHandle = windows.Handle

func startNode(p paths, port int) (*nodeProc, error) {
	if free, err := freeBytes(p.root); err == nil && free < minFreeBytes {
		return nil, errDiskFull
	}
	ipc, err := newPipeName(p)
	if err != nil {
		return nil, err
	}
	logf, err := openNodeLog(p, port)
	if err != nil {
		return nil, err
	}
	defer logf.Close() // the child keeps its own inherited handle

	cmd := exec.Command(p.gocore, nodeArgs(p, ipc, port)...)
	cmd.Dir = p.bin
	cmd.Stdout = logf
	cmd.Stderr = logf
	// Own process group: lets us send CTRL_BREAK to gocore alone for a clean shutdown.
	// No window: gocore is a console program, the user should only see the tray icon.
	// Below-normal priority: the first sync should not make the PC sluggish.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW | windows.BELOW_NORMAL_PRIORITY_CLASS,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := uint32(cmd.Process.Pid)
	h, err := openProcess(pid)
	if err != nil {
		// Without a handle we cannot supervise it; it has not opened the database yet.
		cmd.Process.Kill()
		cmd.Process.Release()
		return nil, err
	}
	cmd.Process.Release()
	np := watch(pid, ipc, h)
	if err := writeFileAtomic(p.pidFile, []byte(strconv.FormatUint(uint64(pid), 10))); err != nil {
		// Without a pid file a later run could not adopt it and would start a second node.
		np.stop()
		windows.CloseHandle(h)
		return nil, fmt.Errorf("write pid file: %w", err)
	}
	log.Printf("started gocore pid %d", pid)
	return np, nil
}

// adoptNode picks up a gocore left running by an earlier instance of this program
// (e.g. after it was killed in Task Manager or Windows ended the tray), so we never
// start a second one on the same data.
func adoptNode(p paths) (*nodeProc, error) {
	pid, h, err := findNode(p)
	if err != nil || h == 0 {
		return nil, err
	}
	ipc, _ := currentPipeName(p) // empty if missing: status unknown, stop still works
	log.Printf("adopted running gocore pid %d", pid)
	np := watch(pid, ipc, h)
	if ipc == "" {
		// Cannot probe readiness without the pipe; an orphan has normally been
		// running for a while, so do not hold up stopping it.
		close(np.ready)
	}
	return np, nil
}

var errForeignNode = errors.New("a gocore.exe is running that this program cannot control (started as administrator?)")

// findNode returns a running gocore started from our bin folder: first via the
// pid file, otherwise by scanning all processes (the pid file can be missing
// after a crash between start and writing it). A gocore.exe we cannot inspect
// is reported as errForeignNode, so we never start a second node next to it.
func findNode(p paths) (uint32, windows.Handle, error) {
	if raw, err := os.ReadFile(p.pidFile); err == nil {
		if pid, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32); err == nil {
			if h, ok := openOurNode(p, uint32(pid)); ok {
				return uint32(pid), h, nil
			}
		}
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, 0, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	foreign := false
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), "gocore.exe") {
			continue
		}
		if h, ok := openOurNode(p, pe.ProcessID); ok {
			return pe.ProcessID, h, nil
		}
		if isForeignNode(pe.ProcessID) {
			foreign = true
		}
	}
	if foreign {
		return 0, 0, errForeignNode
	}
	return 0, 0, nil
}

// isForeignNode: a gocore.exe in our own session that we may not open (access
// denied) could be ours started as administrator. Processes that already
// exited or belong to another user's session do not count.
func isForeignNode(pid uint32) bool {
	h, err := openProcess(pid)
	if err == nil {
		windows.CloseHandle(h)
		return false
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return false
	}
	var mine, theirs uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &mine) != nil ||
		windows.ProcessIdToSessionId(pid, &theirs) != nil {
		return true
	}
	return mine == theirs
}

// openOurNode opens pid if it is a live process of our bin\gocore.exe.
func openOurNode(p paths, pid uint32) (windows.Handle, bool) {
	h, err := openProcess(pid)
	if err != nil {
		return 0, false
	}
	var code uint32
	image, err := processImage(h)
	if err != nil || !strings.EqualFold(filepath.Clean(image), filepath.Clean(p.gocore)) ||
		windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		windows.CloseHandle(h)
		return 0, false
	}
	return h, true
}

func openProcess(pid uint32) (windows.Handle, error) {
	return windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, pid)
}

// watch takes ownership of h; whoever ends supervision closes it.
func watch(pid uint32, ipc string, h windows.Handle) *nodeProc {
	np := &nodeProc{pid: pid, ipc: ipc, handle: h, exited: make(chan struct{}), ready: make(chan struct{})}
	go func() {
		windows.WaitForSingleObject(h, windows.INFINITE)
		close(np.exited)
	}()
	go np.awaitReady()
	return np
}

// dialNode connects to gocore's IPC pipe and checks that the pipe server is the
// expected gocore process, not another program that took the name.
func dialNode(ipc string, pid uint32) (net.Conn, error) {
	if ipc == "" || pid == 0 {
		return nil, errors.New("node not running")
	}
	timeout := 2 * time.Second
	conn, err := winio.DialPipe(`\\.\pipe\`+ipc, &timeout)
	if err != nil {
		return nil, err
	}
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		conn.Close()
		return nil, errors.New("pipe handle not available")
	}
	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &serverPID); err != nil {
		conn.Close()
		return nil, err
	}
	if serverPID != pid {
		conn.Close()
		return nil, fmt.Errorf("pipe served by pid %d, expected gocore pid %d", serverPID, pid)
	}
	return conn, nil
}

func processImage(h windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func (np *nodeProc) exitCode() uint32 {
	var code uint32
	windows.GetExitCodeProcess(np.handle, &code)
	return code
}

// interrupt raises CTRL_BREAK for gocore (see sendCtrlBreak).
func (np *nodeProc) interrupt() error { return sendCtrlBreak(np.pid) }

func (np *nodeProc) kill() { windows.TerminateProcess(np.handle, 1) }

// release closes the process handle; whoever ends supervision calls it.
func (np *nodeProc) release() { windows.CloseHandle(np.handle) }

var consoleMu sync.Mutex

// sendCtrlBreak attaches to gocore's hidden console and raises CTRL_BREAK for its
// process group. Go programs (gocore included) receive that as os.Interrupt.
func sendCtrlBreak(pid uint32) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	if r, _, err := procAttachConsole.Call(uintptr(pid)); r == 0 {
		return fmt.Errorf("AttachConsole: %w", err)
	}
	defer procFreeConsole.Call()
	if r, _, err := procGenerateConsoleCtrlEv.Call(windows.CTRL_BREAK_EVENT, uintptr(pid)); r == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	return nil
}

// killProcessesOf ends every running process whose image is exactly image
// (exeName only narrows the scan). Used for miners left without their tray.
func killProcessesOf(image, exeName string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), exeName) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pe.ProcessID)
		if err != nil {
			continue
		}
		if img, err := processImage(h); err == nil && strings.EqualFold(filepath.Clean(img), filepath.Clean(image)) {
			log.Printf("ending leftover %s pid %d", exeName, pe.ProcessID)
			windows.TerminateProcess(h, 1)
			windows.WaitForSingleObject(h, 5000)
		}
		windows.CloseHandle(h)
	}
}
