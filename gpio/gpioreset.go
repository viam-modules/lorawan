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

func pinctrlSet(pin string, state string) error {
	cmd := exec.Command("pinctrl", "set", pin, state)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	
	return fmt.Errorf("Error setting GPIO %s to %s: %v output: %v", pin, state, err, string(output))
}

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
	return resetGPIO(rst)
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
