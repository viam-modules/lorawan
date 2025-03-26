package lorahw

import (
	"context"
	"errors"
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
		expected error
	}{
		{
			name:     "invalid SPI bus error",
			errCode:  1,
			expected: errInvalidSpiBus,
		},
		{
			name:     "board config error",
			errCode:  2,
			expected: errBoardConfig,
		},
		{
			name:     "unknown region error",
			errCode:  3,
			expected: errUnknownRegion,
		},
		{
			name:     "radio 0 config error",
			errCode:  4,
			expected: errRadio0Config,
		},
		{
			name:     "radio 1 config error",
			errCode:  5,
			expected: errRadio1Config,
		},
		{
			name:     "IF chain config error",
			errCode:  6,
			expected: errIntermediateFreqConfig,
		},
		{
			name:     "lora STD channel error",
			errCode:  7,
			expected: errLoraStdChannel,
		},
		{
			name:     "tx gain settings error",
			errCode:  8,
			expected: errTxGainSettings,
		},
		{
			name:     "gateway start error",
			errCode:  9,
			expected: errGatewayStart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseErrorCode(tt.errCode)
			test.That(t, errors.Is(err, tt.expected), test.ShouldBeTrue)
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

func TestSetupGateway(t *testing.T) {
	// Test successful case
	err := SetupGateway(1, US)
	test.That(t, err, test.ShouldBeNil)

	// Test invalid region
	err = SetupGateway(0, Unspecified)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, errors.Is(err, errUnknownRegion), test.ShouldBeTrue)

	// Test invalid SPI bus
	err = SetupGateway(999, US)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, errors.Is(err, errInvalidSpiBus), test.ShouldBeTrue)
}
