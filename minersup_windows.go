//go:build windows && !nominer

package main

import (
	"errors"
	"log"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	minerExeName = "core-miner.exe"
	idlePriority = 0x00000040
)

// minerGuard is a job object that kills the miner when the tray process ends.
type minerGuard windows.Handle

func (g minerGuard) ok() bool { return g != 0 }

func newMinerGuard() (minerGuard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return minerGuard(job), nil
}

func minerProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | idlePriority,
		HideWindow:    true,
	}
}

// startGuarded starts the miner and puts it into the job object.
func startGuarded(g minerGuard, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		const errorVirusInfected, errorVirusDeleted = syscall.Errno(225), syscall.Errno(226)
		if errors.Is(err, errorVirusInfected) || errors.Is(err, errorVirusDeleted) {
			return errBlocked
		}
		return err
	}
	if err := assignJob(g, cmd.Process.Pid); err != nil {
		log.Printf("miner job object: %v", err)
		cmd.Process.Kill()
		cmd.Wait()
		return errNoJob
	}
	return nil
}

func assignJob(g minerGuard, pid int) error {
	if !g.ok() {
		return errNoJob
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(windows.Handle(g), h)
}

// minerIntact checks core-miner.exe against the hash it was installed with
// and restores it once from the embedded payload (e.g. after the user added an
// antivirus exception). Only if it is still missing or changed is it blocked.
func minerIntact(p paths) bool {
	for _, f := range payloadFiles {
		if f.Name != "core-miner.exe" {
			continue
		}
		if h, err := fileSHA256(p.miner); err == nil && h == f.SHA256 {
			return true
		}
		// An older version's file can stay locked for a moment after its
		// miner ended (update): a few tries before it counts as blocked.
		var err error
		for i := 0; i < 5; i++ {
			if err = extractOne(f, p.miner); err == nil || !exists(p.miner) {
				break
			}
			time.Sleep(time.Second)
		}
		if err != nil {
			log.Printf("restore core-miner.exe: %v", err)
			return false
		}
		h, err := fileSHA256(p.miner)
		return err == nil && h == f.SHA256
	}
	return true // payload without the miner (dev build): nothing to compare
}
