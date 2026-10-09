// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no process groups: each command's processes go into a job
// object of their own instead, which kills them all when its handle
// closes. Blitz holds the handle, so it closes when the command ends
// (Release), when it's killed, and when Blitz exits or crashes: nothing a
// command started, a language server's children included, outlives Blitz
// (spec_visual_editor_037 VE-46).

// jobs are the started commands' job objects.
var jobs sync.Map // *exec.Cmd -> windows.Handle

// configureProcessGroup does nothing before the start: the job is made
// once the process exists (inGroup).
func configureProcessGroup(cmd *exec.Cmd) {}

// inGroup puts the started cmd's process in a job of its own that kills
// everything in it when closed, and returns what closes it. Anything the
// process starts from then on is in the job too.
func inGroup(cmd *exec.Cmd) func() {
	job, err := newKillOnCloseJob()
	if err != nil || cmd.Process == nil {
		return func() {}
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	jobs.Store(cmd, job)
	var once sync.Once
	return func() {
		once.Do(func() {
			jobs.Delete(cmd)
			windows.CloseHandle(job) // kills what's left in it
		})
	}
}

// newKillOnCloseJob is a job object that kills its processes when its
// last handle closes.
func newKillOnCloseJob() (windows.Handle, error) {
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
	return job, nil
}

// killProcessGroup kills the command and everything it started: its job,
// else the process alone.
func killProcessGroup(cmd *exec.Cmd) error {
	if job, ok := jobs.Load(cmd); ok {
		return windows.TerminateJobObject(job.(windows.Handle), 1)
	}
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// guardArgv has nothing to wrap on Windows: the job object is the guard.
func guardArgv(argv []string) ([]string, *os.File, func(), error) {
	return argv, nil, func() {}, nil
}
