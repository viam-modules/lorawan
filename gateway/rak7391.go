package gateway

import (
	"context"
	"errors"

	"github.com/viam-modules/lorawan/regions"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var errConcentrators = errors.New("must configure at least one pcie concentrator")

var (
	resetPin1 = 17
	resetPin2 = 6
)

// ConfigRak7391 describes the configuration of the rak gateway.
type ConfigRak7391 struct {
	Concentrator1 *ConcentratorConfig `json:"pcie1,omitempty"`
	Concentrator2 *ConcentratorConfig `json:"pcie2,omitempty"`
	BoardName     string              `json:"board"`
	Region        string              `json:"region_code,omitempty"`
}

func (conf *ConfigRak7391) getGatewayConfig() *ConfigMultiConcentrator {
	cfg := &ConfigMultiConcentrator{Region: conf.Region, BoardName: conf.BoardName}
	cfg.Concentrators = []*ConcentratorConfig{}

	dualConcentrator := false
	if conf.Concentrator1 != nil && conf.Concentrator2 != nil {
		dualConcentrator = true
	}

	if conf.Concentrator1 != nil {
		conf.Concentrator1.ResetPin = &resetPin1
		conf.Concentrator1.Name = "pcie1"
		cfg.Concentrators = append(cfg.Concentrators, conf.Concentrator1)
	}
	if conf.Concentrator2 != nil {
		conf.Concentrator2.ResetPin = &resetPin2
		conf.Concentrator2.Name = "pcie2"
		// If both concentrators in use, use channels 8-15, otherwise use 0-7
		if dualConcentrator {
			conf.Concentrator2.BaseChannel = 8
		}
		cfg.Concentrators = append(cfg.Concentrators, conf.Concentrator2)
	}

	return cfg
}

// Validate ensures all parts of the config are valid.
func (conf *ConfigRak7391) Validate(path string) ([]string, error) {
	if conf.Concentrator1 == nil && conf.Concentrator2 == nil {
		return nil, resource.NewConfigValidationError(path, errConcentrators)
	}

	if conf.BoardName == "" {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}

	if conf.Concentrator1 != nil {
		if conf.Concentrator1.Bus != nil && *conf.Concentrator1.Bus != 0 && *conf.Concentrator1.Bus != 1 {
			return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
		}
	}

	if conf.Concentrator2 != nil {
		if conf.Concentrator2.Bus != nil && *conf.Concentrator2.Bus != 0 && *conf.Concentrator2.Bus != 1 {
			return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
		}
	}

	if conf.Region != "" {
		if regions.GetRegion(conf.Region) == regions.Unspecified {
			return nil, resource.NewConfigValidationError(path, errInvalidRegion)
		}
	}

	return []string{conf.BoardName}, nil
}

func newRak7391(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	return NewGateway(ctx, deps, conf, logger)
}
