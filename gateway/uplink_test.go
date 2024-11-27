package gateway

import (
	"context"
	"gateway/node"
	"testing"

	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

var (
	// Test device configuration
	testDeviceAddr = []byte{0xe2, 0x73, 0x65, 0x66} // BE
	testAppSKey   = []byte{
		0x55, 0x72, 0x40, 0x4C,
		0x69, 0x6E, 0x6B, 0x4C,
		0x6F, 0x52, 0x61, 0x32,
		0x30, 0x31, 0x38, 0x23,
	}
	testNodeName    = "testNode"
	testDecoderPath = "./testdecoder.js"

	// Valid uplink data fields
	validUplinkData = []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d,       // FOPT
		0x55,       // FPORT
		// Frame payload
		0xd6, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x79,
		0x9c, 0xf9, 0xaa, 0x25,
		0x3b, 0xbe,
		// MIC
		0x7d, 0xfe, 0x35, 0xfd,
	}

	// Expected decoded values
	expectedTemp     = -0.01
	expectedHumidity = 460.8
	expectedCurrent  = 0.0
)

// setupTestGateway creates a test gateway with a configured test device
func setupTestGateway(t *testing.T) *Gateway {
	testDevices := make(map[string]*node.Node)
	testNode := &node.Node{
		Addr:        testDeviceAddr,
		AppSKey:     testAppSKey,
		NodeName:    testNodeName,
		DecoderPath: testDecoderPath,
		JoinType:    "OTAA",
	}
	testDevices[testNodeName] = testNode

	return &Gateway{
		logger:  logging.NewTestLogger(t),
		devices: testDevices,
	}
}

// createInvalidPayload creates an invalid payload for testing error cases
func createInvalidPayload() []byte {
	return []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d,       // FOPT
		0x55,       // FPORT
		// Invalid frame payload
		0x00, 0x02, 0x25, 0x00,
		0x2b, 0xc4, 0xdf, 0x00,
		0x9c, 0x00, 0xaa, 0x00,
		0x00, 0xbe,
		// MIC
		0x7d, 0xfe, 0x35, 0xfd,
	}
}

// createUnknownDevicePayload creates a payload with unknown device address
func createUnknownDevicePayload() []byte {
	return []byte{
		0x40,                   // MHDR: data uplink
		0x61, 0x65, 0x73, 0xe2, // Unknown device address
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d,       // FOPT
		0x55,       // FPORT
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
	g := setupTestGateway(t)

	// Test valid data uplink
	deviceName, readings, err := g.parseDataUplink(context.Background(), validUplinkData)
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
	deviceName, readings, err = g.parseDataUplink(context.Background(), createInvalidPayload())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "data received by node testNode was not parsable")

	// Test unknown device
	deviceName, readings, err = g.parseDataUplink(context.Background(), createUnknownDevicePayload())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldEqual, errNoDevice)
}

func TestConvertTo32Bit(t *testing.T) {
	// Test data
	input := map[string]interface{}{
		"uint8_val":  uint8(255),
		"uint16_val": uint16(65535),
		"int8_val":   int8(-128),
		"int16_val":  int16(-32768),
		"other_val":  "string", // Should remain unchanged
	}

	// Expected values
	expectedUint32 := uint32(255)
	expectedInt32 := int32(-128)
	expectedString := "string"

	// Convert the values
	result := convertTo32Bit(input)

	// Verify uint conversions
	uint8Conv, ok := result["uint8_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint8Conv, test.ShouldEqual, expectedUint32)

	uint16Conv, ok := result["uint16_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint16Conv, test.ShouldEqual, uint32(65535))

	// Verify int conversions
	int8Conv, ok := result["int8_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int8Conv, test.ShouldEqual, expectedInt32)

	int16Conv, ok := result["int16_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int16Conv, test.ShouldEqual, int32(-32768))

	// Verify non-integer values remain unchanged
	otherVal, ok := result["other_val"].(string)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, otherVal, test.ShouldEqual, expectedString)

	// Verify empty input does nothing.
	input = map[string]interface{}{}
	result = convertTo32Bit(input)
	test.That(t, result, test.ShouldEqual, input)
}
