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
	// Join types
	testJoinTypeOTAA = "OTAA"
	testJoinTypeABP  = "ABP"

	// Common test values
	testDecoderPath = "/path/to/decoder"

	// OTAA test values
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// ABP test values
	testDevAddr = "01234567"
	testAppSKey = "0123456789ABCDEF0123456789ABCDEE"
	testNwkSKey = "0123456789ABCDEF0123456789ABCDEF"

	// Gateway dependency
	testGatewayName = "gateway"
)

var testInterval = 5

func TestConfigValidate(t *testing.T) {
	// valid config
	conf := &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
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
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "decoder path is required")

	// Test missing interval
	conf = &Config{
		DecoderPath: testDecoderPath,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "uplink_interval_mins is required")

	zeroInterval := 0

	// Test zero interval
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &zeroInterval,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "uplink_interval_mins cannot be zero")

	// Test invalid join type
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    "INVALID",
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "join type is OTAA or ABP")
}

func TestValidateOTAAAttributes(t *testing.T) {
	// Test missing DevEUI
	conf := &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
		AppKey:      testAppKey,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "dev EUI is required")

	// Test invalid DevEUI length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
		DevEUI:      "0123456", // Not 8 bytes
		AppKey:      testAppKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "dev EUI must be 8 bytes")

	// Test missing AppKey
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
		DevEUI:      testDevEUI,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "app key is required")

	// Test invalid AppKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
		DevEUI:      testDevEUI,
		AppKey:      "0123456", // Not 16 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "app key must be 16 bytes")

	// Test valid OTAA config
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeOTAA,
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
		JoinType:    testJoinTypeABP,
		NwkSKey:     testNwkSKey,
		DevAddr:     testDevAddr,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "app session key is required")

	// Test invalid AppSKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
		AppSKey:     "0123456", // Not 16 bytes
		NwkSKey:     testNwkSKey,
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "app session key must be 16 bytes")

	// Test missing NwkSKey
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
		AppSKey:     testAppSKey,
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "network session key is required")

	// Test invalid NwkSKey length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     "0123456", // Not 16 bytes
		DevAddr:     testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "network session key must be 16 bytes")

	// Test missing DevAddr
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "device address is required")

	// Test invalid DevAddr length
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
		AppSKey:     testAppSKey,
		NwkSKey:     testNwkSKey,
		DevAddr:     "0123", // Not 4 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "device address must be 4 bytes")

	// Test valid ABP config
	conf = &Config{
		DecoderPath: testDecoderPath,
		Interval:    &testInterval,
		JoinType:    testJoinTypeABP,
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

	// Create mock gateway dependency
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		return map[string]interface{}{}, nil
	}
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	// Test OTAA config
	validConf := resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			DecoderPath: testDecoderPath,
			Interval:    &testInterval,
			JoinType:    testJoinTypeOTAA,
			DevEUI:      testDevEUI,
			AppKey:      testAppKey,
		},
	}

	n, err := newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node := n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node")
	test.That(t, node.JoinType, test.ShouldEqual, testJoinTypeOTAA)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Test with valid ABP config
	validABPConf := resource.Config{
		Name: "test-node-abp",
		ConvertedAttributes: &Config{
			DecoderPath: testDecoderPath,
			Interval:    &testInterval,
			JoinType:    testJoinTypeABP,
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
	test.That(t, node.JoinType, test.ShouldEqual, testJoinTypeABP)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Verify ABP byte arrays
	expectedDevAddr, err := hex.DecodeString(testDevAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.Addr, test.ShouldResemble, expectedDevAddr)

	expectedAppSKey, err := hex.DecodeString(testAppSKey)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.AppSKey, test.ShouldResemble, expectedAppSKey)
}
