package gateway

import (
	"context"
	"fmt"

	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	rak7391ResetPin1 = 17 // Default reset pin for first concentrator
	rak7391ResetPin2 = 6  // Default reset pin for second concentrator
)

// ConnectionType defines the type of connection for the RAK gateway
type ConnectionType string

const (
	SPIConnection ConnectionType = "spi"
	USBConnection ConnectionType = "usb"
)

// ConcentratorConfig describes the configuration for a single concentrator
type ConcentratorConfig struct {
	ConnectionType string `json:"connection_type"`
	Path           string `json:"path"`
}

// ConfigRak describes the configuration of the RAK gateway
type ConfigRak struct {
	Concentrator1 *ConcentratorConfig `json:"pcie1,omitempty"`
	Concentrator2 *ConcentratorConfig `json:"pcie2,omitempty"`
	BoardName     string              `json:"board"`
	Region        string              `json:"region_code,omitempty"`
}

func (conf *ConfigRak) getGatewayConfig() *Config {
	// Configure reset pins based on enabled concentrators
	var resetPin int

	return &Config{
		Path:      path,
		BoardName: conf.BoardName,
		PowerPin:  nil,
		ResetPin:  &resetPin,
		Region:    conf.Region,
	}
}

// validateConcentrator validates a single concentrator configuration
func validateConcentrator(c ConcentratorConfig, path string, num int) error {
	if c.ConnectionType != "usb" && c.ConnectionType != "spi" {
		return resource.NewConfigValidationError(path, fmt.Errorf("pcie%d connection type must be usb or spi", num))
	}
	if c.Path == "" {
		return resource.NewConfigValidationFieldRequiredError(path, fmt.Sprintf("pcie%d.path", num))
	}

	return nil
}

// Validate ensures all parts of the config are valid
func (conf *ConfigRak) Validate(path string) ([]string, error) {
	if conf.Concentrator1 == nil && conf.Concentrator2 == nil {
		return nil, resource.NewConfigValidationError(path, fmt.Errorf("must configure at least one pcie concentrator"))
	}

	if conf.BoardName == "" {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}

	if conf.Region != "" {
		if regions.GetRegion(conf.Region) == regions.Unspecified {
			return nil, resource.NewConfigValidationError(path, errInvalidRegion)
		}
	}

	//gatewayConf := conf.getGatewayConfig()
	//_, err := gatewayConf.Validate(path)

	return []string{conf.BoardName}, nil
}

func newRAK(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	return NewGateway(ctx, deps, conf, logger)
}
