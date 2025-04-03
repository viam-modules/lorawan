// Package testutils creates helper functions for tests
package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
)

const (
	// TestDevEUI is a fake deveui for tests.
	TestDevEUI = "0123456789ABCDEF"
	// TestAppKey is fake app key for tests.
	TestAppKey = "0123456789ABCDEF0123456789ABAAAA"
	// TestDevAddr is fake dev addr for tests.
	TestDevAddr = "01234567"
	// TestAppSKey is fake appskey for tests.
	TestAppSKey = "0123456789ABCDEF0123456789ABCDEE"
	// TestNwkSKey is fake nwkskey for tests.
	TestNwkSKey = "0123456789ABCDEF0123456789ABCDEF"
)

const gatewaySendDownlinkKey = "add_downlink_to_queue"

func createMockGateway(devices []string) *inject.Sensor {
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		if _, ok := cmd[gatewaySendDownlinkKey]; ok {
			return map[string]interface{}{gatewaySendDownlinkKey: "downlink added"}, nil
		}
		if _, ok := cmd["get_device"]; ok {
			resp := map[string]interface{}{}
			resp["get_device"] = map[string]interface{}{
				"app_skey": TestAppSKey,
				"nwk_skey": TestNwkSKey, "dev_eui": TestDevEUI, "min_uplink_interval": 60.0,
				"fcnt_down": 1.0, "dev_addr": TestDevAddr,
			}
			return resp, nil
		}
		return map[string]interface{}{}, nil
	}

	testNodeReadings := map[string]interface{}{"reading": 1}

	mockGateway.ReadingsFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		readings := make(map[string]interface{})
		for _, sensor := range devices {
			readings[sensor] = testNodeReadings
		}

		return readings, nil
	}
	return mockGateway
}

// NewNodeTestEnv creates mock gateway dependencies and a temp decoder file for testing Node functions.
func NewNodeTestEnv(t *testing.T, gateways, nodes []string, decoderFilename string) (resource.Dependencies, string) {
	t.Helper()
	tmpDir := t.TempDir()

	testDecoderPath := filepath.Clean(fmt.Sprintf("%s/%s", tmpDir, decoderFilename))
	path := filepath.Dir(testDecoderPath)
	err := os.MkdirAll(path, 0o700)
	test.That(t, err, test.ShouldBeNil)

	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	// Create the file, as http tests were already tested in node_test.go
	file, err := os.Create(testDecoderPath)
	test.That(t, err, test.ShouldBeNil)
	// swallow the error for closing the file.
	t.Cleanup(func() {
		test.That(t, file.Close(), test.ShouldBeNil)
	})
	deps := make(resource.Dependencies)
	for _, gateway := range gateways {
		deps[sensor.Named(gateway)] = createMockGateway(nodes)
	}
	return deps, tmpDir
}
