// Package draginowqslb implements the dragino WQS-LB sensor model
package draginowqslb

import (
	"context"
	"fmt"
	"reflect"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	Model      = "dragino-WQS-LB"
	decoderURL = "https://raw.githubusercontent.com/dragino/dragino-end-node-decoder/" +
		"5a2855dbddba7977e06ba710f33fbee27de124e5/WQS-LB/WQS-LB_ChirpstackV4_Decoder.txt"
)

var (
	// ModelWQSLB represents the WQS-LB sensor model
	ModelWQSLB         = node.LorawanFamily.WithModel(Model)
	defaultIntervalMin = 20. // minutes
)

// Config defines the WQS-LB sensor config
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
		ModelWQSLB,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: func(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
				return newWQSLB(ctx, deps, conf, logger)
			},
		})
}

func (conf *Config) getNodeConfig() node.Config {
	intervalMin := &defaultIntervalMin
	if conf.Interval != nil {
		intervalMin = conf.Interval
	}

	return node.Config{
		JoinType: conf.JoinType,
		Decoder:  decoderURL,
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

// Validate ensures all parts of the config are valid
func (conf *Config) Validate(path string) ([]string, error) {
	nodeConf := conf.getNodeConfig()
	deps, err := nodeConf.Validate(path)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

// WQSLB defines the WQS-LB sensor implementation
type WQSLB struct {
	resource.Named
	logger logging.Logger
	node   node.Node
}

func newWQSLB(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	n := &WQSLB{
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

// Reconfigure reconfigures the sensor
func (n *WQSLB) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	nodeCfg := cfg.getNodeConfig()

	if err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg); err != nil {
		return err
	}

	// call this once outside of background thread to get any info gateway has before calling the interval request
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

// Close removes the device from the gateway
func (n *WQSLB) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings
func (n *WQSLB) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}

// DoCommand implements the DoCommand interface
func (n *WQSLB) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	testOnly := node.CheckTestKey(cmd)

	if interval, intervalSet := cmd[node.IntervalKey]; intervalSet {
		if intervalFloat, floatOk := interval.(float64); floatOk {
			req := node.IntervalRequest{IntervalMin: intervalFloat, PayloadUnits: node.Seconds, Header: "01", NumBytes: 3, TestOnly: testOnly}
			return n.node.SendIntervalDownlink(ctx, req)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected float got %v", reflect.TypeOf(interval))
	}
	if _, ok := cmd[node.ResetKey]; ok {
		// 04 is the header and FFFF is the command for the downlink
		req := node.ResetRequest{Header: "04", PayloadHex: "FFFF", TestOnly: testOnly}
		return n.node.SendResetDownlink(ctx, req)
	}

	// WQSLB specific calibration commands
	if _, ok := cmd["calibrate_ph_9"]; ok {
		return n.node.SendDownlink(ctx, "FB09", testOnly)
	}
	if _, ok := cmd["calibrate_ph_6"]; ok {
		return n.node.SendDownlink(ctx, "FB06", testOnly)
	}
	if _, ok := cmd["calibrate_ph_4"]; ok {
		return n.node.SendDownlink(ctx, "FB04", testOnly)
	}
	if _, ok := cmd["calibrate_ec_1"]; ok {
		return n.node.SendDownlink(ctx, "FD01", testOnly)
	}
	if _, ok := cmd["calibrate_ec_10"]; ok {
		return n.node.SendDownlink(ctx, "FD10", testOnly)
	}
	if _, ok := cmd["calibrate_t_0"]; ok {
		return n.node.SendDownlink(ctx, "FE00", testOnly)
	}
	if _, ok := cmd["calibrate_t_2"]; ok {
		return n.node.SendDownlink(ctx, "FE02", testOnly)
	}
	if _, ok := cmd["calibrate_t_4"]; ok {
		return n.node.SendDownlink(ctx, "FE04", testOnly)
	}
	if _, ok := cmd["calibrate_t_6"]; ok {
		return n.node.SendDownlink(ctx, "FE06", testOnly)
	}
	if _, ok := cmd["calibrate_t_8"]; ok {
		return n.node.SendDownlink(ctx, "FE08", testOnly)
	}
	if _, ok := cmd["calibrate_t_10"]; ok {
		return n.node.SendDownlink(ctx, "FE0S", testOnly)
	}
	if _, ok := cmd["calibrate_orp_86"]; ok {
		return n.node.SendDownlink(ctx, "FC0056", testOnly)
	}
	if _, ok := cmd["calibrate_orp_10"]; ok {
		return n.node.SendDownlink(ctx, "FC0100", testOnly)
	}

	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}
