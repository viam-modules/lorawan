package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

var (
	testDevEUI   = []byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09} // Big endian
	testAppKey   = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	testNodeName = "test-device"

	testJoinEUI  = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	testDevEUILE = []byte{0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10} // Little endian
	testDevNonce = []byte{0x11, 0x12}

	testDecoderPath = "./mockdecoder.js"

	testDeviceAddr = []byte{0xe2, 0x73, 0x65, 0x66} // BE
	testAppSKey    = []byte{
		0x55, 0x72, 0x40, 0x4C,
		0x69, 0x6E, 0x6B, 0x4C,
		0x6F, 0x52, 0x61, 0x32,
		0x30, 0x31, 0x38, 0x23,
	}
	testNwkSKey = []byte{
		0x55, 0x72, 0x40, 0x4C,
		0x69, 0x6E, 0x6B, 0x4C,
		0x6F, 0x52, 0x61, 0x32,
		0x30, 0x30, 0x23, 0x20,
	}

	// Unknown device for testing error cases.
	unknownDevEUI = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
)

func createDataFile(t *testing.T) *os.File {
	// Create a temp device data file for testing
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "devices.txt")
	//nolint:gosec
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0o644)
	test.That(t, err, test.ShouldBeNil)
	return file
}

func createTestGateway(t *testing.T) *gateway {
	// Create a temp device data file for testing
	file := createDataFile(t)

	testDevices := make(map[string]*node.Node)
	testNode := &node.Node{
		Addr:        testDeviceAddr,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
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
