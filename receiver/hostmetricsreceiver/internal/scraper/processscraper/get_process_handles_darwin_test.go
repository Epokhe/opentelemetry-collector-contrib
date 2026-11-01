// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package processscraper

import (
	"errors"
	"os"
	"testing"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// The handles serve name, start time and parent pid from the process list;
// gopsutil reads each from the process itself. Both must agree for every
// process on this host that did not exit or exec after the list was taken.
func TestGetGopsutilProcessHandles_MatchesGopsutil(t *testing.T) {
	handles, err := getGopsutilProcessHandles(t.Context())
	require.NoError(t, err)
	require.Positive(t, handles.Len())

	self := false
	compared := 0
	for i := 0; i < handles.Len(); i++ {
		pid := handles.Pid(i)
		handle := handles.At(i)
		self = self || pid == int32(os.Getpid())

		createTime, err := handle.CreateTimeWithContext(t.Context())
		require.NoError(t, err)
		name, nameErr := handle.NameWithContext(t.Context())
		ppid, err := handle.PpidWithContext(t.Context())
		require.NoError(t, err)

		want, err := process.NewProcessWithContext(t.Context(), pid)
		if errors.Is(err, process.ErrorProcessNotRunning) {
			continue
		}
		require.NoError(t, err)
		wantCreateTime, err := want.CreateTimeWithContext(t.Context())
		require.NoError(t, err)
		wantName, wantNameErr := want.NameWithContext(t.Context())
		wantPpid, err := want.PpidWithContext(t.Context())
		require.NoError(t, err)

		if wantCreateTime == createTime && wantName == name && (wantNameErr == nil) == (nameErr == nil) && wantPpid == ppid {
			compared++
			continue
		}
		// A process that exits, execs or is reparented after the list was taken
		// legitimately differs from gopsutil's later read.
		if current, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid)); err != nil ||
			unix.ByteSliceToString(current.Proc.P_comm[:]) != handle.(*darwinProcessHandle).comm ||
			current.Eproc.Ppid != ppid {
			continue
		}
		assert.Equal(t, wantCreateTime, createTime, "create time of pid %d", pid)
		assert.Equal(t, wantNameErr == nil, nameErr == nil, "name error of pid %d: want %v, got %v", pid, wantNameErr, nameErr)
		assert.Equal(t, wantName, name, "name of pid %d", pid)
		assert.Equal(t, wantPpid, ppid, "parent pid of pid %d", pid)
	}
	assert.True(t, self, "the test process is listed")
	assert.Greater(t, compared, handles.Len()/2, "most processes are compared")
}

func TestGetGopsutilProcessHandles_SkipsKernelTask(t *testing.T) {
	handles, err := getGopsutilProcessHandles(t.Context())
	require.NoError(t, err)
	for i := 0; i < handles.Len(); i++ {
		assert.Positive(t, handles.Pid(i))
	}
}
