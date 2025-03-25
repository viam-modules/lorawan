package lorahw

import (
	"context"
	"testing"
	"time"

	"go.viam.com/test"
)

func TestGetRegion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Region
	}{
		{
			name:     "US region full name",
			input:    "US915",
			expected: US,
		},
		{
			name:     "US region short name",
			input:    "US",
			expected: US,
		},
		{
			name:     "US region frequency only",
			input:    "915",
			expected: US,
		},
		{
			name:     "EU region full name",
			input:    "EU868",
			expected: EU,
		},
		{
			name:     "EU region short name",
			input:    "EU",
			expected: EU,
		},
		{
			name:     "EU region frequency only",
			input:    "868",
			expected: EU,
		},
		{
			name:     "lowercase input",
			input:    "eu868",
			expected: EU,
		},
		{
			name:     "invalid region",
			input:    "INVALID",
			expected: Unspecified,
		},
		{
			name:     "empty string",
			input:    "",
			expected: Unspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRegion(tt.input)
			test.That(t, result, test.ShouldEqual, tt.expected)
		})
	}
}

func TestParseErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		errCode  int
		expected string
	}{
		{
			name:     "invalid SPI bus error",
			errCode:  1,
			expected: "invalid SPI bus",
		},
		{
			name:     "board config error",
			errCode:  2,
			expected: "error setting the board config",
		},
		{
			name:     "unknown region error",
			errCode:  3,
			expected: "unknown region",
		},
		{
			name:     "radio 0 config error",
			errCode:  4,
			expected: "error setting the radio frequency config for radio 0",
		},
		{
			name:     "radio 1 config error",
			errCode:  5,
			expected: "error setting the radio frequency config for radio 1",
		},
		{
			name:     "IF chain config error",
			errCode:  6,
			expected: "error setting the intermediate frequency chain config",
		},
		{
			name:     "lora STD channel error",
			errCode:  7,
			expected: "error configuring the lora STD channel",
		},
		{
			name:     "tx gain settings error",
			errCode:  8,
			expected: "error configuring the tx gain settings",
		},
		{
			name:     "gateway start error",
			errCode:  9,
			expected: "error starting the gateway",
		},
		{
			name:     "unknown error code",
			errCode:  999,
			expected: "unknown error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseErrorCode(tt.errCode)
			test.That(t, result, test.ShouldEqual, tt.expected)
		})
	}
}

func TestSendPacket(t *testing.T) {
	// Test nil packet
	err := SendPacket(context.Background(), nil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldEqual, "packet cannot be nil")

	// Test valid packet
	pkt := &TxPacket{
		Freq:      915000000,
		Power:     14,
		DataRate:  10,
		Bandwidth: 125,
		Size:      10,
		Payload:   []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}

	// Test with context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	err = SendPacket(ctx, pkt)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "context deadline exceeded")
}

func TestSetupAndStopGateway(t *testing.T) {
	// Test invalid region
	err := SetupGateway(0, Unspecified)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errInvalidSpiBus)

	// Test invalid SPI bus
	err = SetupGateway(999, US)
	test.That(t, err, test.ShouldNotBeNil)

	// Test stop gateway when not started
	err = StopGateway()
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldEqual, "error stopping gateway")
}
