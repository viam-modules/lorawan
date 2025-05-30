package main

import (
	"context"
	"os"
	"testing"

	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/regions"
	v1 "go.viam.com/api/common/v1"
	"go.viam.com/test"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestParseAndValidateArguments(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tests := []struct {
		name           string
		args           []string
		expectedConfig concentratorConfig
	}{
		{
			name: "default values",
			args: []string{"cmd"},
			expectedConfig: concentratorConfig{
				comType: 1,
				path:    "/dev/ttyACM0",
				region:  regions.Region(1),
			},
		},
		{
			name: "custom values",
			args: []string{
				"cmd",
				"-comType=0",
				"-path=/dev/spidev0.0",
				"-region=2",
			},
			expectedConfig: concentratorConfig{
				comType: 0,
				path:    "/dev/spidev0.0",
				region:  regions.Region(2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			config := parseAndValidateArguments()
			test.That(t, config.comType, test.ShouldEqual, tt.expectedConfig.comType)
			test.That(t, config.path, test.ShouldEqual, tt.expectedConfig.path)
			test.That(t, config.region, test.ShouldEqual, tt.expectedConfig.region)
		})
	}
}

func TestConvertToTxPacket(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]interface{}
		expected    *lorahw.TxPacket
		expectError bool
	}{
		{
			name: "valid packet",
			input: map[string]interface{}{
				"Freq":      915000000,
				"DataRate":  7,
				"Bandwidth": 1,
				"Size":      3,
				"Payload":   []byte{0x01, 0x02, 0x03},
			},
			expected: &lorahw.TxPacket{
				Freq:      915000000,
				DataRate:  7,
				Bandwidth: 1,
				Size:      3,
				Payload:   []byte{0x01, 0x02, 0x03},
			},
			expectError: false,
		},
		{
			name: "invalid type",
			input: map[string]interface{}{
				"Freq": "invalid", // Should be number
			},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertToTxPacket(tt.input)
			if tt.expectError {
				test.That(t, err, test.ShouldNotBeNil)
				return
			}
			test.That(t, err, test.ShouldBeNil)
			test.That(t, result.Freq, test.ShouldEqual, tt.expected.Freq)
			test.That(t, result.DataRate, test.ShouldEqual, tt.expected.DataRate)
			test.That(t, result.Bandwidth, test.ShouldEqual, tt.expected.Bandwidth)
			test.That(t, result.Size, test.ShouldEqual, tt.expected.Size)
			test.That(t, result.Payload, test.ShouldResemble, tt.expected.Payload)
		})
	}
}

func TestSensorServiceDoCommand(t *testing.T) {
	s := sensorService{}
	ctx := context.Background()

	t.Run("get_packets command", func(t *testing.T) {
		cmd := map[string]interface{}{
			gateway.GetPacketsKey: true,
		}
		pbCmd, err := structpb.NewStruct(cmd)
		test.That(t, err, test.ShouldBeNil)

		req := &v1.DoCommandRequest{
			Command: pbCmd,
		}

		resp, err := s.DoCommand(ctx, req)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, resp, test.ShouldNotBeNil)

		result := resp.GetResult().AsMap()
		packets, ok := result["packets"]
		test.That(t, ok, test.ShouldBeTrue)
		// test packet should be returned
		test.That(t, packets, test.ShouldNotBeNil)
	})

	t.Run("send_packet command", func(t *testing.T) {
		packet := map[string]interface{}{
			"Freq":      915000000,
			"DataRate":  7,
			"Bandwidth": 1,
			"Size":      3,
			"Payload":   []byte{0x01, 0x02, 0x03},
		}
		cmd := map[string]interface{}{
			gateway.SendPacketKey: packet,
		}
		pbCmd, err := structpb.NewStruct(cmd)
		test.That(t, err, test.ShouldBeNil)

		req := &v1.DoCommandRequest{
			Command: pbCmd,
		}

		resp, err := s.DoCommand(ctx, req)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, resp, test.ShouldNotBeNil)
	})

	t.Run("stop command", func(t *testing.T) {
		cmd := map[string]interface{}{
			gateway.StopKey: true,
		}
		pbCmd, err := structpb.NewStruct(cmd)
		test.That(t, err, test.ShouldBeNil)

		req := &v1.DoCommandRequest{
			Command: pbCmd,
		}

		resp, err := s.DoCommand(ctx, req)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, resp, test.ShouldNotBeNil)
	})
}
