// Package node implements the node model
package node

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
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
	ErrInvalidFPort = errors.New("invalid fport value, must be one byte hex value between 01-DF. Value can be found in device's user manual")
)

const (
	// JoinTypeOTAA is the OTAA Join type.
	JoinTypeOTAA = "OTAA"
	// JoinTypeABP is the ABP Join type.
	JoinTypeABP = "ABP"
	// DownlinkKey is the DoCommand Key to send a payload to the gateway.
	DownlinkKey = "send_downlink"
	// GatewaySendDownlinkKey is the DoCommand Key for the gateway model to queue a downlink.
	GatewaySendDownlinkKey = "add_downlink_to_queue"
)

// NoReadings is the return for a sensor that has not received data.
var NoReadings = map[string]interface{}{"": "no readings available yet"}

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

	if conf.FPort != "" {
		fPort, err := hex.DecodeString(conf.FPort)
		if err != nil {
			return nil, resource.NewConfigValidationError(path, ErrInvalidFPort)
		}
		// Valid lorawan frame ports are 1-254.
		if fPort[0] <= byte(0x00) || fPort[0] > byte(0xDF) || len(fPort) > 1 {
			return nil, resource.NewConfigValidationError(path, ErrInvalidFPort)
		}
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

	DecoderPath   string
	NodeName      string
	gateway       sensor.Sensor
	JoinType      string
	reconfigureMu sync.Mutex

	FCntDown  uint16
	FPort     byte     // for downlinks, only required when frame payload exists.
	Downlinks [][]byte // list of downlink frame payloads to send

	Region             regions.Region
	MinIntervalSeconds float64 // estimated minimum uplink interval

	Workers *utils.StoppableWorkers
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

	// call this once outside of background thread to get any info gateway has before calling the interval request.
	n.GetAndUpdateDeviceInfo(ctx)
	// Start the background routine only if it hasn't been started previously.
	if n.Workers == nil {
		n.Workers = utils.NewBackgroundStoppableWorkers(n.PollGateway)
	}

	err = CheckCaptureFrequency(conf, *cfg.Interval, n.logger)
	if err != nil {
		return err
	}

	return nil
}

// validateGateway sends the validate docommand to the gateway, and saves region and gateway to struct.
func (n *Node) validateGateway(ctx context.Context, deps resource.Dependencies) error {
	if len(deps) == 0 {
		return errors.New("must add sx1302-gateway as dependency")
	}
	var dep resource.Resource

	// Assuming there's only one dep.
	// TODO: expand to multiple gateways
	for _, val := range deps {
		dep = val
	}

	gateway, ok := dep.(sensor.Sensor)
	if !ok {
		return errors.New("dependency must be the sx1302-gateway sensor")
	}

	cmd := make(map[string]interface{})
	cmd["validate"] = 1

	// Validate that the dependency is the gateway - gateway will return the region.
	ret, err := gateway.DoCommand(ctx, cmd)
	if err != nil {
		return err
	}

	retVal, ok := ret["validate"]
	if !ok {
		return errors.New("dependency must be the sx1302-gateway sensor")
	}
	re, ok := retVal.(float64)
	if !ok {
		return fmt.Errorf("expected float64 return, got %v", reflect.TypeOf(retVal))
	}

	switch re {
	case 1:
		n.Region = regions.US
	case 2:
		n.Region = regions.EU
	default:
		return errors.New("gateway return unexpected region")
	}

	n.gateway = gateway

	return nil
}

// Close removes the device from the gateway.
func (n *Node) Close(ctx context.Context) error {
	if n.Workers != nil {
		n.Workers.Stop()
	}
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
			return NoReadings, nil
		}
		return reading.(map[string]interface{}), nil
	}
	return map[string]interface{}{}, errors.New("node does not have gateway")
}

// DoCommand lets users send downlink commands from the node to the gateway.
func (n *Node) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	resp := map[string]interface{}{}
	testOnly := CheckTestKey(cmd)

	if payload, payloadSet := cmd[DownlinkKey]; payloadSet {
		if payloadString, payloadOk := payload.(string); payloadOk {
			// SendDownlink locks to prevent commands from being sent during reconfigure.
			return n.SendDownlink(ctx, payloadString, testOnly)
		}
		return map[string]interface{}{}, fmt.Errorf("error parsing payload, expected string got %v", reflect.TypeOf(payload))
	}

	return resp, nil
}
