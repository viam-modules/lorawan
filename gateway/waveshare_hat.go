// Package gateway implements the sx1302 gateway module.
package gateway

import (
	"context"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	waveshareHatResetPin = 23
	waveshareHatPowerPin = 18
)

// ConfigSX1302WaveshareHAT describes the configuration of the gateway.
type ConfigSX1302WaveshareHAT struct {
	Bus       int    `json:"spi_bus,omitempty"`
	BoardName string `json:"board"`
	Region    string `json:"region_code"`
}

func (conf *ConfigSX1302WaveshareHAT) getGatewayConfig() *Config {
	powerPin := waveshareHatPowerPin
	resetPin := waveshareHatResetPin
	return &Config{Bus: &conf.Bus, BoardName: conf.BoardName, PowerPin: &powerPin, ResetPin: &resetPin, Region: conf.Region}
}

// Validate ensures all parts of the config are valid.
func (conf *ConfigSX1302WaveshareHAT) Validate(path string) ([]string, error) {
	gatewayConf := conf.getGatewayConfig()
	return gatewayConf.Validate(path)
}

func newSX1302WaveshareHAT(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	return NewGateway(ctx, deps, conf, logger)
}
