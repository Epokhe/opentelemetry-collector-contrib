// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package pagingscraper

import "context"

// macOS exposes swap capacity but not the paging operation and fault counters
// represented by this scraper's metrics. Do not emit synthetic zero counters.
func (*pagingScraper) scrapePagingMetrics(context.Context) error {
	return nil
}
