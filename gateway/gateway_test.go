package gateway

import (
	"context"
	"gateway/node"
	"testing"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
	"go.viam.com/utils/protoutils"
)

func TestValidate(t *testing.T) {
	// Test valid config with default bus
	resetPin := 25
	conf := &Config{
		ResetPin: &resetPin,
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldBeNil)

	// Test valid config with bus=1
	conf = &Config{
		ResetPin: &resetPin,
		Bus:      1,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldBeNil)

	// Test missing reset pin
	conf = &Config{}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errResetPinRequired))

	// Test invalid bus value
	conf = &Config{
		ResetPin: &resetPin,
		Bus:      2,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidSpiBus))
}

func TestDoCommand(t *testing.T) {
	g := &gateway{
		devices: make(map[string]*node.Node),
	}

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
	test.That(t, retVal.(int), test.ShouldEqual, 1)

	// need to simulate what happens when the DoCommand message is serialized/deserialized into proto
	doOverWire := func(gateway sensor.Sensor, cmd map[string]interface{}) {
		command, err := protoutils.StructToStructPb(cmd)
		test.That(t, err, test.ShouldBeNil)
		_, err = gateway.DoCommand(context.Background(), command.AsMap())
		test.That(t, err, test.ShouldBeNil)
	}

	// Test Register device command
	registerCmd := make(map[string]interface{})
	registerCmd["register_device"] = n
	doOverWire(s, registerCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 1)
	dev, ok := g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, dev.AppKey, test.ShouldResemble, testAppKey)
	test.That(t, dev.DevEui, test.ShouldResemble, testDevEUI)
	test.That(t, dev.DecoderPath, test.ShouldResemble, testDecoderPath)

	// Test that if the device reconfigures, fields get updated
	n.DecoderPath = "/newpath"
	registerCmd["register_device"] = n
	doOverWire(s, registerCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 1)
	dev, ok = g.devices[testNodeName]
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, dev.AppKey, test.ShouldResemble, testAppKey)
	test.That(t, dev.DevEui, test.ShouldResemble, testDevEUI)
	test.That(t, dev.DecoderPath, test.ShouldResemble, "/newpath")

	// Test remove device command
	removeCmd := make(map[string]interface{})
	removeCmd["remove_device"] = n.NodeName
	doOverWire(s, removeCmd)
	test.That(t, len(g.devices), test.ShouldEqual, 0)
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
	expectedReadings := map[string]interface{}{
		"temp": 25.5,
		"hum":  60,
	}
	g.updateReadings("device1", expectedReadings)
	test.That(t, g.lastReadings["device1"], test.ShouldResemble, expectedReadings)

	// Test updating readings of a device that contains readings already.
	newReading := map[string]interface{}{
		"temp": 26.5,
		"pres": 1013,
	}

	g.updateReadings("device1", newReading)
	expectedReadings = map[string]interface{}{
		"temp": 26.5,
		"hum":  60,
		"pres": 1013,
	}
	test.That(t, g.lastReadings["device1"], test.ShouldResemble, expectedReadings)
}

func TestReadings(t *testing.T) {
	g := &gateway{
		lastReadings: map[string]interface{}{
			"device1": map[string]interface{}{
				"temp": 25.5,
				"hum":  60,
			},
			"device2": map[string]interface{}{
				"pres": 1013,
			},
		},
	}

	readings, err := g.Readings(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, g.lastReadings)
}
