// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package pagingscraper

import "github.com/shirou/gopsutil/v4/mem"

var darwinSwapMemory = mem.SwapMemory

func getPageFileStats() ([]*pageFileStats, error) {
	swap, err := darwinSwapMemory()
	if err != nil {
		return nil, err
	}
	return []*pageFileStats{{
		deviceName: "",
		totalBytes: swap.Total,
		usedBytes:  swap.Used,
		freeBytes:  swap.Free,
	}}, nil
}
