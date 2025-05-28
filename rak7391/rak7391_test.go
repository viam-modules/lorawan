package rak7391

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
	"go.viam.com/utils/protoutils"
)

// creates a test gateway with device info populated in the file.
func setupFileAndGateway(t *testing.T) *rak7391 {
	g := createTestrak(t)

	// Write device info to file
	devices := []deviceInfo{
		{
			DevEUI:  fmt.Sprintf("%X", testDevEUI),
			DevAddr: fmt.Sprintf("%X", testDeviceAddr),
			AppSKey: fmt.Sprintf("%X", testAppSKey),
		},
	}
	for _, device := range devices {
		err := g.insertOrUpdateDeviceInDB(context.Background(), device)
		test.That(t, err, test.ShouldBeNil)
	}

	return g
}

func TestValidate(t *testing.T) {
	// Test valid config with one concentrator
	bus := 0
	conf := &Config{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldResemble, []string{"pi"})

	// Test valid config with both concentrators
	bus2 := 1
	conf = &Config{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
		Concentrator2: &ConcentratorConfig{
			Bus: &bus2,
		},
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldResemble, []string{"pi"})

	// Test invalid - no concentrators configured
	conf = &Config{
		BoardName: "pi",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errConcentrators))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid - missing board name
	conf = &Config{
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "board"))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid region code
	conf = &Config{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
		Region: "INVALID",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidRegion))
	test.That(t, deps, test.ShouldBeNil)

	// Test valid region codes
	validRegions := []string{"US915", "EU868", "US", "EU"}
	for _, region := range validRegions {
		conf = &Config{
			BoardName: "pi",
			Concentrator1: &ConcentratorConfig{
				Bus: &bus,
			},
			Region: region,
		}
		deps, err = conf.Validate("")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, deps, test.ShouldResemble, []string{"pi"})
	}
}

