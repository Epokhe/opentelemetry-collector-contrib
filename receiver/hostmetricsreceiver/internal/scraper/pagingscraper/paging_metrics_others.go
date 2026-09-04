// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin

package pagingscraper

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/pagingscraper/internal/metadata"
)

func (s *pagingScraper) scrapePagingMetrics(ctx context.Context) error {
	now := pcommon.NewTimestampFromTime(time.Now())
	swap, err := s.swapMemory(ctx)
	if err != nil {
		return fmt.Errorf("failed to read swap info: %w", err)
	}
	s.recordPagingOperationsDataPoints(now, swap)
	s.recordPageFaultsDataPoints(now, swap)
	return nil
}

func (s *pagingScraper) recordPagingOperationsDataPoints(now pcommon.Timestamp, swap *mem.SwapMemoryStat) {
	s.mb.RecordSystemPagingOperationsDataPoint(now, int64(swap.Sin), metadata.AttributeDirectionPageIn, metadata.AttributeTypeMajor)
	s.mb.RecordSystemPagingOperationsDataPoint(now, int64(swap.Sout), metadata.AttributeDirectionPageOut, metadata.AttributeTypeMajor)
	s.mb.RecordSystemPagingOperationsDataPoint(now, int64(swap.PgIn), metadata.AttributeDirectionPageIn, metadata.AttributeTypeMinor)
	s.mb.RecordSystemPagingOperationsDataPoint(now, int64(swap.PgOut), metadata.AttributeDirectionPageOut, metadata.AttributeTypeMinor)
}

func (s *pagingScraper) recordPageFaultsDataPoints(now pcommon.Timestamp, swap *mem.SwapMemoryStat) {
	s.mb.RecordSystemPagingFaultsDataPoint(now, int64(swap.PgMajFault), metadata.AttributeTypeMajor)
	s.mb.RecordSystemPagingFaultsDataPoint(now, int64(swap.PgFault-swap.PgMajFault), metadata.AttributeTypeMinor)
}
