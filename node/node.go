// Package node implements the node model
package node

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// Model represents a lorawan node model.
var Model = resource.NewModel("viam", "lorawan", "node")

// Error variables for validation.
var (
	errDecoderPathRequired = errors.New("decoder path is required")
	errIntervalRequired    = errors.New("uplink_interval_mins is required")
	errIntervalZero        = errors.New("uplink_interval_mins cannot be zero")
	errInvalidJoinType     = errors.New("join type is OTAA or ABP - defaults to OTAA")
	errDevEUIRequired      = errors.New("dev EUI is required for OTAA join type")
	errDevEUILength        = errors.New("dev EUI must be 8 bytes")
	errAppKeyRequired      = errors.New("app key is required for OTAA join type")
	errAppKeyLength        = errors.New("app key must be 16 bytes")
	errAppSKeyRequired     = errors.New("app session key is required for ABP join type")
	errAppSKeyLength       = errors.New("app session key must be 16 bytes")
	errNwkSKeyRequired     = errors.New("network session key is required for ABP join type")
	errNwkSKeyLength       = errors.New("network session key must be 16 bytes")
	errDevAddrRequired     = errors.New("device address is required for ABP join type")
	errDevAddrLength       = errors.New("device address must be 4 bytes")
)

const (
	// Join types.
	joinTypeOTAA = "OTAA"
	joinTypeABP  = "ABP"
)

var noReadings = map[string]interface{}{"": "no readings available yet"}

// Config defines the node's config.
type Config struct {
	JoinType    string   `json:"join_type,omitempty"`
	DecoderPath string   `json:"decoder_path"`
	Interval    *float64 `json:"uplink_interval_mins"`
	DevEUI      string   `json:"dev_eui,omitempty"`
	AppKey      string   `json:"app_key,omitempty"`
	AppSKey     string   `json:"app_s_key,omitempty"`
	NwkSKey     string   `json:"network_s_key,omitempty"`
	DevAddr     string   `json:"dev_addr,omitempty"`
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newNode,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	if conf.DecoderPath == "" {
		return nil, resource.NewConfigValidationError(path, errDecoderPathRequired)
	}

	if conf.Interval == nil {
		return nil, resource.NewConfigValidationError(path, errIntervalRequired)
	}

	if *conf.Interval == 0 {
		return nil, resource.NewConfigValidationError(path, errIntervalZero)
	}

	switch conf.JoinType {
	case joinTypeABP:
		return conf.validateABPAttributes(path)
	case joinTypeOTAA, "":
		return conf.validateOTAAAttributes(path)
	default:
		return nil, resource.NewConfigValidationError(path, errInvalidJoinType)
	}
}

func (conf *Config) validateOTAAAttributes(path string) ([]string, error) {
	if conf.DevEUI == "" {
		return nil, resource.NewConfigValidationError(path, errDevEUIRequired)
	}
	if len(conf.DevEUI) != 16 {
		return nil, resource.NewConfigValidationError(path, errDevEUILength)
	}
	if conf.AppKey == "" {
		return nil, resource.NewConfigValidationError(path, errAppKeyRequired)
	}
	if len(conf.AppKey) != 32 {
		return nil, resource.NewConfigValidationError(path, errAppKeyLength)
	}
	return nil, nil
}

func (conf *Config) validateABPAttributes(path string) ([]string, error) {
	if conf.AppSKey == "" {
		return nil, resource.NewConfigValidationError(path, errAppSKeyRequired)
	}
	if len(conf.AppSKey) != 32 {
		return nil, resource.NewConfigValidationError(path, errAppSKeyLength)
	}
	if conf.NwkSKey == "" {
		return nil, resource.NewConfigValidationError(path, errNwkSKeyRequired)
	}
	if len(conf.NwkSKey) != 32 {
		return nil, resource.NewConfigValidationError(path, errNwkSKeyLength)
	}
	if conf.DevAddr == "" {
		return nil, resource.NewConfigValidationError(path, errDevAddrRequired)
	}
	if len(conf.DevAddr) != 8 {
		return nil, resource.NewConfigValidationError(path, errDevAddrLength)
	}

	return nil, nil
}

// Node defines a lorawan node device.
type Node struct {
	resource.Named
	logger logging.Logger

	AppSKey []byte
	NwkSKey []byte
	AppKey  []byte

	Addr   []byte
	DevEui []byte

	DecoderPath string
	NodeName    string
	gateway     sensor.Sensor
	JoinType    string
}

func newNode(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	n := &Node{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
		// This returns the name given to the resource by user
		// may be different if using remotes
		NodeName: conf.ResourceName().AsNamed().Name().Name,
	}

	err := n.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return n, nil
}

