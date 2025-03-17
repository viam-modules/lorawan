package gateway

import (
	"testing"

	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

func TestValidateWaveshareHat(t *testing.T) {
	// Test valid config with default bus
	conf := &ConfigSX1302WaveshareHAT{
		BoardName: "pi",
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test valid config with bus=1
	conf = &ConfigSX1302WaveshareHAT{
		BoardName: "pi",
		Bus:       1,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)

	// Test invalid bus value
	conf = &ConfigSX1302WaveshareHAT{
		BoardName: "pi",
		Bus:       2,
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidSpiBus))
	test.That(t, deps, test.ShouldBeNil)

	// Test missing boardName
	conf = &ConfigSX1302WaveshareHAT{}

	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "board"))
	test.That(t, deps, test.ShouldBeNil)
}
