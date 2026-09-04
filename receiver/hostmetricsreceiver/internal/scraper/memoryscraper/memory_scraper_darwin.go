// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package memoryscraper

import (
	"fmt"

	"github.com/shirou/gopsutil/v4/mem"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"golang.org/x/sys/unix"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/precision"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/memoryscraper/internal/metadata"
)

const (
	memoryPressureNormal   = 1
	memoryPressureWarning  = 2
	memoryPressureCritical = 4
)

func (s *memoryScraper) recordMemoryUsageMetric(now pcommon.Timestamp, memInfo *mem.VirtualMemoryStat) {
	s.mb.RecordSystemMemoryUsageDataPoint(now, int64(memInfo.Used), metadata.AttributeStateUsed)
	s.mb.RecordSystemMemoryUsageDataPoint(now, int64(memInfo.Free), metadata.AttributeStateFree)
	s.mb.RecordSystemMemoryUsageDataPoint(now, int64(memInfo.Inactive), metadata.AttributeStateInactive)
}

func (s *memoryScraper) recordMemoryUtilizationMetric(now pcommon.Timestamp, memInfo *mem.VirtualMemoryStat) {
	s.mb.RecordSystemMemoryUtilizationDataPoint(now, precision.Ratio(memInfo.Used, memInfo.Total), metadata.AttributeStateUsed)
	s.mb.RecordSystemMemoryUtilizationDataPoint(now, precision.Ratio(memInfo.Free, memInfo.Total), metadata.AttributeStateFree)
	s.mb.RecordSystemMemoryUtilizationDataPoint(now, precision.Ratio(memInfo.Inactive, memInfo.Total), metadata.AttributeStateInactive)
}

func (s *memoryScraper) recordSystemSpecificMetrics(now pcommon.Timestamp, _ *mem.VirtualMemoryStat) error {
	if !s.config.MetricsBuilderConfig.Metrics.SystemMemoryDarwinPressure.Enabled {
		return nil
	}
	pressure, err := s.memoryPressure()
	if err != nil {
		return fmt.Errorf("read macOS memory pressure: %w", err)
	}
	if pressure != memoryPressureNormal && pressure != memoryPressureWarning && pressure != memoryPressureCritical {
		return fmt.Errorf("read macOS memory pressure: unknown value %d", pressure)
	}
	s.mb.RecordSystemMemoryDarwinPressureDataPoint(now, pressure)
	return nil
}

func readMemoryPressure() (int64, error) {
	pressure, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if err != nil {
		return 0, err
	}
	return int64(pressure), nil
}
