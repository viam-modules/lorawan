package lorahw

import (
	"context"
	"errors"
	"testing"

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
			name:     "radio 0 config error",
			errCode:  3,
			expected: errRadio0Config,
		},
		{
			name:     "radio 1 config error",
			errCode:  4,
			expected: errRadio1Config,
		},
		{
			name:     "IF chain config error",
			errCode:  5,
			expected: errIntermediateFreqConfig,
		},
		{
			name:     "lora STD channel error",
			errCode:  6,
			expected: errLoraStdChannel,
		},
		{
			name:     "tx gain settings error",
			errCode:  7,
			expected: errTxGainSettings,
		},
		{
			name:     "gateway start error",
			errCode:  8,
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
		DataRate:  10,
		Bandwidth: 125,
		Size:      10,
		Payload:   []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}

	err = SendPacket(context.Background(), pkt)
	test.That(t, err, test.ShouldBeNil)
}

func TestReceivePackets(t *testing.T) {
	// should return go packet struct
	pkts, err := ReceivePackets()
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(pkts), test.ShouldEqual, 1)
	// Test that has same values as mock function in gateway.c
	test.That(t, pkts[0].Size, test.ShouldEqual, 3)
	test.That(t, pkts[0].Payload, test.ShouldResemble, []byte{0x01, 0x02, 0x03})
	test.That(t, pkts[0].SNR, test.ShouldEqual, 20)
	test.That(t, pkts[0].DataRate, test.ShouldEqual, 7)
}

func TestSetupGateway(t *testing.T) {
	// Test successful case
	err := SetupGateway(1, US)
	test.That(t, err, test.ShouldBeNil)

	// unspecifed region will not error
	err = SetupGateway(1, Unspecified)
	test.That(t, err, test.ShouldBeNil)

	// Test invalid SPI bus
	err = SetupGateway(999, US)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, errors.Is(err, errInvalidSpiBus), test.ShouldBeTrue)
}
