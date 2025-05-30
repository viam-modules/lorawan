package draginowqslb

import (
	"context"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/testutils"
	"go.viam.com/rdk/components/sensor"
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
	testNodeName    = "test-wqslb"
)

var (
	testNodeReadings = map[string]interface{}{"reading": 1}
	testInterval     = 5.0
	gateways         = []string{testGatewayName}
	nodes            = []string{testNodeName}
)

func TestNewWQSLB(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, _ := testutils.NewNodeTestEnv(t, gateways, nodes, path.Base(decoderURL))

	validConf := resource.Config{
		Name: nodes[0],
		ConvertedAttributes: &Config{
			Interval: &testInterval,
			JoinType: node.JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newWQSLB(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	// Readings should behave the same
	readings, err := n.Readings(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, testNodeReadings)
}

func TestReadings(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, _ := testutils.NewNodeTestEnv(t, gateways, nodes, path.Base(decoderURL))

	t.Run("Test Good Readings", func(t *testing.T) {
		// Test OTAA config
		validConf := resource.Config{
			Name: testNodeName,
			ConvertedAttributes: &Config{
				Interval: &testInterval,
				JoinType: node.JoinTypeOTAA,
				DevEUI:   testDevEUI,
				AppKey:   testAppKey,
			},
		}

		n, err := newWQSLB(ctx, deps, validConf, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, n, test.ShouldNotBeNil)

		readings, err := n.Readings(ctx, nil)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldResemble, testNodeReadings)
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

		n, err := newWQSLB(ctx, deps, validConf, logger)
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
	t.Run("Test Inavlid Readings", func(t *testing.T) {
		// Test OTAA config
		validConf := resource.Config{
			Name: testNodeName,
			ConvertedAttributes: &Config{
				Interval: &testInterval,
				JoinType: node.JoinTypeOTAA,
				DevEUI:   testDevEUI,
				AppKey:   testAppKey,
			},
		}

		n, err := newWQSLB(ctx, deps, validConf, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, n, test.ShouldNotBeNil)

		var dep resource.Resource
		for _, val := range deps {
			dep = val
		}
		gateway, ok := dep.(sensor.Sensor)
		test.That(t, ok, test.ShouldBeTrue)
		injectedGateway, ok := gateway.(*inject.Sensor)
		test.That(t, ok, test.ShouldBeTrue)

		injectedGateway.ReadingsFunc = func(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
			readings := make(map[string]interface{})
			invalidReadings := map[string]interface{}{
				orpKey: -100000.0, // invalid orp
				phKey:  7.0,
			}
			readings[testNodeName] = invalidReadings
			return readings, nil
		}

		// if From DM should remove invalid reading
		expectedReadings := map[string]interface{}{
			phKey: 7.0,
		}
		readings, err := n.Readings(ctx, map[string]interface{}{data.FromDMString: true})
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldResemble, expectedReadings)

		// If data.FromDmString is false, invalid reading should be "INVALID" string
		// if From DM should remove invalid reading
		expectedReadings = map[string]interface{}{
			orpKey: "INVALID",
			phKey:  7.0,
		}

		readings, err = n.Readings(context.Background(), map[string]interface{}{data.FromDMString: false})
		test.That(t, err, test.ShouldBeNil)
		test.That(t, readings, test.ShouldResemble, expectedReadings)
	})
}

func TestDoCommand(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, _ := testutils.NewNodeTestEnv(t, gateways, nodes, path.Base(decoderURL))

	validConf := resource.Config{
		Name: testNodeName,
		ConvertedAttributes: &Config{
			Interval: &testInterval,
			JoinType: node.JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newWQSLB(ctx, deps, validConf, logger)
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
		req := map[string]interface{}{node.IntervalKey: 10.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeTrue)
		test.That(t, gatewayResp, test.ShouldEqual, "downlink added")

		// we should not receive a dragino success message
		nodeResp, nodeOk := resp[node.IntervalKey].(string)
		test.That(t, nodeOk, test.ShouldBeFalse)
		test.That(t, nodeResp, test.ShouldEqual, "")
	})
	t.Run("Test successful interval downlink DoCommand to that returns the payload", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		req := map[string]interface{}{node.TestKey: "", node.IntervalKey: 10.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)
		logger.Info(resp)

		// we should not receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeFalse)
		test.That(t, gatewayResp, test.ShouldEqual, "")

		// we should receive a dragino success message
		nodeResp, nodeOk := resp[node.IntervalKey].(string)
		test.That(t, nodeOk, test.ShouldBeTrue)
		test.That(t, nodeResp, test.ShouldEqual, "01000258") // 600 seconds
	})
	t.Run("Test failed downlink DoCommand due to wrong type", func(t *testing.T) {
		req := map[string]interface{}{node.IntervalKey: false}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err.Error(), test.ShouldContainSubstring, "error parsing payload, expected float")
	})

	t.Run("Test nil DoCommand returns empty", func(t *testing.T) {
		resp, err := n.DoCommand(ctx, nil)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldBeNil)
	})

	// Helper function to test calibration commands
	testCalibration := func(t *testing.T, calibType string, validValues []float64, expectedPayloads []string, invalidValue float64) {
		t.Helper()
		cmdKey := "calibrate_" + calibType

		t.Run(fmt.Sprintf("Test %s calibration commands", strings.ToUpper(calibType)), func(t *testing.T) {
			// Test valid values
			for i, val := range validValues {
				req := map[string]interface{}{node.TestKey: "", cmdKey: val}
				resp, err := n.DoCommand(ctx, req)
				test.That(t, err, test.ShouldBeNil)
				test.That(t, resp, test.ShouldNotBeNil)

				nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
				test.That(t, nodeOk, test.ShouldBeTrue)
				payload, ok := nodeResp[testNodeName].(string)
				test.That(t, ok, test.ShouldBeTrue)
				test.That(t, payload, test.ShouldEqual, expectedPayloads[i])
			}

			// Test invalid value
			req := map[string]interface{}{node.TestKey: "", cmdKey: invalidValue}
			resp, err := n.DoCommand(ctx, req)
			test.That(t, resp, test.ShouldBeEmpty)
			test.That(t, err, test.ShouldNotBeNil)

			// Test invalid type
			req = map[string]interface{}{cmdKey: fmt.Sprintf("%v", validValues[0])}
			resp, err = n.DoCommand(ctx, req)
			test.That(t, resp, test.ShouldBeEmpty)
			test.That(t, err, test.ShouldNotBeNil)
			test.That(t, err.Error(), test.ShouldContainSubstring, "expected float64")
		})
	}

	// Test pH calibration
	testCalibration(t, "ph", []float64{4, 6, 9}, []string{"FB04", "FB06", "FB09"}, 5.5)
	// Test EC calibration
	testCalibration(t, "ec", []float64{1, 10}, []string{"FD01", "FD10"}, 5.0)
	// Test turbidity calibration
	testCalibration(t, "t", []float64{0, 200, 400, 600, 800, 1000}, []string{"FE00", "FE02", "FE04", "FE06", "FE08", "FE0A"}, 300.0)
	// Test ORP calibration
	testCalibration(t, "orp", []float64{86, 256}, []string{"FC0056", "FC0100"}, 100.0)

	t.Run("Test successful reset downlink DoCommand to that returns the payload", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		req := map[string]interface{}{node.TestKey: "", node.ResetKey: ""}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should not receive a success from the gateway
		gatewayResp, gatewayOk := resp[node.GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeFalse)
		test.That(t, gatewayResp, test.ShouldEqual, "")

		// we should receive a dragino success message
		nodeResp, nodeOk := resp[node.ResetKey].(string)
		test.That(t, nodeOk, test.ShouldBeTrue)
		test.That(t, nodeResp, test.ShouldEqual, "04FFFF") // reset
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

func TestSanitizeReading(t *testing.T) {
	t.Run("valid readings remain unchanged", func(t *testing.T) {
		reading := map[string]interface{}{
			phKey:    7.0,    // valid pH
			ecK10Key: 1000.0, // valid EC K10
		}
		extra := map[string]interface{}{}

		result := sanitizeReading(reading, extra, probeRange{key: phKey, min: phMin, max: phMax})
		test.That(t, result[phKey], test.ShouldEqual, 7.0)

		result = sanitizeReading(reading, extra, probeRange{key: ecK10Key, min: ecK10Min, max: ecK10Max})
		test.That(t, result[ecK10Key], test.ShouldEqual, 1000.0)
	})

	t.Run("invalid readings in data capture mode are deleted", func(t *testing.T) {
		reading := map[string]interface{}{
			phKey:    15.0,    // invalid pH
			ecK10Key: 25000.0, // invalid EC K10
		}
		extra := map[string]interface{}{
			data.FromDMString: true,
		}

		result := sanitizeReading(reading, extra, probeRange{key: phKey, min: phMin, max: phMax})
		_, exists := result[phKey]
		test.That(t, exists, test.ShouldBeFalse)

		result = sanitizeReading(reading, extra, probeRange{key: ecK10Key, min: ecK10Min, max: ecK10Max})
		_, exists = result[ecK10Key]
		test.That(t, exists, test.ShouldBeFalse)
	})

	t.Run("invalid readings in normal mode are marked INVALID", func(t *testing.T) {
		reading := map[string]interface{}{
			phKey:   -1.0,    // invalid pH
			ecK1Key: 19000.0, // invalid EC K1
		}
		extra := map[string]interface{}{}

		result := sanitizeReading(reading, extra, probeRange{key: phKey, min: phMin, max: phMax})
		test.That(t, result[phKey], test.ShouldEqual, "INVALID")

		result = sanitizeReading(reading, extra, probeRange{key: ecK1Key, min: ecK1Min, max: ecK1Max})
		test.That(t, result[ecK1Key], test.ShouldEqual, "INVALID")
	})

	t.Run("edge cases at boundaries remain valid", func(t *testing.T) {
		reading := map[string]interface{}{
			phKey:    phMax,    // max pH
			ecK10Key: ecK10Min, // min EC K10
		}
		extra := map[string]interface{}{}

		result := sanitizeReading(reading, extra, probeRange{key: phKey, min: phMin, max: phMax})
		test.That(t, result[phKey], test.ShouldEqual, phMax)

		result = sanitizeReading(reading, extra, probeRange{key: ecK10Key, min: ecK10Min, max: ecK10Max})
		test.That(t, result[ecK10Key], test.ShouldEqual, ecK10Min)
	})

	t.Run("non-float64 values are ignored", func(t *testing.T) {
		reading := map[string]interface{}{
			phKey:    "not a number",
			ecK10Key: true,
		}
		extra := map[string]interface{}{}

		result := sanitizeReading(reading, extra, probeRange{key: phKey, min: phMin, max: phMax})
		test.That(t, result[phKey], test.ShouldEqual, "not a number")

		result = sanitizeReading(reading, extra, probeRange{key: ecK10Key, min: ecK10Min, max: ecK10Max})
		test.That(t, result[ecK10Key], test.ShouldEqual, true)
	})
}
