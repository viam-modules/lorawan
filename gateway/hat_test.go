package gateway

import (
	"context"
	"testing"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
)

func TestValidate(t *testing.T) {
	// Test valid config with default bus
	resetPin := 25
	conf := &HATConfig{
		BoardName: "pi",
		ResetPin:  &resetPin,
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test valid config with bus=1
	conf = &HATConfig{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Bus:       1,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test missing reset pin
	conf = &HATConfig{
		BoardName: "pi",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "reset_pin"))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid bus value
	conf = &HATConfig{
		BoardName: "pi",
		ResetPin:  &resetPin,
		Bus:       2,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidSpiBus))
	test.That(t, deps, test.ShouldBeNil)

	// Test missing boardName
	conf = &HATConfig{
		ResetPin: &resetPin,
	}

	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "board"))
	test.That(t, deps, test.ShouldBeNil)
}

func TestNewGatewayHAT(t *testing.T) {
	logger := logging.NewTestLogger(t)
	resetPin := 25
	conf := resource.Config{
		Name: "test-gateway",

		API:   sensor.API,
		Model: ModelUSB,
		ConvertedAttributes: &HATConfig{
			BoardName: "pi",
			ResetPin:  &resetPin,
		},
	}

	fakeResetPin := &inject.GPIOPin{}
	fakeResetPin.SetFunc = func(ctx context.Context, high bool, extra map[string]interface{}) error {
		return nil
	}
	b := &inject.Board{}
	b.GPIOPinByNameFunc = func(pin string) (board.GPIOPin, error) {
		return fakeResetPin, nil
	}

	deps := make(resource.Dependencies)
	deps[board.Named("pi")] = b

	gw, err := NewGatewayHAT(context.Background(), deps, conf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, gw, test.ShouldNotBeNil)

	gateway := gw.(*gateway)
	test.That(t, gateway.spi, test.ShouldBeTrue)
	test.That(t, gateway.started, test.ShouldBeTrue)
}
