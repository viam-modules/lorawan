package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

var (
	// Test device identifiers.
	testDevEUI = []byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09} // Big endian
	testAppKey = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	testName   = "test-device"

	// Join request fields.
	testJoinEUI  = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	testDevEUILE = []byte{0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10} // Little endian
	testDevNonce = []byte{0x11, 0x12}

	// Unknown device for testing error cases.
	unknownDevEUI = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
)

func createDataFile(t *testing.T) *os.File {
	// Create a temp device data file for testing
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "devices.txt")
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0o644)
	test.That(t, err, test.ShouldBeNil)
	return file
}

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

	payload = append(payload, mic[:]...)
	err = validateMIC(appKey, payload)
	test.That(t, err, test.ShouldBeNil)

	// Test invalid MIC
	invalidMIC := []byte{0x00, 0x00, 0x00, 0x00}

	payload = payload[:len(payload)-4]
	payload = append(payload, invalidMIC...)
	err = validateMIC(appKey, payload)
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

	g := &gateway{
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

	err = g.Close(context.Background())
	test.That(t, err, test.ShouldBeNil)
}

func TestGenerateJoinAccept(t *testing.T) {
	testFile := createDataFile(t)
	tests := []struct {
		name            string
		joinRequest     joinRequest
		device          *node.Node
		file            *os.File
		checkFile       bool // whether to check file contents after test
		expectedFileLen int
	}{
		{
			name: "Device sending initial join reuqest should generate valid join accept, get OTAA fields populated, and added to file",
			joinRequest: joinRequest{
				joinEUI:  testJoinEUI,
				devEUI:   testDevEUILE,
				devNonce: testDevNonce,
			},
			device: &node.Node{
				AppKey: testAppKey,
			},
			file:            testFile,
			expectedFileLen: 1,
			checkFile:       true,
		},
		{
			name: "Same device joining again should generate JA, get OTAA fields repopulated, and info replaced in file",
			joinRequest: joinRequest{
				joinEUI:  testJoinEUI,
				devEUI:   testDevEUILE,
				devNonce: testDevNonce,
			},
			device: &node.Node{
				AppKey: testAppKey,
			},
			file:            testFile,
			expectedFileLen: 1,
			checkFile:       true,
		},
		{
			name: "New device joining should generate JA, get OTTAA fields popualated, and appended to file",
			joinRequest: joinRequest{
				joinEUI:  testJoinEUI,
				devEUI:   []byte{0x11, 0x11, 0x11, 0x0D, 0x0C, 0x0B, 0x0A, 0x09}, // Different DevEUI
				devNonce: testDevNonce,
			},
			device: &node.Node{
				AppKey: testAppKey,
			},
			file:            testFile,
			expectedFileLen: 2,
			checkFile:       true,
		},
		{
			name: "If writing to the file errors, should still return valid JA",
			joinRequest: joinRequest{
				joinEUI:  testJoinEUI,
				devEUI:   testDevEUILE,
				devNonce: testDevNonce,
			},
			device: &node.Node{
				AppKey: testAppKey,
			},
			file:      nil,
			checkFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var file *os.File
			if tt.file != nil {
				var err error
				file, err = os.OpenFile(tt.file.Name(), os.O_RDWR|os.O_CREATE, 0o644)
				test.That(t, err, test.ShouldBeNil)
			}

			g := &gateway{
				dataFile: file,
				logger:   logging.NewTestLogger(t),
			}

			joinAccept, err := g.generateJoinAccept(ctx, tt.joinRequest, tt.device)
			test.That(t, err, test.ShouldBeNil)
			// MHDR(1) + Encrypted(JoinNonce(3) + NetID(3) + DevAddr(4) + DLSettings(1) + RxDelay(1) + CFList(16)) + MIC(4)
			test.That(t, len(joinAccept), test.ShouldEqual, 33)
			test.That(t, joinAccept[0], test.ShouldEqual, byte(0x20))  // Join-accept message type
			test.That(t, len(tt.device.Addr), test.ShouldEqual, 4)     // Device address should be generated
			test.That(t, len(tt.device.AppSKey), test.ShouldEqual, 16) // AppSKey should be generated

			if tt.checkFile {
				devices, err := readFromFile(g.dataFile)
				test.That(t, err, test.ShouldBeNil)
				test.That(t, len(devices), test.ShouldEqual, tt.expectedFileLen)
				// Find the device in the file
				found := false
				devEUIBE := reverseByteArray(tt.joinRequest.devEUI)
				for _, d := range devices {
					if d.DevEUI == fmt.Sprintf("%X", devEUIBE) {
						test.That(t, d.DevAddr, test.ShouldEqual, fmt.Sprintf("%X", tt.device.Addr))
						test.That(t, d.AppSKey, test.ShouldEqual, fmt.Sprintf("%X", tt.device.AppSKey))
						found = true
						break
					}
				}
				test.That(t, found, test.ShouldBeTrue)
			}

			err = g.Close(ctx)
			test.That(t, err, test.ShouldBeNil)
		})
	}
}

