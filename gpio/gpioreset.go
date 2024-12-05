// Package gpio sets the gateway's gpio pins
package gpio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
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
func InitGateway(b board.Board, resetPin, powerPin *int, bookworm bool) error {
	rst := strconv.Itoa(*resetPin)
	var pwr string
	var pwrPin board.GPIOPin
	var err error
	if powerPin != nil {
		pwr = strconv.Itoa(*powerPin)
		pwrPin, err = b.GPIOPinByName(pwr)
		if err != nil {
			return err
		}
	}

	rstPin, err := b.GPIOPinByName(rst)
	if err != nil {
		return err
	}
	err = initGPIO(b, rstPin, pwrPin, bookworm)
	if err != nil {
		return err
	}
	return ResetGPIO(b, rstPin)
}

func initGPIO(b board.Board, resetPin, powerPin board.GPIOPin, bookworm bool) error {
	err := resetPin.Set(context.Background(), true, nil)
	if err != nil {
		return err
	}

	powerPin.Set(context.Background(), true, nil)
	if err != nil {
		return err
	}

	return nil
}

// ResetGPIO resets the gateway.
func ResetGPIO(b board.Board, rst board.GPIOPin) error {
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
