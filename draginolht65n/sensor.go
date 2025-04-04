// Package draginolht65n implements the dragino_lth65n model
package draginolht65n

import (
	"context"
	"embed"
	"fmt"
	"reflect"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	decoderFilename = "LHT65NChirpstack4decoder.js"
)

// Model represents a dragino-LHT65N lorawan node model.
var Model = node.LorawanFamily.WithModel("dragino-LHT65N")

//go:embed LHT65NChirpstack4decoder.js
var decoderFile embed.FS

var defaultIntervalMin = 20. // minutes

// Config defines the dragino-LHT65N's config.
type Config struct {
	JoinType string   `json:"join_type,omitempty"`
	Interval *float64 `json:"uplink_interval_mins,omitempty"`
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
	intervalMin := &defaultIntervalMin
	if conf.Interval != nil {
		intervalMin = conf.Interval
	}

	return node.Config{
		JoinType: conf.JoinType,
		Decoder:  decoderFilePath,
		Interval: intervalMin,
		DevEUI:   conf.DevEUI,
		AppKey:   conf.AppKey,
		AppSKey:  conf.AppSKey,
		NwkSKey:  conf.NwkSKey,
		DevAddr:  conf.DevAddr,
		Gateways: conf.Gateways,
		FPort:    "01",
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
	logger      logging.Logger
	decoderPath string
	node        node.Node
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

	if err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg); err != nil {
		return err
	}

	// call this once outside of background thread to get any info gateway has before calling the interval request.
	n.node.GetAndUpdateDeviceInfo(ctx)

	// set the interval if one was provided
	// we do not send a default in case the user has already set an interval they prefer
	if cfg.Interval != nil && *cfg.Interval != 0 {
		req := node.IntervalRequest{IntervalMin: *nodeCfg.Interval, PayloadUnits: node.Seconds, Header: "01", NumBytes: 3, TestOnly: false}
		if _, err := n.node.SendIntervalDownlink(ctx, req); err != nil {
			return err
		}
	}

	if err = node.CheckCaptureFrequency(conf, *nodeCfg.Interval, n.logger); err != nil {
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

// DoCommand implements the DoCommand for the LHT65N.
func (n *LHT65N) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	testOnly := node.CheckTestKey(cmd)

	if interval, intervalSet := cmd[node.IntervalKey]; intervalSet {
		if intervalFloat, floatOk := interval.(float64); floatOk {
			req := node.IntervalRequest{IntervalMin: intervalFloat, PayloadUnits: node.Seconds, Header: "01", NumBytes: 3, TestOnly: testOnly}
			return n.node.SendIntervalDownlink(ctx, req)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected float got %v", reflect.TypeOf(interval))
	}
	if _, ok := cmd[node.ResetKey]; ok {
		// 04 is the header and FFFF is the command for the downlink.
		req := node.ResetRequest{Header: "04", PayloadHex: "FFFF", TestOnly: testOnly}
		return n.node.SendResetDownlink(ctx, req)
	}

	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}
