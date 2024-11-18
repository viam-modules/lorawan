package gateway

import (
	"testing"

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
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "reset pin is required")

	// Test invalid bus value
	conf = &Config{
		ResetPin: &resetPin,
		Bus:      2,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "spi bus can be 0 or 1")
}
