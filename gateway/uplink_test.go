package gateway

import (
	"context"
	"testing"
	"time"

	"go.viam.com/test"
)

var (
	// Valid uplink payload.
	validUplinkData = []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x80,       // FCTL
		0x29, 0x00, // Frame count
		0x55, // FPORT
		// Frame payload
		0xd6, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x79,
		0x9c, 0xf9, 0xaa, 0x25,
		0x3b, 0xbe,
		// MIC
		0x7d, 0xfe, 0x35, 0xfd,
	}

	// Expected decoded values.
	expectedTemp     = -0.01
	expectedHumidity = 460.8
	expectedCurrent  = 0.0
)

// createInvalidPayload creates an invalid payload for testing error cases.
func createInvalidPayload() []byte {
	return []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x80,       // FCTL
		0x29, 0x00, // Frame count
		0x55, // FPORT
		// Invalid frame payload
		0x00, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x00,
		0x9c, 0x00, 0xaa, 0x00,
		0x00, 0xbe,
		// MIC
		0x7d, 0xfe, 0x35, 0xfd,
	}
}

// createUnknownDevicePayload creates a payload with unknown device address.
func createUnknownDevicePayload() []byte {
	return []byte{
		0x40,                   // MHDR: data uplink
		0x61, 0x65, 0x73, 0xe2, // Unknown device address
		0x80,       // FCTL
		0x29, 0x00, // Frame count
		0x00, // FOPTs
		0x55, // FPORT
		// Frame payload
		0x00, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x00,
		0x9c, 0x00, 0xaa, 0x00,
		0x00, 0xbe,
		// MIC
		0x7d, 0xfe, 0x35, 0xfd,
	}
}

func TestParseDataUplink(t *testing.T) {
	g := createTestGateway(t)

	// Test valid data uplink
	deviceName, readings, err := g.parseDataUplink(context.Background(), validUplinkData, time.Now())
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
	_, _, err = g.parseDataUplink(context.Background(), createInvalidPayload(), time.Now())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "data received by node test-device was not parsable")

	// Test unknown device
	_, _, err = g.parseDataUplink(context.Background(), createUnknownDevicePayload(), time.Now())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errNoDevice)

	err = g.Close(context.Background())
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
