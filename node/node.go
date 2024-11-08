package node

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan node model.
var Model = resource.NewModel("viam", "lorawan", "node")

type Config struct {
	JoinType    string `json:"join_type,omitempty"`
	DecoderPath string `json:"decoder_path"`
	DevEUI      string `json:"dev_eui,omitempty"`
	AppKey      string `json:"app_key,omitempty"`
	AppSKey     string `json:"app_s_key,omitempty"`
	NwkSKey     string `json:"network_s_key,omitempty"`
	DevAddr     string `json:"dev_addr,omitempty"`
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
		return nil, resource.NewConfigValidationError(path,
			errors.New("decoder path is required"))
	}
	switch conf.JoinType {
	case "ABP":
		return conf.validateABPAttributes(path)
	case "OTAA", "":
		return conf.validateOTAAAttributes(path)
	default:
		return nil, resource.NewConfigValidationError(path,
			errors.New("join type is OTAA or ABP - defaults to OTAA"))
	}
}

func (conf *Config) validateOTAAAttributes(path string) ([]string, error) {
	if conf.DevEUI == "" {
		return nil, resource.NewConfigValidationError(path,
			errors.New("dev EUI is required for OTAA join type"))
	}
	if len(conf.DevEUI) != 16 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("dev EUI must be 8 bytes"))
	}
	if conf.AppKey == "" {
		return nil, resource.NewConfigValidationError(path,
			errors.New("app key is required for OTAA join type"))
	}
	if len(conf.AppKey) != 32 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("app key must be 16 bytes"))
	}
	return nil, nil
}

func (conf *Config) validateABPAttributes(path string) ([]string, error) {
	if conf.AppSKey == "" {
		return nil, resource.NewConfigValidationError(path,
			errors.New("app session key is required for ABP join type"))
	}
	if len(conf.AppSKey) != 32 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("app session key must be 16 bytes"))
	}
	if conf.NwkSKey == "" {
		return nil, resource.NewConfigValidationError(path,
			errors.New("network session key is required for ABP join type"))
	}
	if len(conf.NwkSKey) != 32 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("network session key must be 16 bytes"))
	}
	if conf.DevAddr == "" {
		return nil, resource.NewConfigValidationError(path,
			errors.New("device address is required for ABP join type"))
	}
	if len(conf.DevAddr) != 8 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("device address must be 4 bytes"))
	}

	return nil, nil

}

type Node struct {
	resource.Named
	resource.AlwaysRebuild
	logger logging.Logger

	workers *utils.StoppableWorkers
	mu      sync.Mutex

	DecoderPath string

	nwkSKey []byte
	AppSKey []byte
	AppKey  []byte

	Addr   []byte
	DevEui []byte

	gateway  sensor.Sensor
	NodeName string
}

func newNode(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}

	n := &Node{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
		DecoderPath: cfg.DecoderPath,
		NodeName:    conf.ResourceName().AsNamed().Name().Name,
	}

	var dep resource.Resource
	for _, val := range deps {
		dep = val
	}

	gateway, ok := dep.(sensor.Sensor)
	if !ok {
		return nil, errors.New("dependency must be a sensor")
	}

	cmd := make(map[string]interface{})

	cmd["validate"] = 1

	// Validate that its the gateway
	ret, err := gateway.DoCommand(ctx, cmd)

	retVal, ok := ret["validate"]
	if !ok {
		return nil, errors.New("dependency must be the sx1302-gateway")
	}
	if retVal.(float64) != 1 {
		return nil, errors.New("dependency must be the sx1302-gateway")
	}
	n.gateway = gateway

	switch cfg.JoinType {
	case "OTAA", "":
		appKey, err := hex.DecodeString(cfg.AppKey)
		if err != nil {
			return nil, err
		}
		n.AppKey = appKey

		devEui, err := hex.DecodeString(cfg.DevEUI)
		if err != nil {
			return nil, err
		}
		n.DevEui = devEui
	case "ABP":
		devAddr, err := hex.DecodeString(cfg.DevAddr)
		if err != nil {
			return nil, err
		}

		n.Addr = devAddr

		appSKey, err := hex.DecodeString(cfg.AppSKey)
		if err != nil {
			return nil, err
		}

		n.AppSKey = appSKey
	}

	regCmd := make(map[string]interface{})

	regCmd["register_device"] = n

	_, err = gateway.DoCommand(ctx, regCmd)
	if err != nil {
		return nil, err
	}

	return n, nil
}

func (n *Node) Close(ctx context.Context) error {
	return nil
}

func (n *Node) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	allReadings, err := n.gateway.Readings(ctx, nil)
	if err != nil {
		return map[string]interface{}{}, err
	}

	reading, ok := allReadings[n.NodeName]
	if !ok {
		// no readings avaliable yet
		return map[string]interface{}{}, nil
	}

	return reading.(map[string]interface{}), nil
}
