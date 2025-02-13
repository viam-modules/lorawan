// Package draginolht65n implements the dragino_lth65n model
package draginolht65n

import (
	"context"
	"embed"
	"gateway/node"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

//go:embed LHT65NChirpstack4decoder.js
var decoderFile embed.FS

const decoderFilename = "LHT65NChirpstack4decoder.js"

// Model represents a dragino-LHT65N lorawan node model.
var Model = resource.NewModel("viam", "lorawan", "dragino-LHT65N")

// Config defines the dragino-LHT65N's config.
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
			Constructor: newLHT65N,
		})
}

func (conf *Config) getNodeConfig(decoderFilePath string) node.Config {
	return node.Config{
		JoinType:    conf.JoinType,
		DecoderPath: decoderFilePath,
		Interval:    conf.Interval,
		DevEUI:      conf.DevEUI,
		AppKey:      conf.AppKey,
		AppSKey:     conf.AppSKey,
		NwkSKey:     conf.NwkSKey,
		DevAddr:     conf.DevAddr,
		Gateways:    conf.Gateways,
	}
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	nodeConf := conf.getNodeConfig("fixed")
	deps, err := nodeConf.Validate(path)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

// LHT65N defines a lorawan node device.
type LHT65N struct {
	resource.Named
	logger logging.Logger

	node        node.Node
	decoderPath string
}

func newLHT65N(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	decoderFilePath, err := node.WriteDecoderFile(decoderFilename, decoderFile)
	if err != nil {
		return nil, err
	}
	n := &LHT65N{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
		// This returns the name given to the resource by user
		// may be different if using remotes
		node:        node.NewSensor(conf, logger),
		decoderPath: decoderFilePath,
	}

	err = n.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return n, nil
}

// Reconfigure reconfigure's the node.
func (n *LHT65N) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	nodeCfg := cfg.getNodeConfig(n.decoderPath)
	if len(deps) == 0 {
		n.logger.Info("yo no deps")
	}

	err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg)
	if err != nil {
		return err
	}

	_, err = node.CheckCaptureFrequency(conf, *cfg.Interval, n.logger)
	if err != nil {
		return err
	}

	return nil
}

// Close removes the device from the gateway.
func (n *LHT65N) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings.
func (n *LHT65N) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}