func TestDoCommand(t *testing.T) {
	g := setupFileAndGateway(t)
	s := (sensor.Sensor)(g)

	n := node.Node{
		AppKey:      testAppKey,
		DevEui:      testDevEUI,
		DecoderPath: testDecoderPath,
		NodeName:    testNodeName,
		JoinType:    "OTAA",
	}

	// Test Validate response should be 1
	validateCmd := make(map[string]interface{})
	validateCmd["validate"] = 1
	resp, err := s.DoCommand(context.Background(), validateCmd)
	test.That(t, err, test.ShouldBeNil)
	retVal, ok := resp["validate"]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, retVal.(regions.Region), test.ShouldEqual, regions.US)

	// need to simulate what happens when the DoCommand message is serialized/deserialized into proto
	doOverWire := func(gateway sensor.Sensor, cmd map[string]interface{}) {
		command, err := protoutils.StructToStructPb(cmd)
		test.That(t, err, test.ShouldBeNil)
		_, err = gateway.DoCommand(context.Background(), command.AsMap())
		test.That(t, err, test.ShouldBeNil)
	}

	// Test Register device command - device not in file
	// clear devices
	g.devices = map[string]*node.Node{}
	registerCmd := make(map[string]interface{})
	registerCmd["register_device"] = &n
	doOverWire(s, registerCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 1)
	dev, ok := g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, dev.AppKey, test.ShouldResemble, testAppKey)
	test.That(t, dev.DevEui, test.ShouldResemble, testDevEUI)
	test.That(t, dev.DecoderPath, test.ShouldResemble, testDecoderPath)

	// Test that if the device reconfigures, fields get updated
	n.DecoderPath = "/newpath"
	registerCmd["register_device"] = &n
	doOverWire(s, registerCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 1)
	dev, ok = g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, dev.AppKey, test.ShouldResemble, testAppKey)
	test.That(t, dev.DevEui, test.ShouldResemble, testDevEUI)
	test.That(t, dev.DecoderPath, test.ShouldResemble, "/newpath")

	// Test that if device is in file, OTAA fields get populated
	// clear map and device otaa info for the new test
	dev = g.devices[testNodeName]
	g.devices = map[string]*node.Node{}
	dev.AppSKey = nil
	dev.Addr = nil
	registerCmd["register_device"] = dev
	doOverWire(s, registerCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 1)
	dev, ok = g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, dev.AppKey, test.ShouldResemble, testAppKey)
	test.That(t, dev.DevEui, test.ShouldResemble, testDevEUI)
	test.That(t, dev.DecoderPath, test.ShouldResemble, "/newpath")
	// The dev addr and AppsKey should also be populated
	test.That(t, dev.AppSKey, test.ShouldResemble, testAppSKey)
	test.That(t, dev.Addr, test.ShouldResemble, testDeviceAddr)

	// Test remove device command
	removeCmd := make(map[string]interface{})
	removeCmd["remove_device"] = n.NodeName
	doOverWire(s, removeCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 0)

	// Test GetDevice command
	cmd := make(map[string]interface{})
	cmd[node.GetDeviceKey] = fmt.Sprintf("%X", testDevEUI)
	resp, err = s.DoCommand(context.Background(), cmd)
	test.That(t, err, test.ShouldBeNil)
	deviceInfo, ok := resp[node.GetDeviceKey].(deviceInfo)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, deviceInfo.DevEUI, test.ShouldEqual, fmt.Sprintf("%X", testDevEUI))
	test.That(t, deviceInfo.DevAddr, test.ShouldEqual, fmt.Sprintf("%X", testDeviceAddr))
	test.That(t, deviceInfo.AppSKey, test.ShouldEqual, fmt.Sprintf("%X", testAppSKey))

	// Test GetDeviceKey with invalid devEUI type
	cmd[node.GetDeviceKey] = 123
	_, err = s.DoCommand(context.Background(), cmd)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "expected a string")

	// Test GetDeviceKey with non-existent device
	cmd[node.GetDeviceKey] = "09876655"
	ret, err := s.DoCommand(context.Background(), cmd)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, ret[node.GetDeviceKey], test.ShouldBeNil)

	// test sendDownlink command
	testDownLinkPayload := "ff03"

	// Clear devices and add a device for testing
	g.devices = map[string]*node.Node{}
	g.devices[testNodeName] = &node.Node{
		NodeName:    testNodeName,
		DecoderPath: testDecoderPath,
		JoinType:    "OTAA",
		DevEui:      testDevEUI,
		AppKey:      testAppKey,
	}

	downlinkCmd := make(map[string]interface{})
	downlinkCmd[node.GatewaySendDownlinkKey] = map[string]interface{}{
		testNodeName: testDownLinkPayload,
	}

	resp, err = s.DoCommand(context.Background(), downlinkCmd)
	test.That(t, err, test.ShouldBeNil)

	// Verify response
	retVal, ok = resp[node.GatewaySendDownlinkKey]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, retVal, test.ShouldEqual, "downlink added")

	// Verify the downlink was added to the node
	testNode, ok := g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, len(testNode.Downlinks), test.ShouldEqual, 1)

	// Verify the downlink payload matches (after hex decoding)
	expectedPayload, err := hex.DecodeString(testDownLinkPayload)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, testNode.Downlinks[0], test.ShouldResemble, expectedPayload)

	// Unknown device name should error
	downlinkCmd[node.GatewaySendDownlinkKey] = map[string]interface{}{
		"unknown-node": testDownLinkPayload,
	}
	_, err = s.DoCommand(context.Background(), downlinkCmd)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "not found")

	// invalid byte encoding should error
	downlinkCmd[node.GatewaySendDownlinkKey] = map[string]interface{}{
		testNodeName: "olf",
	}
	_, err = s.DoCommand(context.Background(), downlinkCmd)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "failed to decode")
}

