package gateway

import (
	"fmt"
	"testing"

	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestCreateDownLink(t *testing.T) {
	tests := []struct {
		name           string
		device         *node.Node
		framePayload   []byte
		expectedErr    bool
		ack            bool
		expectedLength int
	}{
		{
			name: "valid downlink with standard payload",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
			},
			framePayload:   []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:    false,
			ack:            false,
			expectedLength: 17,
		},
		{
			name: "valid downlink with an ACK",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
			},
			framePayload:   []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:    false,
			ack:            true,
			expectedLength: 17,
		},
		{
			name: "downlink with only ACK should send",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
			},
			expectedErr:    false,
			ack:            true,
			expectedLength: 12,
		},
		{
			name: "invalid fport should return error",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x00,
				DevEui:   testDevEUI,
			},
			framePayload: []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:  true,
			ack:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary data file for testing
			testFile := createDataFile(t)

			// Set up the gateway for testing
			g := &gateway{
				logger:   logging.NewTestLogger(t),
				dataFile: testFile,
			}

			// Store initial FCntDown for verification later
			initialFCntDown := tt.device.FCntDown

			payload, err := g.createDownlink(tt.device, tt.framePayload, tt.ack)

			if tt.expectedErr {
				test.That(t, err, test.ShouldNotBeNil)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, payload, test.ShouldNotBeNil)

				// Check packet structure
				test.That(t, len(payload), test.ShouldEqual, tt.expectedLength)
				test.That(t, payload[0], test.ShouldEqual, byte(0x60)) // Unconfirmed data down

				// DevAddr should be in little-endian format in the packet
				devAddrLE := reverseByteArray(tt.device.Addr)
				test.That(t, payload[1:5], test.ShouldResemble, devAddrLE)

				expectedFctrl := 0x0
				if tt.ack {
					expectedFctrl = 0x20
				}
				test.That(t, payload[5], test.ShouldEqual, expectedFctrl)

				if tt.framePayload != nil {
					test.That(t, payload[8], test.ShouldEqual, tt.device.FPort)
				}

				// Verify MIC (last 4 bytes)
				micBytes := payload[len(payload)-4:]
				payloadWithoutMIC := payload[:len(payload)-4]
				expectedMIC, err := crypto.ComputeLegacyDownlinkMIC(
					types.AES128Key(tt.device.NwkSKey),
					*types.MustDevAddr(tt.device.Addr),
					initialFCntDown+1,
					payloadWithoutMIC,
				)
				test.That(t, err, test.ShouldBeNil)
				test.That(t, micBytes, test.ShouldResemble, expectedMIC[:])

				// Verify FCntDown was incremented
				test.That(t, tt.device.FCntDown, test.ShouldEqual, initialFCntDown+1)

				deviceInfoList, err := readFromFile(g.dataFile)
				test.That(t, err, test.ShouldBeNil)

				found := false
				for _, di := range deviceInfoList {
					if di.DevEUI == fmt.Sprintf("%X", tt.device.DevEui) {
						found = true
						test.That(t, di.FCntDown, test.ShouldEqual, tt.device.FCntDown)
						test.That(t, di.DevAddr, test.ShouldEqual, fmt.Sprintf("%X", tt.device.Addr))
						break
					}
				}
				test.That(t, found, test.ShouldBeTrue)
			}
		})
	}
}
