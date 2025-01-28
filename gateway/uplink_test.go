package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gateway/node"

	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

var (
	// Test device configuration.
	testDeviceAddr = []byte{0xe2, 0x73, 0x65, 0x66} // BE
	testAppSKey    = []byte{
		0x55, 0x72, 0x40, 0x4C,
		0x69, 0x6E, 0x6B, 0x4C,
		0x6F, 0x52, 0x61, 0x32,
		0x30, 0x31, 0x38, 0x23,
	}
	testNodeName    = "testNode"
	testDecoderPath = "./mockdecoder.js"

	// Valid uplink payload.
	validUplinkData = []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d, // FOPT
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

// setupTestGateway creates a test gateway with a configured test device.
func setupTestGateway(t *testing.T) *gateway {
	// Create a temp device data file for testing
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "devices.txt")
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	test.That(t, err, test.ShouldBeNil)

	testDevices := make(map[string]*node.Node)
	testNode := &node.Node{
		Addr:        testDeviceAddr,
		AppSKey:     testAppSKey,
		NodeName:    testNodeName,
		DecoderPath: testDecoderPath,
		JoinType:    "OTAA",
		DevEui:      testDevEUI,
	}
	testDevices[testNodeName] = testNode

	return &gateway{
		logger:   logging.NewTestLogger(t),
		devices:  testDevices,
		dataFile: file,
	}
}

// setupTestGatewayWithFileDevice creates a test gateway with device info only in file.
func setupFileTest(t *testing.T) *gateway {
	// Create a temp device data file for testing
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "devices.txt")
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	test.That(t, err, test.ShouldBeNil)

	// Write device info to file
	devices := []deviceInfo{
		{
			DevEUI:  "0102030405060708", // matches testNode.DevEui
			DevAddr: fmt.Sprintf("%X", testDeviceAddr),
			AppSKey: fmt.Sprintf("%X", testAppSKey),
		},
	}
	data, err := json.MarshalIndent(devices, "", "  ")
	test.That(t, err, test.ShouldBeNil)
	_, err = file.Write(data)
	test.That(t, err, test.ShouldBeNil)

	// Create gateway with empty devices map but device info in file
	testDevices := make(map[string]*node.Node)
	testNode := &node.Node{
		NodeName:    testNodeName,
		DecoderPath: testDecoderPath,
		JoinType:    "OTAA",
		DevEui:      testDevEUI,
	}
	testDevices[testNodeName] = testNode

	return &gateway{
		logger:   logging.NewTestLogger(t),
		devices:  testDevices,
		dataFile: file,
	}
}

// createInvalidPayload creates an invalid payload for testing error cases.
func createInvalidPayload() []byte {
	return []byte{
		0x40,                   // MHDR: data uplink
		0x66, 0x65, 0x73, 0xe2, // Device address
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d, // FOPT
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
		0x81,       // FCTL
		0x29, 0x00, // Frame count
		0x0d, // FOPT
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
	_, _, err = g.parseDataUplink(context.Background(), createInvalidPayload())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "data received by node testNode was not parsable")

	// Test unknown device
	_, _, err = g.parseDataUplink(context.Background(), createUnknownDevicePayload())
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, errNoDevice)
	g.Close(context.Background())

	// Test that a device that is not in the device map but is in the persistent data file is added to the device map.
	g = setupFileTest(t)
	deviceName, readings, err = g.parseDataUplink(context.Background(), validUplinkData)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldNotBeNil)
	test.That(t, deviceName, test.ShouldEqual, testNodeName)

	// Verify device info was updated in device map
	device, ok := g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, device.Addr, test.ShouldResemble, testDeviceAddr)
	test.That(t, len(device.AppSKey), test.ShouldEqual, 16)
	test.That(t, device.AppSKey, test.ShouldResemble, testAppSKey)

	g.Close(context.Background())

}

func TestSearchForDeviceInFile(t *testing.T) {
	g := setupTestGateway(t)

	// Device found in file should return device info and no error
	devices := []deviceInfo{
		{
			DevEUI:  "0102030405060708",
			DevAddr: fmt.Sprintf("%X", testDeviceAddr),
			AppSKey: "5572404C694E6B4C6F526132303138323",
		},
	}
	data, err := json.MarshalIndent(devices, "", "  ")
	test.That(t, err, test.ShouldBeNil)
	_, err = g.dataFile.Write(data)
	test.That(t, err, test.ShouldBeNil)

	device, err := g.searchForDeviceInFile(testDeviceAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, device, test.ShouldNotBeNil)
	test.That(t, device.DevAddr, test.ShouldEqual, testDeviceAddr)

	//  Device not found in file should return nil and no error
	unknownAddr := []byte{0x01, 0x02, 0x03, 0x04}
	device, err = g.searchForDeviceInFile(unknownAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, device, test.ShouldBeNil)

	// Test File read error
	g.dataFile.Close()
	_, err = g.searchForDeviceInFile(testDeviceAddr)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "failed to read device info from file")
}

func TestUpdateDeviceInfo(t *testing.T) {
	g := setupTestGateway(t)

	newAppSKey := []byte{
		0x55, 0x72, 0x40, 0x4C,
		0x69, 0x6E, 0x6B, 0x4C,
		0x6F, 0x52, 0x61, 0x32,
		0x31, 0x30, 0x32, 0x23,
	}

	newDevAddr := []byte{0xe2, 0x73, 0x65, 0x67}

	// Test 1: Successful update
	validInfo := &deviceInfo{
		DevEUI:  fmt.Sprintf("%X", testDevEUI), // matches dev EUI on the gateway map
		DevAddr: fmt.Sprintf("%X", newDevAddr),
		AppSKey: fmt.Sprintf("%X", newAppSKey),
	}

	device, err := g.updateDeviceInfo(validInfo)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, device, test.ShouldNotBeNil)
	test.That(t, device.NodeName, test.ShouldEqual, testNodeName)
	test.That(t, device.AppSKey, test.ShouldResemble, newAppSKey)
	test.That(t, device.Addr, test.ShouldResemble, newDevAddr)

	// Device not found in map
	g.devices = make(map[string]*node.Node) // clear devices map
	_, err = g.updateDeviceInfo(validInfo)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "could not find the matching device in device map")
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
