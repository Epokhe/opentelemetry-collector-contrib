// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || freebsd

package processscraper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/scraper/scrapertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/processscraper/internal/metadata"
)

func TestScrapeMetrics_ExecutableReaderOnlyRunsForEnabledPathAttribute(t *testing.T) {
	for _, test := range []struct {
		name      string
		path      bool
		wantCalls int
	}{
		{name: "executable path disabled", wantCalls: 0},
		{name: "executable path enabled", path: true, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			metricsBuilderConfig := metadata.NewDefaultMetricsBuilderConfig()
			metricsBuilderConfig.ResourceAttributes.ProcessExecutablePath.Enabled = test.path
			// The command attributes read the command line too; keep them off so
			// every CmdlineWithContext call is an executable read.
			metricsBuilderConfig.ResourceAttributes.ProcessCommand.Enabled = false
			metricsBuilderConfig.ResourceAttributes.ProcessCommandLine.Enabled = false

			scraper, err := newProcessScraper(scrapertest.NewNopSettings(metadata.Type), &Config{MetricsBuilderConfig: metricsBuilderConfig})
			require.NoError(t, err)
			require.NoError(t, scraper.start(t.Context(), componenttest.NewNopHost()))

			handleMock := &processHandleMock{}
			handleMock.On("NameWithContext", mock.Anything).Return("test", nil)
			handleMock.On("CmdlineWithContext", mock.Anything).Return("/usr/bin/test --flag", nil)
			handleMock.On("CreateTimeWithContext", mock.Anything).Return(time.Now().UnixMilli(), nil)
			initDefaultsHandleMock(t, handleMock)
			scraper.getProcessHandles = func(context.Context) (processHandles, error) {
				return &processHandlesMock{handles: []*processHandleMock{handleMock}}, nil
			}

			md, err := scraper.scrape(t.Context())
			require.NoError(t, err)
			handleMock.AssertNumberOfCalls(t, "CmdlineWithContext", test.wantCalls)

			attrs := md.ResourceMetrics().At(0).Resource().Attributes()
			name, ok := attrs.Get("process.executable.name")
			require.True(t, ok)
			assert.Equal(t, "test", name.Str())
			path, ok := attrs.Get("process.executable.path")
			assert.Equal(t, test.path, ok)
			if test.path {
				assert.Equal(t, "/usr/bin/test", path.Str())
			}
		})
	}
}
