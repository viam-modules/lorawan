//go:build tools
// +build tools

package tools

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)

// This file is used for build-time dependencies only and does not contribute to the actual application.
