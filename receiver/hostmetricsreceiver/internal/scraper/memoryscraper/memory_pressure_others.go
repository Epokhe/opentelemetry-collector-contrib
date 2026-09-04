// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package memoryscraper

func readMemoryPressure() (int64, error) {
	return 0, nil
}
