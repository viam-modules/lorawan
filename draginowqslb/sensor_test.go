package draginowqslb

import (
	"context"
	"path"
	"testing"

	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/testutils"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
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

	t.Run("Test pH calibration commands", func(t *testing.T) {
		// Test valid pH values
		validPHValues := []float64{4, 6, 9}
		expectedPayloads := []string{"FB04", "FB06", "FB09"}

		for i, ph := range validPHValues {
			req := map[string]interface{}{node.TestKey: "", "calibrate_ph": ph}
			resp, err := n.DoCommand(ctx, req)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, resp, test.ShouldNotBeNil)

			nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
			test.That(t, nodeOk, test.ShouldBeTrue)
			payload, ok := nodeResp[testNodeName].(string)
			test.That(t, ok, test.ShouldBeTrue)
			test.That(t, payload, test.ShouldEqual, expectedPayloads[i])
		}

		// Test invalid pH value
		req := map[string]interface{}{node.TestKey: "", "calibrate_ph": 5.5}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "unexpected ph calibration value")

		// Test invalid type
		req = map[string]interface{}{"calibrate_ph": "4"}
		resp, err = n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "expected float64")
	})

	t.Run("Test EC calibration commands", func(t *testing.T) {
		// Test valid EC values
		validECValues := []float64{1, 10}
		expectedPayloads := []string{"FD01", "FD10"}

		for i, ec := range validECValues {
			req := map[string]interface{}{node.TestKey: "", "calibrate_ec": ec}
			resp, err := n.DoCommand(ctx, req)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, resp, test.ShouldNotBeNil)

			nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
			test.That(t, nodeOk, test.ShouldBeTrue)
			payload, ok := nodeResp[testNodeName].(string)
			test.That(t, ok, test.ShouldBeTrue)
			test.That(t, payload, test.ShouldEqual, expectedPayloads[i])
		}

		// Test invalid EC value
		req := map[string]interface{}{node.TestKey: "", "calibrate_ec": 5.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "unexpected electrical conductivity calibration value")

		// Test invalid type
		req = map[string]interface{}{"calibrate_ec": "1"}
		resp, err = n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "expected float64")
	})

	t.Run("Test turbidity calibration commands", func(t *testing.T) {
		// Test valid turbidity values
		validTValues := []float64{0, 200, 400, 600, 800, 1000}
		expectedPayloads := []string{"FE00", "FE02", "FE04", "FE06", "FE08", "FE0A"}

		for i, turb := range validTValues {
			req := map[string]interface{}{node.TestKey: "", "calibrate_t": turb}
			resp, err := n.DoCommand(ctx, req)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, resp, test.ShouldNotBeNil)

			nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
			test.That(t, nodeOk, test.ShouldBeTrue)
			payload, ok := nodeResp[testNodeName].(string)
			test.That(t, ok, test.ShouldBeTrue)
			test.That(t, payload, test.ShouldEqual, expectedPayloads[i])
		}

		// Test invalid turbidity value
		req := map[string]interface{}{node.TestKey: "", "calibrate_t": 300.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "unexpected turbidity calibration value")

		// Test invalid type
		req = map[string]interface{}{"calibrate_t": "200"}
		resp, err = n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "expected float64")
	})

	t.Run("Test ORP calibration commands", func(t *testing.T) {
		// Test valid ORP values
		validORPValues := []float64{86, 256}
		expectedPayloads := []string{"FC0056", "FC0100"}

		for i, orp := range validORPValues {
			req := map[string]interface{}{node.TestKey: "", "calibrate_orp": orp}
			resp, err := n.DoCommand(ctx, req)
			test.That(t, err, test.ShouldBeNil)
			test.That(t, resp, test.ShouldNotBeNil)

			nodeResp, nodeOk := resp[node.DownlinkKey].(map[string]interface{})
			test.That(t, nodeOk, test.ShouldBeTrue)
			payload, ok := nodeResp[testNodeName].(string)
			test.That(t, ok, test.ShouldBeTrue)
			test.That(t, payload, test.ShouldEqual, expectedPayloads[i])
		}

		// Test invalid ORP value
		req := map[string]interface{}{node.TestKey: "", "calibrate_orp": 100.0}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "unexpected orp calibration value")

		// Test invalid type
		req = map[string]interface{}{"calibrate_orp": "86"}
		resp, err = n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "expected float64")
	})

	t.Run("Test successful reset downlink DoCommand to that returns the payload", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		req := map[string]interface{}{node.TestKey: "", node.ResetKey: ""}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)
		logger.Info(resp)

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
