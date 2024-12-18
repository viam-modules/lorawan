// Package gateway implements the LoRaWAN gateway functionality
package gateway

// Timing constants for Class A devices
const (
	// JoinRx2WindowSec is the delay for sending join accept message in seconds
	JoinRx2WindowSec = 6
	// Rx2WindowSec is the delay for sending other downlink messages in seconds
	Rx2WindowSec = 2
)

// Radio configuration constants
const (
	// Rx2Frequency is the frequency in Hz to send downlinks on rx2 window
	Rx2Frequency = 923300000
	// Rx2SF is the spreading factor for rx2 window
	// SF12 and bandwidth 500k corresponds with data rate 8.
	Rx2SF = 12
	// Rx2Bandwidth is the bandwidth setting (500kHz)
	Rx2Bandwidth = 0x06
)

// Message type constants
const (
	// DownLinkMType is the message type for unconfirmed downlink (0x60)
	DownLinkMType = 0x60
	// DeviceTimePort is Port 13 used for DeviceTimeAns messages
	DeviceTimePort = 0x0D
)