func TestMergeNodes(t *testing.T) {
	tests := []struct {
		name      string
		newNode   *node.Node
		oldNode   *node.Node
		want      *node.Node
		expectErr error
	}{
		{
			name: "OTAA join type - keep old Addr and AppSKey from join procedure",
			newNode: &node.Node{
				DecoderPath: "new/decoder/path",
				NodeName:    "NewNode",
				JoinType:    "OTAA",
				AppKey:      testAppKey,
				DevEui:      testDevEUI,
				AppSKey: []byte{
					0x11, 0x11, 0x40, 0x4C,
					0x69, 0x6E, 0x6B, 0x4C,
					0x6F, 0x52, 0x61, 0x32,
					0x30, 0x31, 0x38, 0x23,
				},
			},
			oldNode: &node.Node{
				Addr:    testDeviceAddr,
				AppSKey: testAppSKey,
			},
			want: &node.Node{
				DecoderPath: "new/decoder/path",
				NodeName:    "NewNode",
				JoinType:    "OTAA",
				Addr:        testDeviceAddr,
				AppSKey:     testAppSKey,
				AppKey:      testAppKey,
				DevEui:      testDevEUI,
			},
			expectErr: nil,
		},
		{
			name: "ABP join type - use new Addr and AppSKey",
			newNode: &node.Node{
				DecoderPath: "new/decoder/path",
				NodeName:    "NewNode",
				JoinType:    "ABP",
				Addr:        testDeviceAddr,
				AppSKey:     testAppSKey,
			},
			oldNode: &node.Node{
				Addr: []byte{1, 5, 6, 7},
				AppSKey: []byte{
					0x11, 0x11, 0x40, 0x4C,
					0x69, 0x6E, 0x6B, 0x4C,
					0x6F, 0x52, 0x61, 0x32,
					0x30, 0x31, 0x38, 0x23,
				},
			},
			want: &node.Node{
				DecoderPath: "new/decoder/path",
				NodeName:    "NewNode",
				JoinType:    "ABP",
				Addr:        testDeviceAddr,
				AppSKey:     testAppSKey,
			},
			expectErr: nil,
		},
		{
			name: "Invalid join type",
			newNode: &node.Node{
				JoinType: "INVALID",
			},
			oldNode:   &node.Node{},
			want:      nil,
			expectErr: errUnexpectedJoinType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeNodes(tt.newNode, tt.oldNode)
			if tt.expectErr != nil {
				test.That(t, err, test.ShouldBeError, tt.expectErr)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, got, test.ShouldResemble, tt.want)
			}
		})
	}
}

func TestUpdateReadings(t *testing.T) {
	g := &rak7391{
		lastReadings: make(map[string]interface{}),
	}

	// Test adding new readings
	newReading := map[string]interface{}{
		"temp": 20.5,
		"hum":  60,
	}

	expectedReadings := map[string]interface{}{
		"device1": newReading,
	}

	g.updateReadings("device1", newReading)
	readings, err := g.Readings(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, expectedReadings)

	// Test updating readings of a device that contains readings already.
	newReading = map[string]interface{}{
		"temp": 26.5,
		"pres": 1013,
	}

	expectedReadings = map[string]interface{}{
		"device1": map[string]interface{}{
			"temp": 26.5,
			"hum":  60,
			"pres": 1013,
		},
	}
	g.updateReadings("device1", newReading)
	readings, err = g.Readings(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, expectedReadings)
}

func TestReadings(t *testing.T) {
	g := &rak7391{
		lastReadings: make(map[string]interface{}),
	}

	// If lastReadings is empty and the call is not from data manager, return no error.
	readings, err := g.Readings(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, noReadings)

	// If lastReadings is empty and the call is from data manager, return ErrNoCaptureToStore
	_, err = g.Readings(context.Background(), map[string]interface{}{data.FromDMString: true})
	test.That(t, err, test.ShouldBeError, data.ErrNoCaptureToStore)

	// If data.FromDmString is false, return no error
	_, err = g.Readings(context.Background(), map[string]interface{}{data.FromDMString: false})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, noReadings)

	// successful readings call test case.
	expectedReadings := map[string]interface{}{
		"device1": map[string]interface{}{
			"temp": 26.5,
			"hum":  60,
			"pres": 1013,
		},
	}

	g.lastReadings = expectedReadings
	readings, err = g.Readings(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, expectedReadings)
}

