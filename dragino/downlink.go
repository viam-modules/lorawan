// Package dragino provides shared functionality for Dragino LoRaWAN sensors
package dragino

import (
	"context"

	"github.com/viam-modules/gateway/node"
)

// Common constants for Dragino sensors
const (
	ResetHeader    = "04"
	ResetPayload   = "FFFF"
	IntervalHeader = "01"
	IntervalBytes  = 3
)

// SendResetDownlink sends a reset command to a Dragino sensor
func SendResetDownlink(ctx context.Context, n *node.Node, testOnly bool) (map[string]interface{}, error) {
	return n.SendResetDownlink(ctx, node.ResetRequest{
		Header:     ResetHeader,
		PayloadHex: ResetPayload,
		TestOnly:   testOnly,
	})
}

// SendIntervalDownlink sends an interval update command to a Dragino sensor
func SendIntervalDownlink(ctx context.Context, n *node.Node, intervalMin float64, testOnly bool) (map[string]interface{}, error) {
	return n.SendIntervalDownlink(ctx, node.IntervalRequest{
		IntervalMin:  intervalMin,
		PayloadUnits: node.Seconds,
		Header:       IntervalHeader,
		NumBytes:     IntervalBytes,
		TestOnly:     testOnly,
	})
}
