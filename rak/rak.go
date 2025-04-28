package rak

import (
	"context"
	"fmt"

	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/node"
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

type RAK7391 struct {
	resource.Named
	resource.AlwaysRebuild
	gw1    sensor.Sensor
	gw2    sensor.Sensor
	logger logging.Logger
}

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string("rak"))

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *ConfigRak]{
			Constructor: newRAK,
		})
}

// getGatewayConfigs returns configurations for enabled concentrators
func (conf *ConfigRak) getGatewayConfigs() []*gateway.Config {
	var configs []*gateway.Config

	if conf.Concentrator1 != nil {
		resetPin1 := rak7391ResetPin1
		configs = append(configs, &gateway.Config{
			Path:      conf.Concentrator1.Path,
			BoardName: conf.BoardName,
			PowerPin:  nil,
			ResetPin:  &resetPin1,
			Region:    conf.Region,
		})
	}

	if conf.Concentrator2 != nil {
		resetPin2 := rak7391ResetPin2
		configs = append(configs, &gateway.Config{
			Path:      conf.Concentrator2.Path,
			BoardName: conf.BoardName,
			PowerPin:  nil,
			ResetPin:  &resetPin2,
			Region:    conf.Region,
		})
	}

	return configs
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

	// if conf.Region != "" {
	// 	if regions.GetRegion(conf.Region) == regions.Unspecified {
	// 		return nil, resource.NewConfigValidationError(path, gateway.ErrInvalidRegion)
	// 	}
	// }

	//gatewayConf := conf.getGatewayConfig()
	//_, err := gatewayConf.Validate(path)

	return []string{conf.BoardName}, nil
}

func newRAK(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	rakConf, err := resource.NativeConfig[*ConfigRak](conf)
	if err != nil {
		return nil, err
	}

	configs := rakConf.getGatewayConfigs()
	if len(configs) == 0 {
		return nil, fmt.Errorf("no concentrators configured")
	}

	// If only one concentrator is configured, return a single gateway
	if len(configs) == 1 {
		return gateway.NewGateway(ctx, deps, conf, logger)
	}

	// Create a multi-gateway wrapper that manages both gateways
	gw1, err := gateway.NewGateway(ctx, deps, resource.Config{
		Name:  conf.ResourceName().Name + "_1",
		API:   conf.API,
		Model: conf.Model,
		Attributes: map[string]interface{}{
			"reset_pin":   *configs[0].ResetPin,
			"board":       configs[0].BoardName,
			"region_code": configs[0].Region,
			"path":        configs[0].Path,
		},
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create first gateway: %w", err)
	}

	gw2, err := gateway.NewGateway(ctx, deps, resource.Config{
		Name:  conf.ResourceName().Name + "_2",
		API:   conf.API,
		Model: conf.Model,
		Attributes: map[string]interface{}{
			"reset_pin":   *configs[1].ResetPin,
			"board":       configs[1].BoardName,
			"region_code": configs[1].Region,
			"path":        configs[1].Path,
		},
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create second gateway: %w", err)
	}

	return &RAK7391{
		gw1:    gw1,
		gw2:    gw2,
		logger: logger,
	}, nil
}

func (r *RAK7391) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	readings1, err1 := r.gw1.Readings(ctx, extra)
	readings2, err2 := r.gw2.Readings(ctx, extra)

	// Combine readings from both gateways
	combined := make(map[string]interface{})
	if err1 == nil {
		for k, v := range readings1 {
			combined["gw1_"+k] = v
		}
	}
	if err2 == nil {
		for k, v := range readings2 {
			combined["gw2_"+k] = v
		}
	}

	if err1 != nil && err2 != nil {
		return combined, fmt.Errorf("both gateways failed: %v, %v", err1, err2)
	}

	return combined, nil
}

func (r *RAK7391) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	// Forward commands to both gateways
	resp1, err1 := r.gw1.DoCommand(ctx, cmd)
	resp2, err2 := r.gw2.DoCommand(ctx, cmd)

	combined := make(map[string]interface{})
	if err1 == nil {
		for k, v := range resp1 {
			combined["gw1_"+k] = v
		}
	}
	if err2 == nil {
		for k, v := range resp2 {
			combined["gw2_"+k] = v
		}
	}

	if err1 != nil && err2 != nil {
		return combined, fmt.Errorf("command failed on both gateways: %v, %v", err1, err2)
	}

	return combined, nil
}

func (r *RAK7391) Close(ctx context.Context) error {
	err1 := r.gw1.Close(ctx)
	err2 := r.gw2.Close(ctx)

	if err1 != nil && err2 != nil {
		return fmt.Errorf("failed to close both gateways: %v, %v", err1, err2)
	}
	if err1 != nil {
		return fmt.Errorf("failed to close first gateway: %v", err1)
	}
	if err2 != nil {
		return fmt.Errorf("failed to close second gateway: %v", err2)
	}
	return nil
}
