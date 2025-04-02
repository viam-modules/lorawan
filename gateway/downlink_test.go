package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestCreateDownlink(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name                string
		device              *node.Node
		framePayload        []byte
		expectedErr         bool
		ack                 bool
		uplinkFopts         []byte
		expectedLength      int
		expectedFctrl       byte
		expectedFOptsLength int
		expectDutyCycleReq  bool
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
				Region:   regions.US,
			},
			framePayload:   []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:    false,
			ack:            false,
			uplinkFopts:    nil,
			expectedLength: 17,
			expectedFctrl:  0x80,
		},
		{
			name: "valid downlink frame payload with an ACK",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
				Region:   regions.US,
			},
			framePayload:   []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:    false,
			ack:            true,
			uplinkFopts:    nil,
			expectedLength: 17,
			expectedFctrl:  0xA0,
		},
		{
			name: "downlink with only ACK",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
				Region:   regions.US,
			},
			expectedErr:    false,
			ack:            true,
			uplinkFopts:    nil,
			expectedLength: 12,
			expectedFctrl:  0xA0,
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
				Region:   regions.US,
			},
			uplinkFopts:         []byte{deviceTimeCID},
			expectedErr:         false,
			ack:                 false,
			expectedLength:      18, // Base length (12) + device time response (6 bytes)
			expectedFctrl:       0x86,
			expectedFOptsLength: 6,
		},
		{
			name: "downlink with link check request",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
				Region:   regions.US,
			},
			framePayload:        []byte{0x01, 0x02, 0x03, 0x04},
			uplinkFopts:         []byte{linkCheckCID},
			expectedErr:         false,
			ack:                 false,
			expectedLength:      20, // Base length (17) + link check answer (3 bytes)
			expectedFctrl:       0x83,
			expectedFOptsLength: 3,
		},
		{
			name: "downlink with duty cycle request",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
				Region:   regions.EU,
			},
			framePayload:        []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:         false,
			ack:                 false,
			expectedLength:      19, // base length (17) + duty cycle request (2 bytes)
			expectedFctrl:       0x82,
			expectedFOptsLength: 2,
			expectDutyCycleReq:  true,
		},
		{
			name: "downlink with devicetimeans, linkcheckans, and ignore unknown command",
			device: &node.Node{
				NodeName: testNodeName,
				Addr:     testDeviceAddr,
				AppSKey:  testAppSKey,
				NwkSKey:  testNwkSKey,
				FCntDown: 0,
				FPort:    0x01,
				DevEui:   testDevEUI,
				Region:   regions.US,
			},
			uplinkFopts:         []byte{deviceTimeCID, linkCheckCID, 0x01},
			expectedErr:         false,
			ack:                 false,
			expectedLength:      21, // Base length (12) + link check answer (3 bytes) + device time ans (6 bytes)
			expectedFctrl:       0x89,
			expectedFOptsLength: 9,
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
				Region:   regions.US,
			},
			framePayload:  []byte{0x01, 0x02, 0x03, 0x04},
			expectedErr:   true,
			ack:           true,
			uplinkFopts:   nil,
			expectedFctrl: 0xA0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the gateway for testing
			g := &gateway{
				logger: logging.NewTestLogger(t),
			}

			dataDirectory1 := t.TempDir()
			err := g.setupSqlite(context.Background(), dataDirectory1)
			test.That(t, err, test.ShouldBeNil)

			// Store initial FCntDown for verification later
			initialFCntDown := tt.device.FCntDown

			payload, err := g.createDownlink(ctx, tt.device, tt.framePayload, tt.uplinkFopts, tt.ack, 0, 12)

			if tt.expectedErr {
				test.That(t, err, test.ShouldNotBeNil)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, payload, test.ShouldNotBeNil)

				// Check packet structure
				test.That(t, len(payload), test.ShouldEqual, tt.expectedLength)
				test.That(t, payload[0], test.ShouldEqual, unconfirmedDownLinkMHdr)
				test.That(t, payload[0], test.ShouldEqual, unconfirmedDownLinkMHdr)

				// DevAddr should be in little-endian format in the packet
				devAddrLE := reverseByteArray(tt.device.Addr)
				test.That(t, payload[1:5], test.ShouldResemble, devAddrLE)
				test.That(t, payload[5], test.ShouldEqual, tt.expectedFctrl)

				currentPos := 8 // Start after MHDR(1) + DevAddr(4) + FCtrl(1) + FCnt(2)
				if tt.expectedFOptsLength != 0 {
					//fOpts := payload[8 : 8+tt.expectedFOptsLength]
					for _, b := range tt.uplinkFopts {
						if b == deviceTimeCID {
							test.That(t, payload[currentPos], test.ShouldEqual, deviceTimeCID)
							currentPos += 6
						}
						if b == linkCheckCID {
							test.That(t, payload[currentPos], test.ShouldEqual, linkCheckCID)
							currentPos += 3
						}
					}
					if tt.expectDutyCycleReq {
						test.That(t, payload[currentPos], test.ShouldEqual, dutyCycleCID)
						currentPos += 2
					}
				}

				actualFOptsLength := currentPos - 8
				test.That(t, actualFOptsLength, test.ShouldEqual, tt.expectedFOptsLength)

				if tt.framePayload != nil {
					portIndex := currentPos
					test.That(t, payload[portIndex], test.ShouldEqual, tt.device.FPort)
				}

				// Verify MIC (last 4 bytes)
				micBytes := payload[len(payload)-4:]
				payloadWithoutMIC := payload[:len(payload)-4]
				expectedMIC, err := crypto.ComputeLegacyDownlinkMIC(
					types.AES128Key(tt.device.NwkSKey),
					*types.MustDevAddr(tt.device.Addr),
					uint32(initialFCntDown)+1,
					payloadWithoutMIC,
				)
				test.That(t, err, test.ShouldBeNil)
				test.That(t, micBytes, test.ShouldResemble, expectedMIC[:])

				// Verify FCntDown was incremented
				test.That(t, tt.device.FCntDown, test.ShouldEqual, initialFCntDown+1)

				deviceInfoList, err := g.getAllDevicesFromDB(context.Background())
				test.That(t, err, test.ShouldBeNil)

				found := false
				for _, di := range deviceInfoList {
					if di.DevEUI == fmt.Sprintf("%X", tt.device.DevEui) {
						found = true
						test.That(t, *di.FCntDown, test.ShouldEqual, tt.device.FCntDown)
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
	timeAns, err := createDeviceTimeAns()

	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(timeAns), test.ShouldEqual, 6) // 1 byte CID + 4 bytes seconds + 1 byte fractional
	test.That(t, timeAns[0], test.ShouldEqual, deviceTimeCID)

	// Last byte should be 0 for fractional seconds
	test.That(t, timeAns[5], test.ShouldEqual, byte(0))

	// Extract the seconds since GPS epoch
	secondsBytes := timeAns[1:5]
	var secondsSinceEpoch uint32
	err = binary.Read(bytes.NewReader(secondsBytes), binary.LittleEndian, &secondsSinceEpoch)
	test.That(t, err, test.ShouldBeNil)

	// Calculate the expected time (roughly)
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	expectedSeconds := uint32(time.Since(gpsEpoch).Seconds())

	// The time should be reasonably close to now (within 5 seconds)
	timeDiff := math.Abs(float64(int64(expectedSeconds) - int64(secondsSinceEpoch)))
	test.That(t, timeDiff < 5, test.ShouldBeTrue)
}

func TestCreateLinkCheckAns(t *testing.T) {
	// Test with SF12 and SNR of -5.0 dB
	snr := -5.0
	sf := 12

	linkCheckAns := createLinkCheckAns(snr, sf)

	test.That(t, len(linkCheckAns), test.ShouldEqual, 3)
	test.That(t, linkCheckAns[0], test.ShouldEqual, linkCheckCID)

	minSNR := sfToSNRMin[sf]
	expectedMargin := byte(snr - minSNR)
	test.That(t, linkCheckAns[1], test.ShouldEqual, expectedMargin)
	// Verify gateway count is always 1
	test.That(t, linkCheckAns[2], test.ShouldEqual, byte(1))
}

func TestCreateDutyCycleReq(t *testing.T) {
	req := createDutyCycleReq()
	test.That(t, len(req), test.ShouldEqual, 2)
	test.That(t, req[0], test.ShouldEqual, dutyCycleCID)
	test.That(t, req[1], test.ShouldEqual, 0x07)
}
