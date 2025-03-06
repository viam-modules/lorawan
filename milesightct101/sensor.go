// Package milesightct101 implements the Milesight-CT101 model
package milesightct101

import (
	"context"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	decoderURL = "https://raw.githubusercontent.com/Milesight-IoT/SensorDecoders/40e844fedbcf9a8c3b279142672fab1c89bee2e0/" +
		"CT_Series/CT101/CT101_Decoder.js"
	// OTAA defaults.
	defaultAppKey = "5572404C696E6B4C6F52613230313823"
	// ABP defaults.
	defaultNwkSKey = "5572404C696E6B4C6F52613230313823"
	defaultAppSKey = "5572404C696E6B4C6F52613230313823"
)

// defaultIntervalMin is how often the CT101 will send an uplink.
var defaultIntervalMin = 10. // minutes

// Model represents a Milesight-CT101 lorawan node model.
var Model = node.LorawanFamily.WithModel("milesight-ct101")

// Config defines the Milesight-CT101's config.
type Config struct {
	JoinType string   `json:"join_type,omitempty"`
	Interval *float64 `json:"uplink_interval_mins"`
	DevEUI   string   `json:"dev_eui,omitempty"`
	AppKey   string   `json:"app_key,omitempty"`
	AppSKey  string   `json:"app_s_key,omitempty"`
	NwkSKey  string   `json:"network_s_key,omitempty"`
	DevAddr  string   `json:"dev_addr,omitempty"`
	Gateways []string `json:"gateways"`
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newCT101,
		})
}

func (conf *Config) getNodeConfig() node.Config {
	appKey := defaultAppKey
	if conf.AppKey != "" {
		appKey = conf.AppKey
	}
	nwkSKey := defaultNwkSKey
	if conf.NwkSKey != "" {
		appKey = conf.NwkSKey
	}
	appSKey := defaultAppSKey
	if conf.AppSKey != "" {
		appSKey = conf.AppSKey
	}
	intervalMin := &defaultIntervalMin
	if conf.Interval != nil {
		intervalMin = conf.Interval
	}
	return node.Config{
		JoinType: conf.JoinType,
		Decoder:  decoderURL,
		Interval: intervalMin,
		DevEUI:   conf.DevEUI,
		AppKey:   appKey,
		AppSKey:  appSKey,
		NwkSKey:  nwkSKey,
		DevAddr:  conf.DevAddr,
		Gateways: conf.Gateways,
	}
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	nodeConf := conf.getNodeConfig()
	deps, err := nodeConf.Validate(path)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

// CT101 defines a lorawan node device.
type CT101 struct {
	resource.Named
	logger logging.Logger
	node   node.Node
}

func newCT101(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	n := &CT101{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
		node:   node.NewSensor(conf, logger),
	}

	err := n.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return n, nil
}

// Reconfigure reconfigure's the node.
func (n *CT101) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	nodeCfg := cfg.getNodeConfig()

	err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg)
	if err != nil {
		return err
	}

	err = node.CheckCaptureFrequency(conf, *cfg.Interval, n.logger)
	if err != nil {
		return err
	}

	return nil
}

// Close removes the device from the gateway.
func (n *CT101) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings.
func (n *CT101) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}
