package gpio

import (
	"fmt"
	"os/exec"
	"time"
)

const (
	SX1302ResetPin   = "23" // SX1302 reset
	SX1302PowerEnPin = "18" // SX1302 power enable
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

func InitGPIO() {
	// Set GPIOs as output
	pinctrlSet(SX1302ResetPin, "op")
	waitGPIO()
	pinctrlSet(SX1302PowerEnPin, "op")
	waitGPIO()
}

func ResetGPIO() {
	pinctrlSet(SX1302ResetPin, "dh")
	waitGPIO()
	pinctrlSet(SX1302ResetPin, "dl")
	waitGPIO()

}
