// Package node implements the node model
package node

import (
	"context"
	"errors"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// LorawanFamily is the model family for the Lorawan module.
var LorawanFamily = resource.NewModelFamily("viam", "lorawan")

// Model represents a lorawan node model.
var Model = LorawanFamily.WithModel("node")

// Error variables for validation.
var (
	ErrDecoderPathRequired = errors.New("decoder path is required")
	ErrGatewayEmpty        = errors.New("gateway name cannot be empty")
	ErrIntervalRequired    = errors.New("uplink_interval_mins is required")
	ErrIntervalZero        = errors.New("uplink_interval_mins cannot be zero")
	ErrInvalidJoinType     = errors.New("join type is OTAA or ABP - defaults to OTAA")
	ErrDevEUIRequired      = errors.New("dev EUI is required for OTAA join type")
	ErrDevEUILength        = errors.New("dev EUI must be 8 bytes")
	ErrAppKeyRequired      = errors.New("app key is required for OTAA join type")
	ErrAppKeyLength        = errors.New("app key must be 16 bytes")
	ErrAppSKeyRequired     = errors.New("app session key is required for ABP join type")
	ErrAppSKeyLength       = errors.New("app session key must be 16 bytes")
	ErrNwkSKeyRequired     = errors.New("network session key is required for ABP join type")
	ErrNwkSKeyLength       = errors.New("network session key must be 16 bytes")
	ErrDevAddrRequired     = errors.New("device address is required for ABP join type")
	ErrDevAddrLength       = errors.New("device address must be 4 bytes")
	ErrBadDecoderURL       = "Error Retreiving decoder url is invalid, please report to maintainer: " +
		"Status Code %v"
	ErrFPortTooLong = errors.New("fport must be one byte long")
)

const (
	// JoinTypeOTAA is the OTAA Join type.
	JoinTypeOTAA = "OTAA"
	// JoinTypeABP is the ABP Join type.
	JoinTypeABP = "ABP"
)

var noReadings = map[string]interface{}{"": "no readings available yet"}

// Config defines the node's config.
type Config struct {
	JoinType string   `json:"join_type,omitempty"`
	Decoder  string   `json:"decoder_path"`
	Interval *float64 `json:"uplink_interval_mins"`
	DevEUI   string   `json:"dev_eui,omitempty"`
	AppKey   string   `json:"app_key,omitempty"`
	AppSKey  string   `json:"app_s_key,omitempty"`
	NwkSKey  string   `json:"network_s_key,omitempty"`
	DevAddr  string   `json:"dev_addr,omitempty"`
	Gateways []string `json:"gateways"`
	FPort    string   `json:"fport,omitempty"`
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
	if conf.Decoder == "" {
		return nil, resource.NewConfigValidationError(path, ErrDecoderPathRequired)
	}

	if conf.Interval == nil {
		return nil, resource.NewConfigValidationError(path, ErrIntervalRequired)
	}

	if *conf.Interval == 0 {
		return nil, resource.NewConfigValidationError(path, ErrIntervalZero)
	}

	deps := []string{}
	for _, gateway := range conf.Gateways {
		if gateway == "" {
			return nil, resource.NewConfigValidationError(path, ErrGatewayEmpty)
		}
		deps = append(deps, gateway)
	}

	if len(conf.FPort) > 2 {
		return nil, resource.NewConfigValidationError(path, ErrFPortTooLong)
	}

	var err error
	switch conf.JoinType {
	case JoinTypeABP:
		err = conf.validateABPAttributes(path)
	case JoinTypeOTAA, "":
		err = conf.validateOTAAAttributes(path)
	default:
		err = resource.NewConfigValidationError(path, ErrInvalidJoinType)
	}
	if err != nil {
		return nil, err
	}
	return deps, nil
}

func (conf *Config) validateOTAAAttributes(path string) error {
	if conf.DevEUI == "" {
		return resource.NewConfigValidationError(path, ErrDevEUIRequired)
	}
	if len(conf.DevEUI) != 16 {
		return resource.NewConfigValidationError(path, ErrDevEUILength)
	}
	if conf.AppKey == "" {
		return resource.NewConfigValidationError(path, ErrAppKeyRequired)
	}
	if len(conf.AppKey) != 32 {
		return resource.NewConfigValidationError(path, ErrAppKeyLength)
	}
	return nil
}

func (conf *Config) validateABPAttributes(path string) error {
	if conf.AppSKey == "" {
		return resource.NewConfigValidationError(path, ErrAppSKeyRequired)
	}
	if len(conf.AppSKey) != 32 {
		return resource.NewConfigValidationError(path, ErrAppSKeyLength)
	}
	if conf.NwkSKey == "" {
		return resource.NewConfigValidationError(path, ErrNwkSKeyRequired)
	}
	if len(conf.NwkSKey) != 32 {
		return resource.NewConfigValidationError(path, ErrNwkSKeyLength)
	}
	if conf.DevAddr == "" {
		return resource.NewConfigValidationError(path, ErrDevAddrRequired)
	}
	if len(conf.DevAddr) != 8 {
		return resource.NewConfigValidationError(path, ErrDevAddrLength)
	}

	return nil
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

	FCntDown  uint32
	FPort     byte     // for downlinks, only required when frame payload exists.
	Downlinks [][]byte // list of downlink frame payloads to send

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

	err = n.ReconfigureWithConfig(ctx, deps, cfg)
	if err != nil {
		return err
	}

	err = CheckCaptureFrequency(conf, *cfg.Interval, n.logger)
	if err != nil {
		return err
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
	// TODO: expand to multiple gateways
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