// Reconfigure reconfigure's the node.
func (n *Node) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	switch cfg.JoinType {
	case "OTAA", "":
		appKey, err := hex.DecodeString(cfg.AppKey)
		if err != nil {
			return err
		}
		n.AppKey = appKey

		devEui, err := hex.DecodeString(cfg.DevEUI)
		if err != nil {
			return err
		}
		n.DevEui = devEui
	case "ABP":
		devAddr, err := hex.DecodeString(cfg.DevAddr)
		if err != nil {
			return err
		}

		n.Addr = devAddr

		appSKey, err := hex.DecodeString(cfg.AppSKey)
		if err != nil {
			return err
		}

		n.AppSKey = appSKey
	}

	n.DecoderPath = cfg.DecoderPath
	n.JoinType = cfg.JoinType

	if n.JoinType == "" {
		n.JoinType = "OTAA"
	}

	gateway, err := getGateway(ctx, deps)
	if err != nil {
		return err
	}

	cmd := make(map[string]interface{})

	// send the device to the gateway.
	cmd["register_device"] = n

	_, err = gateway.DoCommand(ctx, cmd)
	if err != nil {
		return err
	}

	n.gateway = gateway

	// Warn if user's configured capture frequency is more than the expected uplink interval.
	captureFreq, err := getCaptureFrequencyHzFromConfig(conf)
	if err != nil {
		return err
	}

	intervalSeconds := (time.Duration(*cfg.Interval) * time.Minute).Seconds()
	expectedFreq := 1 / intervalSeconds

	if captureFreq > expectedFreq {
		n.logger.Warnf(
			`"configured capture frequency (%v) is greater than the frequency (%v)
			of expected uplink interval for node %v: lower capture frequency to avoid duplicate data"`,
			captureFreq,
			expectedFreq,
			n.NodeName)
	}

	return nil
}

// getGateway sends the validate docommand to the gateway to confirm the dependency.
func getGateway(ctx context.Context, deps resource.Dependencies) (sensor.Sensor, error) {
	if len(deps) == 0 {
		return nil, errors.New("must add sx1302-gateway as dependency")
	}
	var dep resource.Resource

	// Assuming there's only one dep.
	for _, val := range deps {
		dep = val
	}

	gateway, ok := dep.(sensor.Sensor)
	if !ok {
		return nil, errors.New("dependency must be the sx1302-gateway sensor")
	}

	cmd := make(map[string]interface{})
	cmd["validate"] = 1

	// Validate that the dependency is the gateway - gateway will return 1.
	ret, err := gateway.DoCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}

	retVal, ok := ret["validate"]
	if !ok {
		return nil, errors.New("dependency must be the sx1302-gateway sensor")
	}
	if retVal.(float64) != 1 {
		return nil, errors.New("dependency must be the sx1302-gateway sensor")
	}
	return gateway, nil
}

// Close removes the device from the gateway.
func (n *Node) Close(ctx context.Context) error {
	cmd := make(map[string]interface{})
	cmd["remove_device"] = n.NodeName
	_, err := n.gateway.DoCommand(ctx, cmd)
	return err
}

// Readings returns the node's readings.
func (n *Node) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	if n.gateway != nil {
		allReadings, err := n.gateway.Readings(ctx, nil)
		if err != nil {
			return map[string]interface{}{}, err
		}

		reading, ok := allReadings[n.NodeName]
		// no readings available yet
		if !ok {
			// If the readings call came from data capture, return noCaptureToStore error to indicate not to capture data.
			if extra[data.FromDMString] == true {
				return map[string]interface{}{}, data.ErrNoCaptureToStore
			}
			return noReadings, nil
		}
		return reading.(map[string]interface{}), nil
	}
	return map[string]interface{}{}, errors.New("node does not have gateway")
}

// getCaptureFrequencyHzFromConfig extract the capture_frequency_hz from the device config.
func getCaptureFrequencyHzFromConfig(c resource.Config) (float64, error) {
	var captureFreqHz float64
	var captureMethodFound bool
	for _, assocResourceCfg := range c.AssociatedResourceConfigs {
		if captureMethodsMapInterface := assocResourceCfg.Attributes["capture_methods"]; captureMethodsMapInterface != nil {
			captureMethodFound = true
			for _, captureMethodsInterface := range captureMethodsMapInterface.([]interface{}) {
				captureMethods := captureMethodsInterface.(map[string]interface{})
				if captureMethods["method"].(string) == "Readings" {
					captureFreqHz = captureMethods["capture_frequency_hz"].(float64)
				}
			}
		}
	}
	if captureMethodFound && captureFreqHz <= 0 {
		return 0.0, errors.New("zero or negative capture frequency")
	}
	return captureFreqHz, nil
}
