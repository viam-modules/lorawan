// Package draginolht65n implements the dragino_lth65n model
package draginolht65n

import (
	"context"
	"net/http"
	"time"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	decoderFilename = "LHT65NChirpstack4decoder.js"
	decoderURL      = "https://raw.githubusercontent.com/dragino/dragino-end-node-decoder/refs/heads/main/LHT65N/" +
		"LHT65N Chirpstack  4.0 decoder.txt"
)

// Model represents a dragino-LHT65N lorawan node model.
var Model = node.LorawanFamily.WithModel("dragino-LHT65N")

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
	httpClient := &http.Client{
		Timeout: time.Second * 25,
	}
	decoderFilePath, err := node.WriteDecoderFileFromURL(ctx, decoderFilename, decoderURL, httpClient, logger)
	if err != nil {
		return nil, err
	}

	n := &LHT65N{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
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
func (n *LHT65N) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings.
func (n *LHT65N) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}
