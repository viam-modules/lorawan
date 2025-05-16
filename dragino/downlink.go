// Package dragino provides shared functionality for Dragino LoRaWAN sensors
package dragino

import (
	"context"

	"github.com/viam-modules/gateway/node"
)

// Common constants for Dragino sensors.
const (
	ResetHeader    = "04"
	ResetPayload   = "FFFF"
	IntervalHeader = "01"
	IntervalBytes  = 3
)

// DraginoResetRequest defines the reset downlink for draginos.
var DraginoResetRequest = node.ResetRequest{
	Header:     ResetHeader,
	PayloadHex: ResetPayload,
}

// CreateIntervalDownlinkRequest sends an interval update command to a Dragino sensor.
func CreateIntervalDownlinkRequest(ctx context.Context, intervalMin float64, testOnly bool) node.IntervalRequest {
	return node.IntervalRequest{
		IntervalMin:  intervalMin,
		PayloadUnits: node.Seconds,
		Header:       IntervalHeader,
		NumBytes:     IntervalBytes,
		TestOnly:     testOnly,
	}
}
