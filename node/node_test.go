package node

import (
	"context"
	"encoding/hex"
	"testing"

	"go.viam.com/rdk/components/encoder"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
)

const (

	// Common test values.
	testDecoderPath = "/path/to/decoder"

	// OTAA test values.
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// ABP test values.
	testDevAddr = "01234567"
	testAppSKey = "0123456789ABCDEF0123456789ABCDEE"
	testNwkSKey = "0123456789ABCDEF0123456789ABCDEF"

	// Gateway dependency.
	testGatewayName = "gateway"
)

var (
	testInterval     = 5.0
	testNodeReadings = map[string]interface{}{"reading": 1}
)

func createMockGateway() *inject.Sensor {
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		return map[string]interface{}{}, nil
	}
	mockGateway.ReadingsFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		readings := make(map[string]interface{})
		readings["test-node"] = testNodeReadings
		otherNodeReadings := make(map[string]interface{})
		otherNodeReadings["reading"] = "fake"
		readings["other-node"] = otherNodeReadings
		return readings, nil
	}
	return mockGateway
}

func TestConfigValidate(t *testing.T) {
	// valid config
	conf := &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		DevEUI:      testDevEUI,
		AppKey:      testAppKey,
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldBeNil)

	// Test missing decoder path
	conf = &Config{
		Interval: &testInterval,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errDecoderPathRequired))

	// Test missing interval
	conf = &Config{
		DecoderPath: testDecoderPath,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errIntervalRequired))

	zeroInterval := 0.0
	// Test zero interval
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &zeroInterval,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errIntervalZero))

	// Test invalid join type
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    "INVALID",
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidJoinType))
}

func TestValidateOTAAAttributes(t *testing.T) {
	// Test missing DevEUI
	conf := &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		AppKey:      testAppKey,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errDevEUIRequired))

	// Test invalid DevEUI length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		DevEUI:      "0123456", // Not 8 bytes
		AppKey:      testAppKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errDevEUILength))

	// Test missing AppKey
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		DevEUI:      testDevEUI,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errAppKeyRequired))

	// Test invalid AppKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		DevEUI:      testDevEUI,
		AppKey:      "0123456", // Not 16 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errAppKeyLength))

	// Test valid OTAA config
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeOTAA,
		DevEUI:      testDevEUI,
		AppKey:      testAppKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
}

func TestValidateABPAttributes(t *testing.T) {
	// Test missing AppSKey
	conf := &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		NwkSKey:     testNwkSKey,
		DevAddr:     testDevAddr,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errAppSKeyRequired))

	// Test invalid AppSKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     "0123456", // Not 16 bytes
		NwkSKey:     testNwkSKey,
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errAppSKeyLength))

	// Test missing NwkSKey
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     testAppSKey,
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errNwkSKeyRequired))

	// Test invalid NwkSKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     "0123456", // Not 16 bytes
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errNwkSKeyLength))

	// Test missing DevAddr
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errDevAddrRequired))

	// Test invalid DevAddr length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
		DevAddr:     "0123", // Not 4 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errDevAddrLength))

	// Test valid ABP config
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    joinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
}

func TestNewNode(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	// Test OTAA config
	validConf := resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			DecoderPath: testDecoderPath,
			Interval:    &testInterval,
			JoinType:    joinTypeOTAA,
			DevEUI:      testDevEUI,
			AppKey:      testAppKey,
		},
	}

	n, err := newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node := n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node")
	test.That(t, node.JoinType, test.ShouldEqual, joinTypeOTAA)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Test with valid ABP config
	validABPConf := resource.Config{
		Name: "test-node-abp",
		ConvertedAttributes: &Config{
			DecoderPath: testDecoderPath,
			Interval:    &testInterval,
			JoinType:    joinTypeABP,
			AppSKey:     testAppSKey,
			NwkSKey:     testNwkSKey,
			DevAddr:     testDevAddr,
		},
	}

	n, err = newNode(ctx, deps, validABPConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node = n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node-abp")
	test.That(t, node.JoinType, test.ShouldEqual, joinTypeABP)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Verify ABP byte arrays
	expectedDevAddr, err := hex.DecodeString(testDevAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.Addr, test.ShouldResemble, expectedDevAddr)

	expectedAppSKey, err := hex.DecodeString(testAppSKey)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.AppSKey, test.ShouldResemble, expectedAppSKey)
}

func TestReadings(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	validConf := resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			DecoderPath: testDecoderPath,
			Interval:    &testInterval,
			JoinType:    joinTypeOTAA,
			DevEUI:      testDevEUI,
			AppKey:      testAppKey,
		},
	}

	n, err := newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	readings, err := n.Readings(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldEqual, testNodeReadings)
}
