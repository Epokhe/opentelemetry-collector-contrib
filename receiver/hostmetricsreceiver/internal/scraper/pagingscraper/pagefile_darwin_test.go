// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package pagingscraper

import (
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDarwinPageFileStatsUsesSwapMemory(t *testing.T) {
	original := darwinSwapMemory
	t.Cleanup(func() { darwinSwapMemory = original })
	darwinSwapMemory = func() (*mem.SwapMemoryStat, error) {
		return &mem.SwapMemoryStat{Total: 4096, Used: 1024, Free: 3072}, nil
	}

	stats, err := getPageFileStats()
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, uint64(1024), stats[0].usedBytes)
	assert.Equal(t, uint64(3072), stats[0].freeBytes)
	assert.Nil(t, stats[0].cachedBytes)
}

func TestDarwinPageFileStatsReadError(t *testing.T) {
	original := darwinSwapMemory
	t.Cleanup(func() { darwinSwapMemory = original })
	darwinSwapMemory = func() (*mem.SwapMemoryStat, error) { return nil, errors.New("swap unavailable") }

	stats, err := getPageFileStats()
	assert.Nil(t, stats)
	assert.EqualError(t, err, "swap unavailable")
}

func TestDarwinPageFileStatsReadLive(t *testing.T) {
	stats, err := getPageFileStats()
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, stats[0].totalBytes, stats[0].usedBytes+stats[0].freeBytes)
	t.Logf("swap total=%d used=%d free=%d", stats[0].totalBytes, stats[0].usedBytes, stats[0].freeBytes)
}
