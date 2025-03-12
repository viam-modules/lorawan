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
	testFile := createDataFile(t)

	// Create a test device using existing test variables
	testDevice := &node.Node{
		NodeName: testNodeName,
		Addr:     testDeviceAddr,
		AppSKey:  testAppSKey,
		NwkSKey:  testNwkSKey,
		FCntDown: 0,
		FPort:    0x01, // Port 1
		DevEui:   testDevEUI,
	}

	// Set up the gateway for testing
	g := &gateway{
		logger:   logging.NewTestLogger(t),
		dataFile: testFile,
	}

	framePayload := []byte{0x01, 0x02, 0x03, 0x04}

	payload, err := g.createDownlink(testDevice, framePayload)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, payload, test.ShouldNotBeNil)

	test.That(t, len(payload), test.ShouldEqual, 17)       // 13 bytes + the 4 byte frame payload
	test.That(t, payload[0], test.ShouldEqual, byte(0x60)) // Unconfirmed data down

	// DevAddr should be in little-endian format in the packet
	devAddrLE := reverseByteArray(testDevice.Addr)
	test.That(t, payload[1:5], test.ShouldResemble, devAddrLE)
	test.That(t, payload[5], test.ShouldResemble, byte(0)) // fctrl value
	test.That(t, payload[8], test.ShouldEqual, testDevice.FPort)

	// Verify FCntDown was incremented
	test.That(t, testDevice.FCntDown, test.ShouldEqual, 1)

	// Verify MIC (last 4 bytes)
	micBytes := payload[len(payload)-4:]
	payloadWithoutMIC := payload[:len(payload)-4]

	expectedMIC, err := crypto.ComputeLegacyDownlinkMIC(
		types.AES128Key(testDevice.NwkSKey),
		*types.MustDevAddr(testDevice.Addr),
		testDevice.FCntDown,
		payloadWithoutMIC,
	)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, micBytes, test.ShouldResemble, expectedMIC[:])

	// Verify file persistence
	deviceInfoList, err := readFromFile(g.dataFile)
	test.That(t, err, test.ShouldBeNil)

	// Find our device in the list
	found := false
	for _, di := range deviceInfoList {
		if di.DevEUI == fmt.Sprintf("%X", testDevice.DevEui) {
			found = true
			test.That(t, di.FCntDown, test.ShouldEqual, testDevice.FCntDown)
			test.That(t, di.DevAddr, test.ShouldEqual, fmt.Sprintf("%X", testDevice.Addr))
			break
		}
	}
	test.That(t, found, test.ShouldBeTrue)
}
