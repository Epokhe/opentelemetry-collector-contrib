// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package processscraper

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	"go.opentelemetry.io/collector/scraper/scrapertest"
	"golang.org/x/sys/unix"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/processscraper/internal/metadata"
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

// Task info is read while listing, so gopsutil's later read of the same
// process sees at least as much CPU time.
func TestGetGopsutilProcessHandles_TaskInfoMatchesGopsutil(t *testing.T) {
	handles, err := getGopsutilProcessHandles(t.Context())
	require.NoError(t, err)

	for i := 0; i < handles.Len(); i++ {
		if handles.Pid(i) != int32(os.Getpid()) {
			continue
		}
		times, err := handles.At(i).TimesWithContext(t.Context())
		require.NoError(t, err)
		memory, err := handles.At(i).MemoryInfoWithContext(t.Context())
		require.NoError(t, err)

		want, err := process.NewProcessWithContext(t.Context(), handles.Pid(i))
		require.NoError(t, err)
		wantTimes, err := want.TimesWithContext(t.Context())
		require.NoError(t, err)
		wantMemory, err := want.MemoryInfoWithContext(t.Context())
		require.NoError(t, err)

		assert.Positive(t, times.User+times.System)
		assert.LessOrEqual(t, times.User+times.System, wantTimes.User+wantTimes.System)
		assert.InDelta(t, wantTimes.User+wantTimes.System, times.User+times.System, 1)
		assert.Positive(t, memory.RSS)
		assert.InEpsilon(t, wantMemory.RSS, memory.RSS, 0.5)
		return
	}
	t.Fatal("the test process is not listed")
}

// A process macOS refuses (EPERM) or no longer has (ESRCH) is not listed;
// as a non-root user, that is every process of another effective uid.
func TestGetGopsutilProcessHandles_SkipsUnreadableTaskInfo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every process")
	}
	handles, err := getGopsutilProcessHandles(t.Context())
	require.NoError(t, err)

	listed := make(map[int32]bool, handles.Len())
	for i := 0; i < handles.Len(); i++ {
		listed[handles.Pid(i)] = true
		handle := handles.At(i).(*darwinProcessHandle)
		assert.False(t, errors.Is(handle.taskInfoErr, unix.EPERM) || errors.Is(handle.taskInfoErr, unix.ESRCH),
			"pid %d listed with %v", handles.Pid(i), handle.taskInfoErr)
	}
	assert.False(t, listed[1], "launchd, owned by root, is listed")
}

// Any other task info failure is a scrape error, and the process emits no
// metrics.
func TestScrapeMetrics_TaskInfoFailureIsReportedAndEmitsNothing(t *testing.T) {
	metricsBuilderConfig := metadata.NewDefaultMetricsBuilderConfig()
	scraper, err := newProcessScraper(scrapertest.NewNopSettings(metadata.Type), &Config{MetricsBuilderConfig: metricsBuilderConfig})
	require.NoError(t, err)
	require.NoError(t, scraper.start(t.Context(), componenttest.NewNopHost()))

	readable := darwinProcessHandle{
		wrappedProcessHandle: wrappedProcessHandle{Process: &process.Process{Pid: 10}},
		comm:                 "readable",
		taskInfo:             process.ProcTaskInfo{Total_user: 3e9, Resident_size: 4096},
		timescale:            1,
	}
	failing := darwinProcessHandle{
		wrappedProcessHandle: wrappedProcessHandle{Process: &process.Process{Pid: 11}},
		comm:                 "failing",
		taskInfoErr:          unix.EIO,
	}
	scraper.getProcessHandles = func(context.Context) (processHandles, error) {
		return &darwinProcessHandles{handles: []darwinProcessHandle{readable, failing}}, nil
	}

	md, err := scraper.scrape(t.Context())
	var partial scrapererror.PartialScrapeError
	require.ErrorAs(t, err, &partial)
	assert.ErrorContains(t, err, `error reading cpu times for process "failing" (pid 11): input/output error`)
	assert.ErrorContains(t, err, `error reading memory info for process "failing" (pid 11): input/output error`)

	require.Equal(t, 1, md.ResourceMetrics().Len())
	pid, ok := md.ResourceMetrics().At(0).Resource().Attributes().Get("process.pid")
	require.True(t, ok)
	assert.Equal(t, int64(10), pid.Int())
}
