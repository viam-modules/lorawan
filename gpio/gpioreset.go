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

func pinctrlSet(pin string, state string) {
	cmd := exec.Command("pinctrl", "set", pin, state)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error setting GPIO %s to %s: %v\n", pin, state, err)
	}
}

func InitGateway(resetPin, powerPin *int) {
	rst := strconv.Itoa(*resetPin)
	var pwr string
	if powerPin != nil {
		pwr = strconv.Itoa(*powerPin)
	}
	initGPIO(rst, pwr)
	resetGPIO(rst)
}

func initGPIO(resetPin, powerPin string) {
	// Set GPIOs as output
	pinctrlSet(resetPin, "op")
	waitGPIO()
	if powerPin != "" {
		pinctrlSet(powerPin, "op")
		waitGPIO()
	}
}

func resetGPIO(resetPin string) {
	pinctrlSet(resetPin, "dh")
	waitGPIO()
	pinctrlSet(resetPin, "dl")
	waitGPIO()

}
