// Package milesightct101 implements the Milesight-CT101 model
package milesightct101

import (
	"context"
	"fmt"
	"reflect"

	"github.com/viam-modules/lorawan/node"
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

	resetKey = "restart_sensor"
)

// defaultIntervalMin is how often the CT101 will send an uplink.
var defaultIntervalMin = 10. // minutes

// Model represents a Milesight-CT101 lorawan node model.
var Model = node.LorawanFamily.WithModel("milesight-ct101")

// Config defines the Milesight-CT101's config.
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
		FPort:    "55", // in hex, 85 in decimal
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

	if err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg); err != nil {
		return err
	}

	// call this once outside of background thread to get any info gateway has before calling the interval request.
	n.node.GetAndUpdateDeviceInfo(ctx)

	// set the interval if one was provided
	// we do not send a default in case the user has already set an interval they prefer
	if cfg.Interval != nil && *cfg.Interval != 0 {
		req := node.IntervalRequest{
			IntervalMin: *nodeCfg.Interval, PayloadUnits: node.Minutes, Header: "ff8e00",
			UseLittleEndian: true, NumBytes: 2, TestOnly: false,
		}
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
func (n *CT101) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings.
func (n *CT101) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}

// DoCommand implements the DoCommand for the CT101.
func (n *CT101) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	testOnly := node.CheckTestKey(cmd)
	if interval, intervalSet := cmd[node.IntervalKey]; intervalSet {
		if intervalFloat, floatOk := interval.(float64); floatOk {
			req := node.IntervalRequest{
				IntervalMin: intervalFloat, PayloadUnits: node.Minutes, Header: "FF8E00",
				UseLittleEndian: true, NumBytes: 2, TestOnly: testOnly,
			}
			return n.node.SendIntervalDownlink(ctx, req)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected float got %v", reflect.TypeOf(interval))
	}
	if _, ok := cmd[node.ResetKey]; ok {
		// FF byte is the channel, 10 is the message type and FF is the command for the downlink.
		req := node.ResetRequest{Header: "FF10", PayloadHex: "FF", TestOnly: testOnly}
		return n.node.SendResetDownlink(ctx, req)
	}

	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}