func TestSearchAndRemove(t *testing.T) {
	tests := []struct {
		name          string
		initialData   []deviceInfo
		devEUIToFind  []byte
		expectError   bool
		expectedCount int // number of devices expected after removal
	}{
		{
			name: "device exists and is removed",
			initialData: []deviceInfo{
				{DevEUI: "100F0E0D0C0B0A09", DevAddr: "01020304", AppSKey: "0102030405060708090A0B0C0D0E0F10"},
			},
			devEUIToFind:  testDevEUI,
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "device doesn't exist",
			initialData: []deviceInfo{
				{DevEUI: "100F0E0D0C0B0A09", DevAddr: "01020304", AppSKey: "0102030405060708090A0B0C0D0E0F10"},
			},
			devEUIToFind:  []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name:          "empty file",
			initialData:   []deviceInfo{},
			devEUIToFind:  testDevEUI,
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "multiple devices, one removed",
			initialData: []deviceInfo{
				{DevEUI: "100F0E0D0C0B0A09", DevAddr: "01020304", AppSKey: "0102030405060708090A0B0C0D0E0F10"},
				{DevEUI: "2021222324252627", DevAddr: "01020305", AppSKey: "0102030405060708090A0B0C0D0E0F11"},
			},
			devEUIToFind:  testDevEUI,
			expectError:   false,
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := createDataFile(t)

			g := gateway{}

			// Initialize file with test data
			err := writeToFile(file, tt.initialData)
			test.That(t, err, test.ShouldBeNil)

			// Test searchAndRemove
			err = g.searchAndRemove(file, tt.devEUIToFind)
			if tt.expectError {
				test.That(t, err, test.ShouldNotBeNil)
			} else {
				test.That(t, err, test.ShouldBeNil)
			}

			// Verify remaining devices
			devices, err := readFromFile(file)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, len(devices), test.ShouldEqual, tt.expectedCount)

			// If we removed a device, verify it's not in the file
			if tt.expectedCount < len(tt.initialData) {
				devEUIHex := fmt.Sprintf("%X", tt.devEUIToFind)
				for _, device := range devices {
					test.That(t, device.DevEUI, test.ShouldNotEqual, devEUIHex)
				}
			}
		})
	}
}

func TestAddAndRemoveDeviceInfoToFile(t *testing.T) {
	g := gateway{}
	file := createDataFile(t)
	info := deviceInfo{DevEUI: fmt.Sprintf("%X", testDevEUI), DevAddr: "123456", AppSKey: fmt.Sprintf("%X", testAppSKey)}
	err := g.addDeviceInfoToFile(file, info)
	test.That(t, err, test.ShouldBeNil)

	deviceInfo, err := readFromFile(file)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deviceInfo), test.ShouldEqual, 1)
	test.That(t, deviceInfo[0].DevEUI, test.ShouldEqual, fmt.Sprintf("%X", testDevEUI))
	test.That(t, deviceInfo[0].DevAddr, test.ShouldEqual, "123456")
	test.That(t, deviceInfo[0].AppSKey, test.ShouldEqual, fmt.Sprintf("%X", testAppSKey))
}

func TestHandleJoin(t *testing.T) {
	devices := make(map[string]*node.Node)
	testDevice := &node.Node{
		DevEui:   testDevEUI,
		AppKey:   testAppKey,
		NodeName: testName,
	}
	devices[testName] = testDevice

	g := &gateway{
		logger:   logging.NewTestLogger(t),
		devices:  devices,
		dataFile: createDataFile(t),
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

	err = g.handleJoin(ctx, payload, time.Now())
	test.That(t, err, test.ShouldBeNil)

	// Test with unknown device
	unknownPayload := []byte{0x00} // MHDR
	unknownPayload = append(unknownPayload, testJoinEUI...)
	unknownPayload = append(unknownPayload, unknownDevEUI...)
	unknownPayload = append(unknownPayload, testDevNonce...)
	unknownPayload = append(unknownPayload, mic[:]...)

	err = g.handleJoin(ctx, unknownPayload, time.Now())
	test.That(t, err, test.ShouldEqual, errNoDevice)

	err = g.Close(ctx)
	test.That(t, err, test.ShouldBeNil)
}
