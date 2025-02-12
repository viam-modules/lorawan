package gateway

import (
	"context"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// Model represents a lorawan gateway HAT model.
var ModelHAT = resource.NewModel("viam", "lorawan", "sx1302-gateway")

// HATConfig describes the configuration of the gateway hat
type HATConfig struct {
	Bus       int    `json:"spi_bus,omitempty"`
	PowerPin  *int   `json:"power_en_pin,omitempty"`
	ResetPin  *int   `json:"reset_pin"`
	BoardName string `json:"board"`
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		ModelHAT,
		resource.Registration[sensor.Sensor, *HATConfig]{
			Constructor: NewGatewayHAT,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *HATConfig) Validate(path string) ([]string, error) {
	var deps []string
	if conf.ResetPin == nil {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "reset_pin")
	}
	if conf.Bus != 0 && conf.Bus != 1 {
		return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
	}

	if len(conf.BoardName) == 0 {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}
	deps = append(deps, conf.BoardName)

	return deps, nil
}

// NewGatewayHAT creates a new gateway HAT
func NewGatewayHAT(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {

	g, err := newGatewayHAT(ctx, deps, conf, logger, false)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// NewGatewayHAT creates a new gateway HAT
func newGatewayHAT(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
	test bool,
) (sensor.Sensor, error) {
	g := &gateway{
		Named:   conf.ResourceName().AsNamed(),
		logger:  logger,
		started: false,
		spi:     true,
	}

	if !test {
		// Create or open the file used to save device data across restarts.
		file, err := getDataFile()
		if err != nil {
			return nil, err
		}

		g.dataFile = file
	}

	err := g.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return g, nil
}
