package gateway

import (
	"context"
	"testing"
	"time"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/test"
)

func createUplinkData(devAddr, framePayload []byte) ([]byte, error) {
	// Create the frame header
	payload := []byte{unconfirmedUplinkMHdr}
	payload = append(payload, reverseByteArray(devAddr)...)
	// FCtrl: ADR enabled
	payload = append(payload, 0x80)
	// FCnt: 1 (little-endian)
	fcnt := uint32(1)
	fcntBytes := []byte{0x01, 0x00}
	payload = append(payload, fcntBytes...)
	// FPort: 85
	fport := byte(0x55)
	payload = append(payload, fport)

	// Encrypt the payload using the AppSKey
	encrypted, err := crypto.EncryptUplink(
		types.AES128Key(testAppSKey),
		*types.MustDevAddr(testDeviceAddr),
		fcnt,
		framePayload,
	)
	if err != nil {
		return nil, err
	}

	payload = append(payload, encrypted...)

	// Calculate MIC using NwkSKey
	mic, err := crypto.ComputeLegacyUplinkMIC(
		types.AES128Key(testNwkSKey),
		*types.MustDevAddr(testDeviceAddr),
		fcnt,
		payload,
	)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	return payload, nil
}

var (
	// Expected decoded values.
	expectedTemp     = -0.01
	expectedHumidity = 460.8
	expectedCurrent  = 0.0
)

func TestParseDataUplink(t *testing.T) {
	g := createTestGateway(t)

	// Create the plaintext payload that will decode to the expected values
	plainText := []byte{
		0x09, 0x67, 0xFF, 0xFF, // Temperature: -0.01°C
		0x03, 0x97, 0x00, 0xB4, 0x00, 0x00, // Humidity: 460.8%
		0x04, 0x98, 0x00, 0x00, // Current: 0.0A
	}

	validPayload, err := createUplinkData(testDeviceAddr, plainText)
	test.That(t, err, test.ShouldBeNil)

	// Test valid data uplink
	deviceName, readings, err := g.parseDataUplink(context.Background(), validPayload, time.Now(), 0, 0)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldNotBeNil)
	test.That(t, deviceName, test.ShouldEqual, testNodeName)

	// Verify decoded sensor values
	temp, ok := readings["temperature"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, temp, test.ShouldEqual, expectedTemp)

	humidity, ok := readings["humidity"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, humidity, test.ShouldEqual, expectedHumidity)

	current, ok := readings["current"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, current, test.ShouldEqual, expectedCurrent)

	// Test unparsable data
	// Invalid frame payload
	invalidText := []byte{
		0x00, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x00,
		0x9c, 0x00, 0xaa, 0x00,
		0x00, 0xbe,
	}

	invalidPayload, err := createUplinkData(testDeviceAddr, invalidText)
	test.That(t, err, test.ShouldBeNil)

	_, _, err = g.parseDataUplink(context.Background(), invalidPayload, time.Now(), 0, 0)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "data received by node test-device was not parsable")

	// Test unknown device
	unknownAddr := []byte{0x1, 0x2, 0x3, 0x3}
	unknownPayload, err := createUplinkData(unknownAddr, plainText)
	test.That(t, err, test.ShouldBeNil)
	_, _, err = g.parseDataUplink(context.Background(), unknownPayload, time.Now(), 0, 0)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errNoDevice)

	// Test invalid MIC
	validPayload, err = createUplinkData(testDeviceAddr, plainText)
	test.That(t, err, test.ShouldBeNil)
	validPayload[len(validPayload)-1] = 0x00
	test.That(t, err, test.ShouldBeNil)
	_, _, err = g.parseDataUplink(context.Background(), validPayload, time.Now(), 0, 0)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errInvalidMIC)

	// Test no NwkSKey
	validPayload, err = createUplinkData(testDeviceAddr, plainText)
	test.That(t, err, test.ShouldBeNil)
	validPayload[len(validPayload)-1] = 0x00
	test.That(t, err, test.ShouldBeNil)
	g.devices[testNodeName].NwkSKey = []byte{}
	_, _, err = g.parseDataUplink(context.Background(), validPayload, time.Now(), 0, 0)
	test.That(t, err, test.ShouldBeNil)

	err = g.Close(context.Background())
	test.That(t, err, test.ShouldBeNil)
}

func TestGetFOptsToSend(t *testing.T) {
	g := createTestGateway(t)

	tests := []struct {
		name     string
		fopts    []byte
		expected []byte
	}{
		{
			name:     "empty input",
			fopts:    []byte{},
			expected: []byte{},
		},
		{
			name:     "device time command only",
			fopts:    []byte{deviceTimeCID},
			expected: []byte{deviceTimeCID},
		},
		{
			name:     "link check command only",
			fopts:    []byte{linkCheckCID},
			expected: []byte{linkCheckCID},
		},
		{
			name:     "multiple commands including unsupported",
			fopts:    []byte{deviceTimeCID, 0xFF, linkCheckCID, 0xAA},
			expected: []byte{deviceTimeCID, linkCheckCID},
		},
		{
			name:     "only unsupported commands",
			fopts:    []byte{0xFF, 0xAA},
			expected: []byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := g.getFOptsToSend(tc.fopts, g.devices[testNodeName])
			test.That(t, result, test.ShouldResemble, tc.expected)
		})
	}

	err := g.Close(context.Background())
	test.That(t, err, test.ShouldBeNil)
}

func TestConvertTo32Bit(t *testing.T) {
	// Create test input with various integer types
	input := map[string]interface{}{
		"uint8_val":  uint8(255),
		"uint16_val": uint16(65535),
		"int8_val":   int8(-128),
		"int16_val":  int16(-32768),
		"other_val":  "string", // Should remain unchanged
	}

	// Convert the values
	result := convertTo32Bit(input)

	// Verify uint8 was converted to uint32
	uint8Conv, ok := result["uint8_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint8Conv, test.ShouldEqual, uint32(255))

	// Verify uint16 was converted to uint32
	uint16Conv, ok := result["uint16_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint16Conv, test.ShouldEqual, uint32(65535))

	// Verify int8 was converted to int32
	int8Conv, ok := result["int8_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int8Conv, test.ShouldEqual, int32(-128))

	// Verify int16 was converted to int32
	int16Conv, ok := result["int16_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int16Conv, test.ShouldEqual, int32(-32768))

	// Verify non-integer values remain unchanged
	otherVal, ok := result["other_val"].(string)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, otherVal, test.ShouldEqual, "string")

	// Verify empty input does nothing.
	input = map[string]interface{}{}
	result = convertTo32Bit(input)
	test.That(t, result, test.ShouldEqual, input)
}
