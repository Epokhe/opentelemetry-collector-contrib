// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package memoryscraper

import (
	"context"
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	"go.opentelemetry.io/collector/scraper/scrapertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/memoryscraper/internal/metadata"
)

func TestDarwinMemoryPressureMetric(t *testing.T) {
	mbc := metadata.NewDefaultMetricsBuilderConfig()
	mbc.Metrics.SystemMemoryUsage.Enabled = false
	mbc.Metrics.SystemMemoryDarwinPressure.Enabled = true
	scraper := newMemoryScraper(t.Context(), scrapertest.NewNopSettings(metadata.Type), &Config{MetricsBuilderConfig: mbc})
	scraper.virtualMemory = func(_ context.Context) (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{Total: 1}, nil }
	scraper.memoryPressure = func() (int64, error) { return memoryPressureWarning, nil }

	require.NoError(t, scraper.start(t.Context(), componenttest.NewNopHost()))
	md, err := scraper.scrape(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, md.MetricCount())
	metric := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0)
	assert.Equal(t, "system.memory.darwin.pressure", metric.Name())
	assert.Equal(t, "1", metric.Unit())
	assert.Equal(t, pmetric.MetricTypeGauge, metric.Type())
	require.Equal(t, 1, metric.Gauge().DataPoints().Len())
	assert.Equal(t, int64(memoryPressureWarning), metric.Gauge().DataPoints().At(0).IntValue())
	assert.Equal(t, 0, metric.Gauge().DataPoints().At(0).Attributes().Len())
}

func TestDarwinMemoryPressureFailuresDoNotEmitZero(t *testing.T) {
	for _, tc := range []struct {
		name     string
		read     func() (int64, error)
		contains string
	}{
		{name: "read error", read: func() (int64, error) { return 0, errors.New("sysctl unavailable") }, contains: "sysctl unavailable"},
		{name: "unknown value", read: func() (int64, error) { return 0, nil }, contains: "unknown value 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mbc := metadata.NewDefaultMetricsBuilderConfig()
			mbc.Metrics.SystemMemoryUsage.Enabled = false
			mbc.Metrics.SystemMemoryDarwinPressure.Enabled = true
			scraper := newMemoryScraper(t.Context(), scrapertest.NewNopSettings(metadata.Type), &Config{MetricsBuilderConfig: mbc})
			scraper.virtualMemory = func(_ context.Context) (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{Total: 1}, nil }
			scraper.memoryPressure = tc.read

			require.NoError(t, scraper.start(t.Context(), componenttest.NewNopHost()))
			md, err := scraper.scrape(t.Context())
			assert.Zero(t, md.MetricCount())
			assert.ErrorContains(t, err, tc.contains)
			assert.True(t, scrapererror.IsPartialScrapeError(err))
		})
	}
}

func TestDarwinMemoryPressureReadLive(t *testing.T) {
	pressure, err := readMemoryPressure()
	require.NoError(t, err)
	assert.Contains(t, []int64{memoryPressureNormal, memoryPressureWarning, memoryPressureCritical}, pressure)
	t.Logf("kern.memorystatus_vm_pressure_level=%d", pressure)
}
