package draginolht65n

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/encoder"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
)

const (
	// OTAA test values.
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// Gateway dependency.
	testGatewayName = "gateway"
)

var testNodeReadings = map[string]interface{}{"reading": 1}
var testInterval = 5.0

func createMockGateway() *inject.Sensor {
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		if _, ok := cmd[node.GatewaySendDownlinkKey]; ok {
			return map[string]interface{}{node.GatewaySendDownlinkKey: "downlink added"}, nil
		}
		return map[string]interface{}{}, nil
	}
	mockGateway.ReadingsFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		readings := make(map[string]interface{})
		readings["test-lht65n"] = testNodeReadings
		return readings, nil
	}
	return mockGateway
}

func TestNewLHT65N(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	tmpDir := t.TempDir()
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, decoderFilename)
	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	// Create the file, as http tests were already tested in node_test.go
	file, err := os.Create(testDecoderPath)
	test.That(t, err, test.ShouldBeNil)
	defer file.Close()

	// Test OTAA config
	validConf := resource.Config{
		Name: "test-lht65n",
		ConvertedAttributes: &Config{
			Interval: &testInterval,
			JoinType: node.JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newLHT65N(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	// Readings should behave the same
	readings, err := n.Readings(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldEqual, testNodeReadings)
}

func TestReadings(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	tmpDir := t.TempDir()
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, decoderFilename)
	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	// Create the file, as http tests were already tested in node_test.go
	file, err := os.Create(testDecoderPath)
	test.That(t, err, test.ShouldBeNil)
	defer file.Close()

	t.Run("Test Good Readings", func(t *testing.T) {
		// Test OTAA config
		validConf := resource.Config{
			Name: "test-lht65n",
			ConvertedAttributes: &Config{
				Interval: &testInterval,
				JoinType: node.JoinTypeOTAA,
				DevEUI:   testDevEUI,
				AppKey:   testAppKey,
			},
		}

		n, err := newLHT65N(ctx, deps, validConf, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, n, test.ShouldNotBeNil)

		readings, err := n.Readings(ctx, nil)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldEqual, testNodeReadings)
	})
	t.Run("Test Bad Readings", func(t *testing.T) {
		// Test OTAA config
		validConf := resource.Config{
			Name: "test-bad-readings",
			ConvertedAttributes: &Config{
				Interval: &testInterval,
				JoinType: node.JoinTypeOTAA,
				DevEUI:   testDevEUI,
				AppKey:   testAppKey,
			},
		}

		n, err := newLHT65N(ctx, deps, validConf, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, n, test.ShouldNotBeNil)

		// If lastReadings is empty and the call is not from data manager, return no error.
		readings, err := n.Readings(ctx, nil)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldResemble, node.NoReadings)

		// If lastReadings is empty and the call is from data manager, return ErrNoCaptureToStore
		_, err = n.Readings(ctx, map[string]interface{}{data.FromDMString: true})
		test.That(t, err, test.ShouldBeError, data.ErrNoCaptureToStore)

		// If data.FromDmString is false, return no error
		_, err = n.Readings(context.Background(), map[string]interface{}{data.FromDMString: false})
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldResemble, node.NoReadings)
	})
}

func TestDoCommand(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	tmpDir := t.TempDir()
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, decoderFilename)
	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	// Create the file, as http tests were already tested in node_test.go
	file, err := os.Create(testDecoderPath)
	test.That(t, err, test.ShouldBeNil)
	defer file.Close()

	validConf := resource.Config{
		Name: "test-lht65n",
		ConvertedAttributes: &Config{
			Interval: &testInterval,
			JoinType: node.JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newLHT65N(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	t.Run("Test successful generic downlink DoCommand that sends to the gateway", func(t *testing.T) {
		// this test case is to test using the default node DoCommand
		req := map[string]interface{}{node.DownlinkKey: "bytes"}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeTrue)
		test.That(t, gatewayResp, test.ShouldEqual, "downlink added")

		// we should not receive a node success message
		nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
		test.That(t, nodeOk, test.ShouldBeFalse)
		test.That(t, nodeResp, test.ShouldBeNil)
	})
	t.Run("Test successful interval downlink DoCommand to Gateway", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		// req := map[string]interface{}{testKey: "", DownlinkKey: "bytes"}
		req := map[string]interface{}{intervalKey: 10.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeTrue)
		test.That(t, gatewayResp, test.ShouldEqual, "downlink added")

		// we should not receive a dragino success message
		nodeResp, nodeOk := resp[intervalKey].(string)
		test.That(t, nodeOk, test.ShouldBeFalse)
		test.That(t, nodeResp, test.ShouldEqual, "")
	})
	t.Run("Test successful interval downlink DoCommand to that returns the payload", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		req := map[string]interface{}{node.TestKey: "", intervalKey: 10.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)
		logger.Info(resp)

		// we should not receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeFalse)
		test.That(t, gatewayResp, test.ShouldEqual, "")

		// we should receive a dragino success message
		nodeResp, nodeOk := resp[intervalKey].(string)
		test.That(t, nodeOk, test.ShouldBeTrue)
		test.That(t, nodeResp, test.ShouldEqual, "01000258") // 600 seconds
	})
	t.Run("Test failed downlink DoCommand due to wrong type", func(t *testing.T) {
		req := map[string]interface{}{intervalKey: false}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err.Error(), test.ShouldContainSubstring, "Error parsing payload, expected float")
	})

	t.Run("Test nil DoCommand returns empty", func(t *testing.T) {
		resp, err := n.DoCommand(ctx, nil)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldBeNil)
	})

}

func TestConfigValidate(t *testing.T) {
	// valid config
	conf := &Config{
		Interval: &testInterval,
		JoinType: node.JoinTypeOTAA,
		DevEUI:   testDevEUI,
		AppKey:   testAppKey,
		Gateways: []string{testGatewayName},
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)
	test.That(t, deps[0], test.ShouldEqual, testGatewayName)
}