func TestUpdateDeviceInfo(t *testing.T) {
	g := createTestrak(t)

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

	device := g.devices[testNodeName]

	err := g.updateDeviceInfo(device, validInfo)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, device, test.ShouldNotBeNil)
	test.That(t, device.NodeName, test.ShouldEqual, testNodeName)
	test.That(t, device.AppSKey, test.ShouldResemble, newAppSKey)
	test.That(t, device.Addr, test.ShouldResemble, newDevAddr)
}

func TestCreateConcentrator(t *testing.T) {
	// Create test rak instance
	r := createTestrak(t)

	// Create mock board with inject
	b := &inject.Board{}
	rstPin := &inject.GPIOPin{}

	// Setup mock pin behavior
	rstPin.SetFunc = func(ctx context.Context, high bool, extra map[string]interface{}) error {
		return nil
	}

	// Setup mock board behavior
	b.GPIOPinByNameFunc = func(name string) (board.GPIOPin, error) {
		if name == rak7391ResetPin1 {
			return rstPin, nil
		}
		return nil, fmt.Errorf("unknown pin %s", name)
	}

	// Create temp dir for test files
	tmpDir := t.TempDir()

	// Create mock CGO managed process file
	mockCGOSource := `package main

import "fmt"

func main() {
	port := 8080
	fmt.Println("Server successfully started:", port)
	fmt.Println("done setting up gateway")
}`

	srcPath := filepath.Join(tmpDir, "main.go")
	err := os.WriteFile(srcPath, []byte(mockCGOSource), 0o644)
	test.That(t, err, test.ShouldBeNil)

	// Build the mock binary
	cgoPath := filepath.Join(tmpDir, "cgo")
	cmd := exec.Command("go", "build", "-o", cgoPath, srcPath)
	err = cmd.Run()
	test.That(t, err, test.ShouldBeNil)
	r.cgoPath = cgoPath
	defer os.Remove(cgoPath)

	testBus := 1

	// Create a real device file that our symlinks will point to
	deviceFile, err := os.CreateTemp(tmpDir, "test-device")
	test.That(t, err, test.ShouldBeNil)
	defer os.Remove(deviceFile.Name())

	// Create test directories to mimic Linux's /dev/serial structure
	byPathDir := filepath.Join(tmpDir, "by-path")
	byIDDir := filepath.Join(tmpDir, "by-id")
	err = os.MkdirAll(byPathDir, 0o755)
	test.That(t, err, test.ShouldBeNil)
	err = os.MkdirAll(byIDDir, 0o755)
	test.That(t, err, test.ShouldBeNil)

	// Create test symlinks
	byPathLink := filepath.Join(byPathDir, "pci-0000:00:14.0-usb-0:2:1.0")
	byIDLink := filepath.Join(byIDDir, "usb-FTDI_FT232R_USB_UART_AB0CDEFG-if00-port0")

	err = os.Symlink(deviceFile.Name(), byPathLink)
	test.That(t, err, test.ShouldBeNil)
	err = os.Symlink(deviceFile.Name(), byIDLink)
	test.That(t, err, test.ShouldBeNil)

	tests := []struct {
		name        string
		config      *ConcentratorConfig
		id          string
		expectError bool
		expectedErr error
	}{
		{
			name: "SPI configuration",
			config: &ConcentratorConfig{
				Bus: &testBus,
			},
			id:          "rak1",
			expectError: false,
		},
		{
			name: "USB configuration",
			config: &ConcentratorConfig{
				Path: "/dev/ttyUSB0",
			},
			id:          "rak1",
			expectError: false,
		},
		{
			name: "USB configuration with symlink by-path",
			config: &ConcentratorConfig{
				Path: byPathDir,
			},
			id:          "rak1",
			expectError: false,
		},
		{
			name: "USB configuration with symlink by-id",
			config: &ConcentratorConfig{
				Path: byIDDir,
			},
			id:          "rak1",
			expectError: false,
		},
		{
			name: "unknown id should return error",
			config: &ConcentratorConfig{
				Path: "/dev/ttyUSB0",
			},
			id:          "test-rak",
			expectError: true,
			expectedErr: errUnknownID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := r.createConcentrator(context.Background(), tc.id, rak7391ResetPin1, b, tc.config)
			if tc.expectError {
				test.That(t, err, test.ShouldNotBeNil)
				test.That(t, err, test.ShouldBeError, tc.expectedErr)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, len(r.concentrators), test.ShouldBeGreaterThan, 0)
				test.That(t, r.concentrators[len(r.concentrators)-1].client, test.ShouldNotBeNil)
				test.That(t, r.concentrators[len(r.concentrators)-1].logReader, test.ShouldNotBeNil)
				test.That(t, r.concentrators[len(r.concentrators)-1].pipeReader, test.ShouldNotBeNil)
				test.That(t, r.concentrators[len(r.concentrators)-1].pipeWriter, test.ShouldNotBeNil)
				test.That(t, r.concentrators[len(r.concentrators)-1].started, test.ShouldBeTrue)
			}

			r.closeProcesses()
		})
	}
}

