package node

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

func createMockGateway(devices []string) *inject.Sensor {
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		if _, ok := cmd[GatewaySendDownlinkKey]; ok {
			return map[string]interface{}{GatewaySendDownlinkKey: "downlink added"}, nil
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

func NewNodeTestEnv(t *testing.T, gateways []string, nodes []string, decoderFilename string) (resource.Dependencies, string) {
	// t.Helper()
	tmpDir := t.TempDir()

	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, decoderFilename)
	path := filepath.Dir(testDecoderPath)
	err := os.MkdirAll(path, 0o755)
	test.That(t, err, test.ShouldBeNil)

	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	// Create the file, as http tests were already tested in node_test.go
	file, err := os.Create(testDecoderPath)
	test.That(t, err, test.ShouldBeNil)
	// swallow the error for closing the file.
	t.Cleanup(func() {
		file.Close()
	})
	deps := make(resource.Dependencies)
	for _, gateway := range gateways {
		deps[sensor.Named(gateway)] = createMockGateway(nodes)
	}
	return deps, tmpDir
}
