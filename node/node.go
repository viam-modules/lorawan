package node

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"

	"go.thethings.network/lorawan-stack/v3/pkg/types"
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

type Device struct {
	name        string
	decoderPath string

	nwkSKey types.AES128Key
	appSKey types.AES128Key
	AppKey  types.AES128Key

	addr   []byte
	devEui []byte
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
			errors.New("join type is OTAA or ABP"))
	}

	return nil, nil
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

	name        string
	decoderPath string

	nwkSKey types.AES128Key
	appSKey types.AES128Key
	AppKey  types.AES128Key

	addr   []byte
	devEui []byte
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
		decoderPath: cfg.DecoderPath,
	}

	gateway, err := sensor.FromDependencies(deps, name)
	if err != nil {
		return errors.Wrapf(err, "no movement sensor named (%s)", name)
	}

	switch cfg.JoinType {
	case "OTAA", "":
		appKey, err := hex.DecodeString(cfg.AppKey)
		if err != nil {
			return nil, err
		}
		n.AppKey = types.AES128Key(appKey)

		devEui, err := hex.DecodeString(cfg.DevEUI)
		if err != nil {
			return nil, err
		}
		n.devEui = devEui
	case "ABP":
		devAddr, err := hex.DecodeString(cfg.DevAddr)
		if err != nil {
			return nil, err
		}

		n.addr = devAddr

		appSKey, err := hex.DecodeString(cfg.AppSKey)
		if err != nil {
			return nil, err
		}

		n.appSKey = types.AES128Key(appSKey)
	}

	return n, nil
}

func (n *Node) Close(ctx context.Context) error {
	n.workers.Stop()
	return nil
}

func (g *Node) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
