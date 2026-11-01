// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package processscraper // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/processscraper"

import (
	"context"
	"path/filepath"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

// getGopsutilProcessHandles lists processes with one kern.proc.all sysctl and
// keeps each process's name, start time and parent pid from that list.
// gopsutil's ProcessesWithContext reads the same list, then reads every
// process again with kern.proc.pid for its start time, and NameWithContext
// reads it once more.
func getGopsutilProcessHandles(context.Context) (processHandles, error) {
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
		handles = append(handles, darwinProcessHandle{
			wrappedProcessHandle: wrappedProcessHandle{
				Process:   &process.Process{Pid: k.Proc.P_pid},
				parentPid: k.Eproc.Ppid,
				flags:     flagParentPidSet,
			},
			comm:       unix.ByteSliceToString(k.Proc.P_comm[:]),
			createTime: k.Proc.P_starttime.Sec*1000 + int64(k.Proc.P_starttime.Usec)/1000,
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
	comm       string
	createTime int64
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
