package lorahw

import (
	"context"
	"errors"
	"testing"

	"github.com/viam-modules/gateway/regions"
	"go.viam.com/test"
)

func TestParseErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		errCode  int
		expected error
	}{
		{
			name:     "base channel",
			errCode:  1,
			expected: errInvalidBaseChannel,
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
	testPath := "/dev/spidev0.0"
	// Test successful case
	err := SetupGateway(1, testPath, regions.US, 0)
	test.That(t, err, test.ShouldBeNil)

	// unspecifed region will not error
	err = SetupGateway(1, testPath, regions.Unspecified, 16)
	test.That(t, err, test.ShouldBeNil)

	// invalid basechannel
	err = SetupGateway(1, testPath, regions.Unspecified, 100)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, errInvalidBaseChannel.Error())
}
