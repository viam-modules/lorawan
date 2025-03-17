package gateway

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

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
		uplinkFopts    []byte
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
			uplinkFopts:    nil,
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
			uplinkFopts:    nil,
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
			uplinkFopts:    nil,
			expectedLength: 12,
		},
		{
			name: "downlink with device time request",
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
			uplinkFopts:    []byte{deviceTimeCID},
			expectedErr:    false,
			ack:            true,
			expectedLength: 23, // Base length (17) + device time response (6 bytes)
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
			uplinkFopts:  nil,
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

			payload, err := g.createDownlink(tt.device, tt.framePayload, tt.ack, tt.uplinkFopts)

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

				// Check for FOpts length in FCtrl if device time request is included
				if tt.uplinkFopts != nil && len(tt.uplinkFopts) > 0 && tt.uplinkFopts[0] == deviceTimeCID {
					// The FCtrl should include FOpts length (6) in the lower 4 bits and ACK bit if set
					expectedFctrl = expectedFctrl | 0x06 // fopts length is 6 bytes
					test.That(t, payload[5], test.ShouldEqual, byte(expectedFctrl))

					// Check that the device time MAC command is included in FOpts
					// FOpts starts after FCtrl and FCnt (8 bytes into the packet)
					test.That(t, payload[8], test.ShouldEqual, deviceTimeCID)

					// Verify that the time bytes are included (4 bytes for seconds since GPS epoch)
					// We don't check exact values since time will vary, but structure should be correct
					test.That(t, len(payload[8:14]), test.ShouldEqual, 6) // 1 for CID, 4 for seconds, 1 for fractional
				} else {
					fmt.Println(payload)
					test.That(t, payload[5], test.ShouldEqual, byte(expectedFctrl))
				}

				if tt.framePayload != nil {
					// If we have FOpts, port is after that
					portIndex := 8
					if tt.uplinkFopts != nil && len(tt.uplinkFopts) > 0 && tt.uplinkFopts[0] == deviceTimeCID {
						portIndex = 14 // 8 + 6 bytes of FOpts
					}
					test.That(t, payload[portIndex], test.ShouldEqual, tt.device.FPort)
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

func TestCreateDeviceTimeAns(t *testing.T) {
	// Call createDeviceTimeAns to get the device time answer payload
	timeAns := createDeviceTimeAns()

	// Verify the structure of the response
	test.That(t, len(timeAns), test.ShouldEqual, 6) // 1 byte CID + 4 bytes seconds + 1 byte fractional
	test.That(t, timeAns[0], test.ShouldEqual, deviceTimeCID)

	// Last byte should be 0 for fractional seconds
	test.That(t, timeAns[5], test.ShouldEqual, byte(0))

	// Extract the seconds since GPS epoch
	secondsBytes := timeAns[1:5]
	var secondsSinceEpoch uint32
	err := binary.Read(bytes.NewReader(secondsBytes), binary.LittleEndian, &secondsSinceEpoch)
	test.That(t, err, test.ShouldBeNil)

	// Calculate the expected time (roughly)
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	expectedSeconds := uint32(time.Since(gpsEpoch).Seconds())

	// The time should be reasonably close to now (within a small window)
	// We can't check exact equality since time passes between createDeviceTimeAns() and this test
	timeDiff := int64(expectedSeconds) - int64(secondsSinceEpoch)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	test.That(t, timeDiff < 5, test.ShouldBeTrue) // Should be within 5 seconds

	// Verify that the time is in the expected range (GPS epoch was in 1980)
	minExpectedSeconds := uint32(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Sub(gpsEpoch).Seconds())
	test.That(t, secondsSinceEpoch > minExpectedSeconds, test.ShouldBeTrue)
}
