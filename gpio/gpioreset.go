package gpio

import (
	"fmt"
	"os/exec"
	"time"
)

const (
	SX1302ResetPin   = "23" // SX1302 reset
	SX1302PowerEnPin = "18" // SX1302 power enable
	SX1261ResetPin   = "22" // SX1261 reset (LBT / Spectral Scan)
	AD5338RResetPin  = "13" // AD5338R reset (full-duplex CN490 reference design)
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
	pinctrlSet(SX1261ResetPin, "op")
	waitGPIO()
	pinctrlSet(SX1302PowerEnPin, "op")
	waitGPIO()
	pinctrlSet(AD5338RResetPin, "op")
	waitGPIO()
}

func ResetGPIO() {
	fmt.Printf("CoreCell reset through GPIO%s...\n", SX1302ResetPin)
	fmt.Printf("SX1261 reset through GPIO%s...\n", SX1261ResetPin)
	fmt.Printf("CoreCell power enable through GPIO%s...\n", SX1302PowerEnPin)
	fmt.Printf("CoreCell ADC reset through GPIO%s...\n", AD5338RResetPin)

	// Write output for SX1302 CoreCell power_enable and reset
	pinctrlSet(SX1302PowerEnPin, "dh")
	waitGPIO()

	pinctrlSet(SX1302ResetPin, "dh")
	waitGPIO()
	pinctrlSet(SX1302ResetPin, "dl")
	waitGPIO()

	pinctrlSet(SX1261ResetPin, "dl")
	waitGPIO()
	pinctrlSet(SX1261ResetPin, "dh")
	waitGPIO()

	pinctrlSet(AD5338RResetPin, "dl")
	waitGPIO()
	pinctrlSet(AD5338RResetPin, "dh")
	waitGPIO()
}
