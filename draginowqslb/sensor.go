// Package draginowqslb implements the dragino WQS-LB sensor model
package draginowqslb

import (
	"context"
	"fmt"
	"reflect"

	"github.com/viam-modules/gateway/dragino"
	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	decoderURL = "https://raw.githubusercontent.com/dragino/dragino-end-node-decoder/" +
		"5a2855dbddba7977e06ba710f33fbee27de124e5/WQS-LB/WQS-LB_ChirpstackV4_Decoder.txt"

	// Ranges for each probe were found in the user manual:
	// https://wiki.dragino.com/xwiki/bin/view/Main/User%20Manual%20for%20LoRaWAN%20End%20Nodes
	// /WQS-LB--LoRaWAN_Water_Quality_Sensor_Transmitter_User_Manual/
	ecK10Key     = "EC_K10"
	ecK10Min     = 20.
	ecK10Max     = 20000.
	ecK1Key      = "EC_K1"
	ecK1Min      = 0.
	ecK1Max      = 10000.
	phMin        = 0.
	phMax        = 14.
	phKey        = "PH"
	orpKey       = "ORP"
	orpMin       = -1999.
	orpMax       = 1999.
	doKey        = "dissolved_oxygen"
	doMin        = 0.
	doMax        = 20.
	turbidityKey = "turbidity"
	turbidityMin = 0.1
	turbidityMax = 10000.
)

var probeRanges = []probeRange{
	{ecK10Key, ecK10Min, ecK10Max},
	{ecK1Key, ecK1Min, ecK1Max},
	{phKey, phMin, phMax},
	{orpKey, orpMin, orpMax},
	{doKey, doMin, doMax},
	{turbidityKey, turbidityMin, turbidityMax},
}

var (
	// Model represents the WQS-LB sensor model.
	Model              = node.LorawanFamily.WithModel("dragino-WQS-LB")
	defaultIntervalMin = 20. // minutes
)

// Config defines the WQS-LB sensor config.
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
			Constructor: newWQSLB,
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

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	nodeConf := conf.getNodeConfig()
	deps, err := nodeConf.Validate(path)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

// WQSLB defines the WQS-LB sensor implementation.
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

// Reconfigure reconfigures the sensor.
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

	// Set the interval if one was provided
	// We do not send a default in case the user has already set an interval they prefer
	if cfg.Interval != nil && *cfg.Interval != 0 {
		req := dragino.CreateIntervalDownlinkRequest(ctx, *cfg.Interval, false)
		_, err := n.node.SendIntervalDownlink(ctx, req)
		if err != nil {
			return err
		}
	}

	if err = node.CheckCaptureFrequency(conf, *nodeCfg.Interval, n.logger); err != nil {
		return err
	}

	return nil
}

// Close removes the device from the gateway.
func (n *WQSLB) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// probeRange defines the valid ranges for a probe.
type probeRange struct {
	key      string
	min, max float64
}

// Readings returns the node's readings.
func (n *WQSLB) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	reading, err := n.node.Readings(ctx, extra)
	if err != nil {
		return map[string]interface{}{}, err
	}

	for _, probeRange := range probeRanges {
		reading = sanitizeReading(reading, extra, probeRange)
	}

	return reading, nil
}

func sanitizeReading(reading, extra map[string]interface{}, limits probeRange) map[string]interface{} {
	if val, ok := reading[limits.key].(float64); ok && (val > limits.max || val < limits.min) {
		// remove reading if from data capture
		if extra != nil && extra[data.FromDMString] == true {
			delete(reading, limits.key)
		} else {
			reading[limits.key] = "INVALID"
		}
	}
	return reading
}

// DoCommand implements the DoCommand interface.
func (n *WQSLB) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	testOnly := node.CheckTestKey(cmd)

	if interval, intervalSet := cmd[node.IntervalKey]; intervalSet {
		if intervalFloat, floatOk := interval.(float64); floatOk {
			req := dragino.CreateIntervalDownlinkRequest(ctx, intervalFloat, testOnly)
			return n.node.SendIntervalDownlink(ctx, req)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected float got %v", reflect.TypeOf(interval))
	}
	if _, ok := cmd[node.ResetKey]; ok {
		dragino.DraginoResetRequest.TestOnly = testOnly
		return n.node.SendResetDownlink(ctx, dragino.DraginoResetRequest)
	}
	// WQSLB specific calibration commands
	if ph, ok := cmd["calibrate_ph"]; ok {
		phFloat, ok := ph.(float64)
		if !ok {
			return map[string]interface{}{}, fmt.Errorf("expected float64 got %v", reflect.TypeOf(ph))
		}
		payload := "FB"

		switch phFloat {
		// probe in 9.18 buffer soluton
		case 9:
			payload += "09"
		// probe in 6.86 buffer soluton
		case 6:
			payload += "06"
		// probe in 4.01 buffer soluton
		case 4:
			payload += "04"
		default:
			return map[string]interface{}{}, fmt.Errorf("unexpected ph calibration value %f, valid values are 4, 6, or 9", phFloat)
		}
		return n.node.SendDownlink(ctx, payload, testOnly)
	}

	if ec, ok := cmd["calibrate_ec"]; ok {
		ecFloat, ok := ec.(float64)
		if !ok {
			return map[string]interface{}{}, fmt.Errorf("expected float64 got %v", reflect.TypeOf(ec))
		}
		payload := "FD"
		switch ecFloat {
		// for DR-ECK1.0 probe
		case 1:
			payload += "01"
		// for DR-ECK10.0 probe
		case 10:
			payload += "10"
		default:
			return map[string]interface{}{}, fmt.Errorf("unexpected electrical conductivity calibration value %f, valid values are 1 or 10", ecFloat)
		}
		return n.node.SendDownlink(ctx, payload, testOnly)
	}

	// turbidity calibration
	if t, ok := cmd["calibrate_t"]; ok {
		tFloat, ok := t.(float64)
		if !ok {
			return map[string]interface{}{}, fmt.Errorf("expected float64 got %v", reflect.TypeOf(t))
		}
		payload := "FE"
		// each case here corresponds to an NTU value,
		// corresponding to each step in the calibration instructions of the README.
		switch tFloat {
		case 0:
			payload += "00"
		case 200:
			payload += "02"
		case 400:
			payload += "04"
		case 600:
			payload += "06"
		case 800:
			payload += "08"
		case 1000:
			payload += "0A"
		default:
			return map[string]interface{}{}, fmt.Errorf("unexpected turbidity calibration value %f,"+
				"expected values are 0,200,400,600,800,or 1000", tFloat)
		}
		return n.node.SendDownlink(ctx, payload, testOnly)
	}

	// orp calibration
	if orp, ok := cmd["calibrate_orp"]; ok {
		orpFloat, ok := orp.(float64)
		if !ok {
			return map[string]interface{}{}, fmt.Errorf("expected float64 got %v", reflect.TypeOf(orp))
		}
		payload := "FC"
		switch orpFloat {
		// probe in 86mV standard buffer
		case 86:
			payload += "0056"
		// probe in 256mV standard buffer
		case 256:
			payload += "0100"
		default:
			return map[string]interface{}{}, fmt.Errorf("unexpected orp calibration value %f, expected values are 86 or 256", orpFloat)
		}
		return n.node.SendDownlink(ctx, payload, testOnly)
	}
	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}