func TestWatchLogs(t *testing.T) {
	logger := logging.NewTestLogger(t)
	tests := []struct {
		name          string
		logLines      []string
		expectPort    string
		expectError   bool
		expectedError error
	}{
		{
			name: "successful startup",
			logLines: []string{
				"some initial log",
				"Server successfully started:8080",
				"configuring something",
				"done setting up gateway",
			},
			expectPort:  "8080",
			expectError: false,
		},
		{
			name: "missing port information",
			logLines: []string{
				"some initial log",
				"configuring something",
				"done setting up gateway",
			},
			expectPort:    "",
			expectError:   true,
			expectedError: errTimedOut,
		},
		{
			name: "missing done setup message",
			logLines: []string{
				"some initial log",
				"Server successfully started:8080",
				"configuring something",
			},
			expectPort:    "",
			expectError:   true,
			expectedError: errTimedOut,
		},
		{
			name:          "timeout with no logs",
			logLines:      []string{},
			expectPort:    "",
			expectError:   true,
			expectedError: errTimedOut,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a pipe for testing
			pr, pw := io.Pipe()
			reader := bufio.NewReader(pr)
			c := &concentrator{
				pipeReader: pr,
				pipeWriter: pw,
				logReader:  reader,
			}

			// Start goroutine to write test logs
			go func() {
				for _, line := range tc.logLines {
					_, err := pw.Write([]byte(line + "\n"))
					test.That(t, err, test.ShouldBeNil)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			port, err := c.watchLogs(ctx, logger)

			if tc.expectError {
				test.That(t, err, test.ShouldNotBeNil)
				test.That(t, err, test.ShouldBeError, errTimedOut)
				test.That(t, port, test.ShouldBeEmpty)
			} else {
				test.That(t, err, test.ShouldBeNil)
				test.That(t, port, test.ShouldEqual, tc.expectPort)
			}

			if c.workers != nil {
				c.workers.Stop()
			}
			pw.Close()
			pr.Close()
		})
	}
}

func TestClose(t *testing.T) {
	// Create a gateway instance for testing
	cfg := resource.Config{
		Name: "test-rak",
	}

	pr1, pw1 := io.Pipe()
	pr2, pw2 := io.Pipe()

	r := &rak7391{
		Named:         cfg.ResourceName().AsNamed(),
		logger:        logging.NewTestLogger(t),
		concentrators: []*concentrator{{pipeReader: pr1, pipeWriter: pw1, started: true}, {pipeReader: pr2, pipeWriter: pw2, started: true}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Call Close and verify cleanup
	err := r.Close(ctx)
	test.That(t, err, test.ShouldBeNil)

	// Verify concentrator started is false
	for _, c := range r.concentrators {
		test.That(t, c.started, test.ShouldBeFalse)
		// Verify logs pipe is closed
		{
			buf := make([]byte, 1)
			_, err = c.pipeWriter.Write(buf)
			test.That(t, err, test.ShouldNotBeNil)
		}
		{
			buf := make([]byte, 1)
			_, err = c.pipeReader.Read(buf)
			test.That(t, err, test.ShouldNotBeNil)
		}
	}

	// call close again
	err = r.Close(ctx)
	test.That(t, err, test.ShouldBeNil)
}
