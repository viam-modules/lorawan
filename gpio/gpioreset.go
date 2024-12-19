// Package gpio sets the gateway's gpio pins
package gpio

import (
	"context"
	"time"

	"go.viam.com/rdk/components/board"
)

// it is neccessary to sleep betweeen setting the gpio pins - the gateway will not initislize correctly without it.
func waitGPIO() {
	time.Sleep(100 * time.Millisecond)
}

func ResetGateway(ctx context.Context, rst, pwr board.GPIOPin) error {
	if pwr != nil {
		err := pwr.Set(ctx, true, nil)
		if err != nil {
			return err
		}

		waitGPIO()
	}

	err := rst.Set(ctx, true, nil)
	if err != nil {
		return err
	}

	waitGPIO()

	err = rst.Set(ctx, false, nil)
	if err != nil {
		return err
	}

	waitGPIO()
	return nil
}
