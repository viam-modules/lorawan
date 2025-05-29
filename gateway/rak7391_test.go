package gateway

import (
	"testing"

	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

func TestValidateRakConfig(t *testing.T) {
	// Test valid config with one concentrator
	bus := 0
	conf := &ConfigRak7391{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldResemble, []string{"pi"})

	// Test valid config with both concentrators
	bus2 := 1
	conf = &ConfigRak7391{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
		Concentrator2: &ConcentratorConfig{
			Bus: &bus2,
		},
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldResemble, []string{"pi"})

	// Test invalid - no concentrators configured
	conf = &ConfigRak7391{
		BoardName: "pi",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errConcentrators))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid - missing board name
	conf = &ConfigRak7391{
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "board"))
	test.That(t, deps, test.ShouldBeNil)

	// Test invalid region code
	conf = &ConfigRak7391{
		BoardName: "pi",
		Concentrator1: &ConcentratorConfig{
			Bus: &bus,
		},
		Region: "INVALID",
	}
	deps, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", errInvalidRegion))
	test.That(t, deps, test.ShouldBeNil)

	// Test valid region codes
	validRegions := []string{"US915", "EU868", "US", "EU"}
	for _, region := range validRegions {
		conf = &ConfigRak7391{
			BoardName: "pi",
			Concentrator1: &ConcentratorConfig{
				Bus: &bus,
			},
			Region: region,
		}
		deps, err = conf.Validate("")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, deps, test.ShouldResemble, []string{"pi"})
	}
}
