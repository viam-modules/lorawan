package gateway

import (
	"context"
	"testing"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

func TestUSBValidate(t *testing.T) {
	conf := &USBConfig{
		Path: "/some/path",
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)

	conf = &USBConfig{}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationFieldRequiredError("", "serial_path"))
}

func TestNewUSBGateway(t *testing.T) {
	// Create a test configuration
	conf := resource.Config{
		Name: "test-gateway",

		API:   sensor.API,
		Model: ModelUSB,
		ConvertedAttributes: &USBConfig{
			Path: "/path",
		},
	}

	deps := resource.Dependencies{}
	logger := logging.NewTestLogger(t)

	gw, err := newUSBGateway(context.Background(), deps, conf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, gw, test.ShouldNotBeNil)

	gateway := gw.(*gateway)
	test.That(t, gateway.spi, test.ShouldBeFalse)
}
