package gateway

import (
	"context"
	"gateway/node"
	"testing"

	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestParseDataUplink(t *testing.T) {

	g := Gateway{logger: logging.NewTestLogger(t)}

	testDevices := make(map[string]*node.Node)

	testNode := &node.Node{
		Addr:        []byte{0xe2, 0x73, 0x65, 0x66}, // BE
		AppSKey:     []byte{0x55, 0x72, 0x40, 0x4C, 0x69, 0x6E, 0x6B, 0x4C, 0x6F, 0x52, 0x61, 0x32, 0x30, 0x31, 0x38, 0x23},
		NodeName:    "testNode",
		DecoderPath: "./testdecoder.js",
		JoinType:    "OTAA",
	}

	testDevices["testNode"] = testNode

	g.devices = testDevices

	// Valid data
	input := []byte{
		0x40, //mhdr: data uplink
		0x66, //addr
		0x65, // addr
		0x73, // addr
		0xe2, //addr
		0x81, //fctl
		0x29, //frame count
		0x00, // frame count
		0x0d, // fopt
		0x55, //fport
		0xd6, // frame payload
		0x02,
		0x25,
		0x00,
		0x2b,
		0xc4,
		0xdf,
		0x79,
		0x9c,
		0xf9,
		0xaa,
		0x25,
		0x3b,
		0xbe,
		0x7d, // MIC
		0xfe,
		0x35,
		0xfd,
	}

	deviceName, readings, err := g.parseDataUplink(context.Background(), input)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldNotBeNil)
	test.That(t, deviceName, test.ShouldEqual, testNode.NodeName)

	// Verify the values were parsed correctly
	temp, ok := readings["temperature"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, temp, test.ShouldEqual, -0.01)

	humidity, ok := readings["humidity"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, humidity, test.ShouldEqual, 460.8)

	current, ok := readings["current"].(float64)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, current, test.ShouldEqual, 0)

	// unparsable data
	// invalidPayload := []byte{
	// 	0x40, //mhdr: data uplink
	// 	0x66, //addr
	// 	0x65, // addr
	// 	0x73, // addr
	// 	0xe2, //addr
	// 	0x81, //fctl
	// 	0x29, //frame count
	// 	0x00, // frame count
	// 	0x0d, // fopt
	// 	0x55, //fport
	// 	0x00, // frame payload
	// 	0x02,
	// 	0x25,
	// 	0x00,
	// 	0x2b,
	// 	0xc4,
	// 	0xdf,
	// 	0x00,
	// 	0x9c,
	// 	0x00,
	// 	0xaa,
	// 	0x00,
	// 	0x00,
	// 	0xbe,
	// 	0x7d, // MIC
	// 	0xfe,
	// 	0x35,
	// 	0xfd,
	// }

	// // deviceName, readings, err = g.parseDataUplink(context.Background(), invalidPayload)
	// test.That(t, err, test.ShouldNotBeNil)

	// data from unknown devicre
	unknownPayload := []byte{
		0x40, //mhdr: data uplink
		0x61, //addr
		0x65, // addr
		0x73, // addr
		0xe2, //addr
		0x81, //fctl
		0x29, //frame count
		0x00, // frame count
		0x0d, // fopt
		0x55, //fport
		0x00, // frame payload
		0x02,
		0x25,
		0x00,
		0x2b,
		0xc4,
		0xdf,
		0x00,
		0x9c,
		0x00,
		0xaa,
		0x00,
		0x00,
		0xbe,
		0x7d, // MIC
		0xfe,
		0x35,
		0xfd,
	}

	deviceName, readings, err = g.parseDataUplink(context.Background(), unknownPayload)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errNoDevice)

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
