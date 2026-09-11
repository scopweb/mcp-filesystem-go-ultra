//go:build windows

package main

import (
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureChild(_ *exec.Cmd) {}

func bindChildLifetime(p *os.Process) (func(), error) {
	noop := func() {
		if p != nil {
			_ = p.Kill()
		}
	}
	job, err := createKillOnCloseJob()
	if err != nil {
		return noop, err
	}
	if err := assignProcessToJob(job, p.Pid); err != nil {
		_ = windows.CloseHandle(job)
		return noop, err
	}
	return func() {
		_ = windows.CloseHandle(job)
	}, nil
}

func createKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func assignProcessToJob(job windows.Handle, pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(job, h)
}
