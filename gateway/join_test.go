package gateway

import (
	"context"
	"testing"

	"gateway/node"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

var (
	// Test device identifiers
	testDevEUI = []byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09} // Big endian
	testAppKey = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	testName   = "test-device"

	// Join request fields
	testJoinEUI  = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	testDevEUILE = []byte{0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10} // Little endian
	testDevNonce = []byte{0x11, 0x12}

	// Unknown device for testing error cases
	unknownDevEUI = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
)

func TestReverseByteArray(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "empty array",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "single byte",
			input:    []byte{0x01},
			expected: []byte{0x01},
		},
		{
			name:     "multiple bytes",
			input:    []byte{0x01, 0x02, 0x03, 0x04},
			expected: []byte{0x04, 0x03, 0x02, 0x01},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reverseByteArray(tt.input)
			test.That(t, result, test.ShouldResemble, tt.expected)
		})
	}
}

// test that random dev addr is 4 bytes and 7 msb is network id.
func TestGenerateDevAddr(t *testing.T) {
	addr := generateDevAddr()
	test.That(t, len(addr), test.ShouldEqual, 4)
	test.That(t, addr[0], test.ShouldEqual, netID[0])
	test.That(t, addr[1], test.ShouldEqual, netID[1])
}

// test that random join nonce is 3 bytes long.
func TestGenerateJoinNonce(t *testing.T) {
	nonce := generateJoinNonce()
	test.That(t, len(nonce), test.ShouldEqual, 3)
}

func TestValidateMIC(t *testing.T) {
	appKey := types.AES128Key(testAppKey)

	// Test valid MIC
	payload := []byte{0x00} // MHDR
	payload = append(payload, testJoinEUI...)
	payload = append(payload, testDevEUILE...)
	payload = append(payload, testDevNonce...)

	mic, err := crypto.ComputeJoinRequestMIC(appKey, payload)
	test.That(t, err, test.ShouldBeNil)

	validPayload := append(payload, mic[:]...)
	err = validateMIC(appKey, validPayload)
	test.That(t, err, test.ShouldBeNil)

	// Test invalid MIC
	invalidMIC := []byte{0x00, 0x00, 0x00, 0x00}
	invalidPayload := append(payload, invalidMIC...)
	err = validateMIC(appKey, invalidPayload)
	test.That(t, err, test.ShouldBeError, errInvalidMIC)
}

func TestParseJoinRequestPacket(t *testing.T) {
	devices := make(map[string]*node.Node)
	testDevice := &node.Node{
		DevEui:   testDevEUI,
		AppKey:   testAppKey,
		NodeName: testName,
	}
	devices[testName] = testDevice

	g := &Gateway{
		logger:  logging.NewTestLogger(t),
		devices: devices,
	}

	// Create valid join request payload
	payload := []byte{0x00} // MHDR
	payload = append(payload, testJoinEUI...)
	payload = append(payload, testDevEUILE...)
	payload = append(payload, testDevNonce...)

	mic, err := crypto.ComputeJoinRequestMIC(types.AES128Key(testDevice.AppKey), payload)
	test.That(t, err, test.ShouldBeNil)
	payload = append(payload, mic[:]...)

	// Test valid payload
	jr, device, err := g.parseJoinRequestPacket(payload)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, device, test.ShouldNotBeNil)
	test.That(t, jr.joinEUI, test.ShouldResemble, testJoinEUI)
	test.That(t, jr.devEUI, test.ShouldResemble, testDevEUILE)
	test.That(t, jr.devNonce, test.ShouldResemble, testDevNonce)

	// Test payload from unknown device
	unknownPayload := []byte{0x00} // MHDR
	unknownPayload = append(unknownPayload, testJoinEUI...)
	unknownPayload = append(unknownPayload, unknownDevEUI...)
	unknownPayload = append(unknownPayload, testDevNonce...)
	unknownPayload = append(unknownPayload, mic[:]...)

	_, _, err = g.parseJoinRequestPacket(unknownPayload)
	test.That(t, err, test.ShouldEqual, errNoDevice)
}

func TestGenerateJoinAccept(t *testing.T) {
	ctx := context.Background()
	jr := joinRequest{
		joinEUI:  testJoinEUI,
		devEUI:   testDevEUILE,
		devNonce: testDevNonce,
	}

	device := &node.Node{
		AppKey: testAppKey,
	}

	joinAccept, err := generateJoinAccept(ctx, jr, device)
	test.That(t, err, test.ShouldBeNil)
	// MHDR(1) + Encrypted(JoinNonce(3) + NetID(3) + DevAddr(4) + DLSettings(1) + RxDelay(1) + CFList(16)) + MIC(4) = 33 bytes
	test.That(t, len(joinAccept), test.ShouldEqual, 33)
	// Join-accept MHDR byte.
	test.That(t, joinAccept[0], test.ShouldEqual, byte(0x20))
	// Device address should be generated and added to device.
	test.That(t, len(device.Addr), test.ShouldEqual, 4)
	// AppSKey should be generated and added to device.
	test.That(t, len(device.AppSKey), test.ShouldEqual, 16)
}

func TestHandleJoin(t *testing.T) {
	devices := make(map[string]*node.Node)
	testDevice := &node.Node{
		DevEui:   testDevEUI,
		AppKey:   testAppKey,
		NodeName: testName,
	}
	devices[testName] = testDevice

	g := &Gateway{
		logger:  logging.NewTestLogger(t),
		devices: devices,
	}

	// Create valid join request payload
	payload := []byte{0x00} // MHDR
	payload = append(payload, testJoinEUI...)
	payload = append(payload, testDevEUILE...)
	payload = append(payload, testDevNonce...)

	mic, err := crypto.ComputeJoinRequestMIC(types.AES128Key(testDevice.AppKey), payload)
	test.That(t, err, test.ShouldBeNil)
	payload = append(payload, mic[:]...)

	// Test with context that will timeout before rx2 window
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	err = g.handleJoin(ctx, payload)
	test.That(t, err, test.ShouldBeNil)

	// Test with unknown device
	unknownPayload := []byte{0x00} // MHDR
	unknownPayload = append(unknownPayload, testJoinEUI...)
	unknownPayload = append(unknownPayload, unknownDevEUI...)
	unknownPayload = append(unknownPayload, testDevNonce...)
	unknownPayload = append(unknownPayload, mic[:]...)

	err = g.handleJoin(ctx, unknownPayload)
	test.That(t, err, test.ShouldEqual, errNoDevice)
}
