// Package gpio sets the gateway's gpio pins
package gpio

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"go.viam.com/rdk/components/board"
)

func waitGPIO() {
	time.Sleep(100 * time.Millisecond)
}

func pinctrlSet(pin, state string, bookworm bool) error {
	if bookworm {
		cmd := exec.Command("pinctrl", "set", pin, state)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error setting GPIO %s to %s: %w", pin, state, err)
		}
	} else {
		cmd := exec.Command("raspi-gpio", "set", pin, state)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error setting GPIO %s to %s: %w", pin, state, err)
		}
	}
	return nil
}

// InitGateway initializes the gateway hardware.
func InitGatewaycontext(ctx context.Context, resetPin, powerPin board.GPIOPin) error {
	err := initGPIO(ctx, resetPin, powerPin)
	if err != nil {
		return err
	}
	return ResetGPIO(resetPin)
}

func initGPIO(ctx context.Context, resetPin, powerPin board.GPIOPin) error {
	err := resetPin.Set(context.Background(), true, nil)
	if err != nil {
		return err
	}

	powerPin.Set(ctx, true, nil)
	if err != nil {
		return err
	}

	return nil
}

// ResetGPIO resets the gateway.
func ResetGPIO(rst board.GPIOPin) error {
	err := rst.Set(context.Background(), true, nil)
	if err != nil {
		return err
	}

	waitGPIO()

	err = rst.Set(context.Background(), false, nil)
	if err != nil {
		return err
	}
	waitGPIO()
	return nil
}
