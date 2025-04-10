// Package dragino implements unified dragino sensor models
package dragino

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

// Model types for different dragino sensors
const (
	LHT65N                = "dragino-LHT65N"
	WQSLB                 = "dragino-WQS-LB"
	lht65nDecoderFileName = "LHT65NChirpstack4decoder.js"
	wqsDecoderURL         = "https://raw.githubusercontent.com/dragino/dragino-end-node-decoder/" +
		"5a2855dbddba7977e06ba710f33fbee27de124e5/WQS-LB/WQS-LB_ChirpstackV4_Decoder.txt"
)

var (
	ModelLHT65N = node.LorawanFamily.WithModel(LHT65N)
	ModelWQSLB  = node.LorawanFamily.WithModel(WQSLB)

	// Models represents the available dragino sensor models
	Models = map[string]resource.Model{
		LHT65N: ModelLHT65N,
		WQSLB:  ModelWQSLB,
	}

	//go:embed LHT65NChirpstack4decoder.js
	lht65nDecoderFile embed.FS

	defaultIntervalMin = 20. // minutes
)

// DecoderSource defines how to get the decoder
type DecoderSource struct {
	URL      string   // URL to fetch decoder from
	Embedded embed.FS // Embedded decoder file
	Filename string   // Name of decoder file
}

// Config defines the dragino sensor config
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
	// Register both sensor types
	for modelName, model := range Models {
		resource.RegisterComponent(
			sensor.API,
			model,
			resource.Registration[sensor.Sensor, *Config]{
				Constructor: func(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
					return newDraginoSensor(ctx, deps, conf, logger, modelName)
				},
			})
	}
}

func (conf *Config) getNodeConfig(decoderPath string) node.Config {
	intervalMin := &defaultIntervalMin
	if conf.Interval != nil {
		intervalMin = conf.Interval
	}

	return node.Config{
		JoinType: conf.JoinType,
		Decoder:  decoderPath,
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
	nodeConf := conf.getNodeConfig("fixed")
	deps, err := nodeConf.Validate(path)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

// DraginoSensor defines a unified dragino sensor implementation
type DraginoSensor struct {
	resource.Named
	logger      logging.Logger
	decoderPath string
	node        node.Node
	model       string
}

// getDecoderSource returns the appropriate decoder source based on model type
func getDecoderSource(modelName string) DecoderSource {
	switch modelName {
	case WQSLB:
		return DecoderSource{
			URL: wqsDecoderURL,
		}
	default: // ModelLHT65N
		return DecoderSource{
			Filename: lht65nDecoderFileName,
			Embedded: lht65nDecoderFile,
		}
	}
}

func newDraginoSensor(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
	model string,
) (sensor.Sensor, error) {
	decoderSource := getDecoderSource(model)

	var decoderPath string
	if decoderSource.URL != "" {
		decoderPath = decoderSource.URL
	} else if decoderSource.Filename != "" {
		var err error
		decoderPath, err = node.WriteDecoderFile(decoderSource.Filename, decoderSource.Embedded)
		if err != nil {
			return nil, err
		}
	}

	n := &DraginoSensor{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
		node:        node.NewSensor(conf, logger),
		decoderPath: decoderPath,
		model:       model,
	}

	err := n.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return n, nil
}

// Reconfigure reconfigures the sensor
func (n *DraginoSensor) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	nodeCfg := cfg.getNodeConfig(n.decoderPath)

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
func (n *DraginoSensor) Close(ctx context.Context) error {
	return n.node.Close(ctx)
}

// Readings returns the node's readings
func (n *DraginoSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return n.node.Readings(ctx, extra)
}

// DoCommand implements the DoCommand interface
func (n *DraginoSensor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
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

	// do generic node if no sensor specific key was found
	return n.node.DoCommand(ctx, cmd)
}
