// Package gpio sets the gateway's gpio pins
package gpio

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func waitGPIO() {
	time.Sleep(100 * time.Millisecond)
}

func pinctrlSet(pin, state string) error {
	cmd := exec.Command("pinctrl", "set", pin, state)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error setting GPIO %s to %s: %w", pin, state, err)
	}
	return nil
}

// InitGateway initializes the gateway hardware.
func InitGateway(resetPin, powerPin *int) error {
	rst := strconv.Itoa(*resetPin)
	var pwr string
	if powerPin != nil {
		pwr = strconv.Itoa(*powerPin)
	}
	err := initGPIO(rst, pwr)
	if err != nil {
		return err
	}
	err = resetGPIO(rst)
	if err != nil {
		return err
	}
	return nil
}

func initGPIO(resetPin, powerPin string) error {
	// Set GPIOs as output
	err := pinctrlSet(resetPin, "op")
	if err != nil {
		return err
	}
	waitGPIO()
	if powerPin != "" {
		err := pinctrlSet(powerPin, "op")
		if err != nil {
			return err
		}
		waitGPIO()
	}
	return nil
}

func resetGPIO(resetPin string) error {
	err := pinctrlSet(resetPin, "dh")
	if err != nil {
		return err
	}
	waitGPIO()
	err = pinctrlSet(resetPin, "dl")
	if err != nil {
		return err
	}
	waitGPIO()
	return nil
}
