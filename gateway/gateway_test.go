package gateway

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
	"go.viam.com/utils/protoutils"
)

// creates a test gateway with device info populated in the file.
func setupFileAndGateway(t *testing.T) *gateway {
	g := createTestGateway(t)

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
	// Test valid config with default bus
	resetPin := 25
	conf := &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test valid config with bus=1
	conf = &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Bus:       1,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test missing reset pin
	conf = &Config{
		BoardName: "pi",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "reset_pin"))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid bus value
	conf = &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Bus:       2,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidSpiBus))
	test.That(t, deps, test.ShouldBeNil)

	// Test missing boardName
	conf = &Config{
		ResetPin: &resetPin,
	}

	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "board"))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid region
	conf = &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Region:    "AS923",
	}

	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidRegion))
	test.That(t, deps, test.ShouldBeNil)

	// Region code can be just the region
	conf = &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Region:    "EU",
	}

	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)

	// Region can be just be the number
	conf = &Config{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Region:    "915",
	}

	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
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
	g := &gateway{
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
	g := &gateway{
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

func TestStartCLogging(t *testing.T) {
	// Create a gateway instance for testing
	cfg := resource.Config{
		Name: "test-gateway",
	}

	loggingRoutineStarted = make(map[string]bool)

	g := &gateway{
		Named:  cfg.ResourceName().AsNamed(),
		logger: logging.NewTestLogger(t),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ensure logging is started if there is no entry in the loggingRoutineStarted map.
	g.startCLogging()
	test.That(t, g.loggingWorker, test.ShouldNotBeNil)
	test.That(t, len(loggingRoutineStarted), test.ShouldEqual, 1)
	test.That(t, loggingRoutineStarted["test-gateway"], test.ShouldBeTrue)

	// Test that closing the gateway removes the gateway from the loggingRoutineStarted map.
	err := g.Close(ctx)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(loggingRoutineStarted), test.ShouldEqual, 0)

	// reader and writer files should error if closed.
	buf := make([]byte, 1)
	_, err = g.logWriter.Write(buf)
	test.That(t, err, test.ShouldNotBeNil)
	_, err = g.logReader.Read(buf)
	test.That(t, err, test.ShouldNotBeNil)

	// Ensure no new goroutine is started if the loggingRoutineStarted entry is true.
	// reset fields for new test case
	g.loggingWorker = nil
	loggingRoutineStarted["test-gateway"] = true
	g.startCLogging()
	test.That(t, g.loggingWorker, test.ShouldBeNil)
	test.That(t, len(loggingRoutineStarted), test.ShouldEqual, 1)
	test.That(t, loggingRoutineStarted["test-gateway"], test.ShouldBeTrue)
}

func TestUpdateDeviceInfo(t *testing.T) {
	g := createTestGateway(t)

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

func TestClose(t *testing.T) {
	// Create a gateway instance for testing
	cfg := resource.Config{
		Name: "test-gateway",
	}

	loggingRoutineStarted = make(map[string]bool)

	g := &gateway{
		Named:   cfg.ResourceName().AsNamed(),
		logger:  logging.NewTestLogger(t),
		started: true,
	}
	dataDirectory1 := t.TempDir()
	err := g.setupSqlite(context.Background(), dataDirectory1)
	test.That(t, err, test.ShouldBeNil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start logging to test cleanup
	g.startCLogging()
	test.That(t, g.loggingWorker, test.ShouldNotBeNil)
	test.That(t, loggingRoutineStarted["test-gateway"], test.ShouldBeTrue)

	// Call Close and verify cleanup
	err = g.Close(ctx)
	test.That(t, err, test.ShouldBeNil)

	// Verify gateway is reset
	test.That(t, g.started, test.ShouldBeFalse)

	// Verify logging resources are cleaned up
	test.That(t, len(loggingRoutineStarted), test.ShouldEqual, 0)

	// Verify file handles are closed
	{
		buf := make([]byte, 1)
		_, err = g.logWriter.Write(buf)
		test.That(t, err, test.ShouldNotBeNil)
	}
	{
		buf := make([]byte, 1)
		_, err = g.logReader.Read(buf)
		test.That(t, err, test.ShouldNotBeNil)
	}

	_, err = g.getAllDevicesFromDB(ctx)
	test.That(t, err, test.ShouldNotBeNil)

	// call close again
	err = g.Close(ctx)
	test.That(t, err, test.ShouldBeNil)
}

func TestNativeConfig(t *testing.T) {
	t.Run("Test Default Config", func(t *testing.T) {
		resetPin := 85
		powerPin := 74
		validConf := resource.Config{
			Name: "test-default",
			ConvertedAttributes: &Config{
				BoardName: "pi",
				ResetPin:  &resetPin,
				Bus:       1,
				PowerPin:  &powerPin,
			},
			Model: ModelGenericHat,
		}
		cfg, err := getNativeConfig(validConf)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, cfg.BoardName, test.ShouldEqual, "pi")
		test.That(t, *cfg.ResetPin, test.ShouldEqual, resetPin)
		test.That(t, *cfg.PowerPin, test.ShouldEqual, powerPin)
	})
	t.Run("Test WaveshareHat Config", func(t *testing.T) {
		validConf := resource.Config{
			Name: "test-default",
			ConvertedAttributes: &ConfigSX1302WaveshareHAT{
				BoardName: "pi",
				Bus:       1,
			},
			Model: ModelSX1302WaveshareHat,
		}
		cfg, err := getNativeConfig(validConf)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, cfg.BoardName, test.ShouldEqual, "pi")
		test.That(t, *cfg.ResetPin, test.ShouldEqual, waveshareHatResetPin)
		test.That(t, *cfg.PowerPin, test.ShouldEqual, waveshareHatPowerPin)
	})
	t.Run("Test some random Config", func(t *testing.T) {
		validConf := resource.Config{
			Name:                "test-default",
			ConvertedAttributes: &node.Config{},
		}
		cfg, err := getNativeConfig(validConf)
		test.That(t, err.Error(), test.ShouldContainSubstring, "build error in module. Unsupported Gateway model")
		test.That(t, cfg, test.ShouldBeNil)
	})
}
