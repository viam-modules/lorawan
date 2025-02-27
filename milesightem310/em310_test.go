package milesightem310

import (
	"testing"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/test"
)

const (
	// OTAA test values.
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// Gateway dependency.
	testGatewayName = "gateway"
)

var testInterval = 5.0

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
