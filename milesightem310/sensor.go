// Package milesightem310 implements em310 node
package milesightem310

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"

	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	decoderURL = "https://raw.githubusercontent.com/Milesight-IoT/SensorDecoders/fddc96c379ae0999d0fa2baef616b7856010cf14/" +
		"EM_Series/EM300_Series/EM310-TILT/EM310-TILT_Decoder.js"
	defaultAppKey  = "5572404C696E6B4C6F52613230313823"
	defaultNwkSKey = "5572404C696E6B4C6F52613230313823"
	defaultAppSKey = "5572404C696E6B4C6F52613230313823"

	intervalKey = "set_interval"
	resetKey    = "restart_sensor"
)

var defaultIntervalMin = 1080.

// Model represents a dragino-LHT65N lorawan node model.
var Model = node.LorawanFamily.WithModel("milesight-em310-tilt")

// Config defines the em310-tilt's config.
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
			Constructor: newEM310Tilt,
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

// em310Tilt defines a lorawan node device.
type em310Tilt struct {
	resource.Named
	logger logging.Logger
	node   node.Node
}

func newEM310Tilt(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	n := &em310Tilt{
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
func (n *em310Tilt) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	nodeCfg := cfg.getNodeConfig()

	err = n.node.ReconfigureWithConfig(ctx, deps, &nodeCfg)
	if err != nil {
		return err
	}

	// set the interval if one was provided
	// we do not send a default in case the user has already set an interval they prefer
	if cfg.Interval != nil && *cfg.Interval != 0 {
		_, err = n.addIntervalToQueue(ctx, *nodeCfg.Interval, false)
		if err != nil {
			return err
		}
	}

	err = node.CheckCaptureFrequency(conf, *nodeCfg.Interval, n.logger)
	if err != nil {
		return err
	}

	return nil
}

// Close removes the device from the gateway.
func (n *em310Tilt) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings.
func (n *em310Tilt) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}

// DoCommand implements the DoCommand for the em310Tilt.
func (n *em310Tilt) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	testOnly := node.CheckTestKey(cmd)
	if interval, intervalSet := cmd[intervalKey]; intervalSet {
		if intervalFloat, floatOk := interval.(float64); floatOk {
			return n.addIntervalToQueue(ctx, intervalFloat, testOnly)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected float got %v", reflect.TypeOf(interval))
	}
	if _, ok := cmd[resetKey]; ok {
		return n.addRestartToQueue(ctx, testOnly)
	}

	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}

func (n *em310Tilt) addIntervalToQueue(ctx context.Context, interval float64, testOnly bool) (map[string]interface{}, error) {
	// convert to the nearest second.
	convertToSeconds := uint16(math.Round(interval * 60))

	// create a byte array of size 2 and put the payload in as little endian.
	bs := make([]byte, 2)
	binary.LittleEndian.PutUint16(bs, convertToSeconds)

	// ff byte is the channel, 03 is the message type byte for the downlink.
	intervalString := "ff03"
	for _, b := range bs {
		intervalString = fmt.Sprintf("%s%02x", intervalString, b)
	}

	if testOnly {
		return map[string]interface{}{intervalKey: intervalString}, nil
	}

	return n.node.SendDownlink(ctx, intervalString, false)
}

func (n *em310Tilt) addRestartToQueue(ctx context.Context, testOnly bool) (map[string]interface{}, error) {
	// ff byte is the channel, 10 is the message type and ff is the command for the downlink.
	intervalString := "ff10ff"
	if testOnly {
		return map[string]interface{}{resetKey: intervalString}, nil
	}

	return n.node.SendDownlink(ctx, intervalString, false)
}
