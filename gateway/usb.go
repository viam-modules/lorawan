package gateway

import (
	"context"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

type USBConfig struct {
	Path string `json:"serial_path"`
}

// Model represents a lorawan gateway model.
var ModelUSB = resource.NewModel("viam", "lorawan", "sx1302-usb")

// Validate ensures all parts of the config are valid.
func (conf *USBConfig) Validate(path string) ([]string, error) {
	if conf.Path == "" {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "serial_path")
	}
	return nil, nil
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		ModelUSB,
		resource.Registration[sensor.Sensor, *USBConfig]{
			Constructor: newUSBGateway,
		})
}

// NewUSBGateway creates a new gateway
func newUSBGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g := &gateway{
		Named:   conf.ResourceName().AsNamed(),
		logger:  logger,
		started: false,
		spi:     false,
	}

	file, err := getDataFile()
	if err != nil {
		return nil, err
	}

	g.dataFile = file

	err = g.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return g, nil
}
