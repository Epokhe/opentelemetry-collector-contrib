// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package processscraper // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/processscraper"

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

// getGopsutilProcessHandles lists processes with one kern.proc.all sysctl and
// keeps each process's name, start time and parent pid from that list.
// gopsutil's ProcessesWithContext reads the same list, then reads every
// process again with kern.proc.pid for its start time, and NameWithContext
// reads it once more.
//
// It also reads each process's task info (CPU times and memory) once.
// gopsutil ignores proc_pidinfo's result, so a refused read becomes zero CPU
// and zero memory. A process whose task info macOS refuses (EPERM: another
// user's process) or no longer has (ESRCH: exited, or a zombie) is left out of
// the list. Any other failure stays with the handle and fails its CPU and
// memory reads, so the scrape reports it and emits no metrics for it.
func getGopsutilProcessHandles(context.Context) (processHandles, error) {
	lib, err := loadLibSystem()
	if err != nil {
		return nil, err
	}
	kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}

	handles := make([]darwinProcessHandle, 0, len(kprocs))
	for i := range kprocs {
		k := &kprocs[i]
		// gopsutil rejects pid 0 (kernel_task).
		if k.Proc.P_pid <= 0 {
			continue
		}
		taskInfo, taskInfoErr := lib.taskInfo(k.Proc.P_pid)
		if errors.Is(taskInfoErr, unix.EPERM) || errors.Is(taskInfoErr, unix.ESRCH) {
			continue
		}
		handles = append(handles, darwinProcessHandle{
			wrappedProcessHandle: wrappedProcessHandle{
				Process:   &process.Process{Pid: k.Proc.P_pid},
				parentPid: k.Eproc.Ppid,
				flags:     flagParentPidSet,
			},
			comm:        unix.ByteSliceToString(k.Proc.P_comm[:]),
			createTime:  k.Proc.P_starttime.Sec*1000 + int64(k.Proc.P_starttime.Usec)/1000,
			taskInfo:    taskInfo,
			taskInfoErr: taskInfoErr,
			timescale:   lib.timescale,
		})
	}
	return &darwinProcessHandles{handles: handles}, nil
}

type darwinProcessHandles struct {
	handles []darwinProcessHandle
}

func (p *darwinProcessHandles) Pid(index int) int32 {
	return p.handles[index].Pid
}

func (p *darwinProcessHandles) At(index int) processHandle {
	return &p.handles[index]
}

func (p *darwinProcessHandles) Len() int {
	return len(p.handles)
}

type darwinProcessHandle struct {
	wrappedProcessHandle
	comm        string
	createTime  int64
	taskInfo    process.ProcTaskInfo
	taskInfoErr error
	timescale   float64
}

// CreateTimeWithContext returns the start time from the process list, in
// milliseconds since the epoch, as gopsutil computes it.
func (p *darwinProcessHandle) CreateTimeWithContext(context.Context) (int64, error) {
	return p.createTime, nil
}

// NameWithContext returns the name gopsutil returns: p_comm, or, when p_comm
// may be truncated (15 characters or more), the base name of the first
// command-line argument.
func (p *darwinProcessHandle) NameWithContext(ctx context.Context) (string, error) {
	if len(p.comm) < 15 {
		return p.comm, nil
	}
	args, err := p.CmdlineSliceWithContext(ctx)
	if err != nil {
		return "", err
	}
	if len(args) == 0 || args[0] == "" {
		return p.comm, nil
	}
	return filepath.Base(args[0]), nil
}

// TimesWithContext returns the CPU times from the task info read while
// listing, converted from Mach ticks as gopsutil does.
func (p *darwinProcessHandle) TimesWithContext(context.Context) (*cpu.TimesStat, error) {
	if p.taskInfoErr != nil {
		return nil, p.taskInfoErr
	}
	return &cpu.TimesStat{
		CPU:    "cpu",
		User:   float64(p.taskInfo.Total_user) * p.timescale / 1e9,
		System: float64(p.taskInfo.Total_system) * p.timescale / 1e9,
	}, nil
}

// MemoryInfoWithContext returns the resident and virtual size from the task
// info read while listing.
func (p *darwinProcessHandle) MemoryInfoWithContext(context.Context) (*process.MemoryInfoStat, error) {
	if p.taskInfoErr != nil {
		return nil, p.taskInfoErr
	}
	return &process.MemoryInfoStat{
		RSS: p.taskInfo.Resident_size,
		VMS: p.taskInfo.Virtual_size,
	}, nil
}

// libSystem holds proc_pidinfo and the Mach tick length, loaded once per
// collector; gopsutil opens and closes libSystem on every call.
type libSystem struct {
	procPidInfo uintptr
	// timescale is nanoseconds per Mach tick.
	timescale float64
}

var loadLibSystem = sync.OnceValues(func() (*libSystem, error) {
	handle, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	procPidInfo, err := purego.Dlsym(handle, "proc_pidinfo")
	if err != nil {
		return nil, err
	}
	machTimebaseInfo, err := purego.Dlsym(handle, "mach_timebase_info")
	if err != nil {
		return nil, err
	}
	var timebase struct{ numer, denom uint32 }
	if r1, _, _ := purego.SyscallN(machTimebaseInfo, uintptr(unsafe.Pointer(&timebase))); r1 != 0 {
		return nil, fmt.Errorf("mach_timebase_info: kern_return %d", int32(r1))
	}
	return &libSystem{procPidInfo: procPidInfo, timescale: float64(timebase.numer) / float64(timebase.denom)}, nil
})

// procPidTaskInfo is PROC_PIDTASKINFO from sys/proc_info.h.
const procPidTaskInfo = 4

// taskInfo reads one process's task info. proc_pidinfo returns the bytes
// written, and 0 with errno set when the read fails.
func (l *libSystem) taskInfo(pid int32) (process.ProcTaskInfo, error) {
	var info process.ProcTaskInfo
	size := unsafe.Sizeof(info)
	r1, _, errno := purego.SyscallN(l.procPidInfo, uintptr(pid), procPidTaskInfo, 0, uintptr(unsafe.Pointer(&info)), size)
	if n := int32(r1); n != int32(size) {
		// purego loads the 32-bit errno into a 64-bit register.
		if e := unix.Errno(uint32(errno)); n <= 0 && e != 0 {
			return process.ProcTaskInfo{}, fmt.Errorf("proc_pidinfo PROC_PIDTASKINFO: %w", e)
		}
		return process.ProcTaskInfo{}, fmt.Errorf("proc_pidinfo PROC_PIDTASKINFO: read %d of %d bytes", n, size)
	}
	return info, nil
}
