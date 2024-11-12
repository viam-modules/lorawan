package node

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
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
	logger logging.Logger

	nwkSKey []byte
	AppSKey []byte
	AppKey  []byte

	Addr   []byte
	DevEui []byte

	DecoderPath string
	NodeName    string
	gateway     sensor.Sensor
}

func newNode(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	n := &Node{
		Named:    conf.ResourceName().AsNamed(),
		logger:   logger,
		NodeName: conf.ResourceName().AsNamed().Name().Name,
	}

	fmt.Println("build")

	n.Reconfigure(ctx, deps, conf)

	return n, nil
}

func (n *Node) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	fmt.Println("in RECONFIGURE node")

	gateway, err := getGateway(ctx, deps)
	if err != nil {
		return err
	}

	fmt.Println(n.NodeName)

	// node name changed, remove the old device name from the gateway
	if n.NodeName != conf.ResourceName().AsNamed().Name().Name {
		cmd := make(map[string]interface{})
		cmd["remove_device"] = n.NodeName
		n.NodeName = conf.ResourceName().AsNamed().Name().Name
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

	// send the device to the gateway.
	cmd := make(map[string]interface{})
	cmd["register_device"] = n

	_, err = gateway.DoCommand(ctx, cmd)
	if err != nil {
		return err
	}

	n.gateway = gateway
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
		return nil, errors.New("dependency must be the sx1302-gateway")
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
		return nil, errors.New("dependency must be the sx1302-gateway")
	}
	if retVal.(float64) != 1 {
		return nil, errors.New("dependency must be the sx1302-gateway")
	}
	return gateway, nil
}

func (n *Node) Close(ctx context.Context) error {
	return nil
}

func (n *Node) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	if n.gateway != nil {
		allReadings, err := n.gateway.Readings(ctx, nil)
		if err != nil {
			return map[string]interface{}{}, err
		}

		reading, ok := allReadings[n.NodeName]
		if !ok {
			// no readings available yet
			return map[string]interface{}{}, fmt.Errorf("no readings available yet: %w", data.ErrNoCaptureToStore)
		}
		return reading.(map[string]interface{}), nil
	}
	return map[string]interface{}{}, errors.New("node does not have gateway")

}
