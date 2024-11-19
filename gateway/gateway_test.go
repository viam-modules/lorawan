package gateway

import (
	"testing"

	"go.viam.com/rdk/resource"
	"go.viam.com/test"
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