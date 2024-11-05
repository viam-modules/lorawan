package gateway

/*
#cgo CFLAGS: -I./sx1302/libloragw/inc -I./sx1302/libtools/inc
#cgo LDFLAGS: -L./sx1302/libloragw -lloragw -L./sx1302/libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdlib.h>

*/
import "C"
import (
	"context"
	"encoding/hex"
	"errors"
	"gateway/gpio"
	"time"

	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan gateway model.
var Model = resource.NewModel("viam", "lorawan", "sx1302-gateway")

const (
	joinRx2WindowSec = 6         // rx2 delay for sending join accept message.
	rx2Frequenecy    = 923300000 // Frequuency for rx2 window
	rx2SF            = 12        // spreading factor for rx2 window
	rx2Bandwidth     = 0x06      // 500k bandwidth
)

// Config describes the configuration of the gateway
type Config struct {
	Devices []DeviceConfig `json:"devices"`
}

type DeviceConfig struct {
	Name        string `json:"name"`
	JoinType    string `json:"join_type,omitempty"`
	DevEUI      string `json:"dev_eui,omitempty"`
	AppSKey     string `json:"app_s_key,omitempty"`
	NwkSKey     string `json:"network_s_key,omitempty"`
	DevAddr     string `json:"dev_addr,omitempty"`
	DecoderPath string `json:"decoder_path"`
	AppKey      string `json:"app_key,omitempty"`
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
			Constructor: newGateway,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	for _, d := range conf.Devices {
		if d.DecoderPath == "" {
			return nil, resource.NewConfigValidationError(path,
				errors.New("decoder path is required"))
		}
		switch d.JoinType {
		case "ABP":
			return d.validateABPAttributes(path)
		case "OTAA", "":
			return d.validateOTAAAttributes(path)
		default:
			return nil, resource.NewConfigValidationError(path,
				errors.New("join type is OTAA or ABP"))
		}

	}
	return nil, nil
}

func (conf *DeviceConfig) validateOTAAAttributes(path string) ([]string, error) {
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

func (conf *DeviceConfig) validateABPAttributes(path string) ([]string, error) {
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
			errors.New("ntowkr session key must be 16 bytes"))
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

type Gateway struct {
	resource.Named
	resource.AlwaysRebuild
	logger logging.Logger

	workers *utils.StoppableWorkers

	lastReadings map[string]interface{} // map devices to their last reading

	appSKey string // app session key used to decrypt messages.

	devices map[string]*Device
}

func newGateway(
	ctx context.Context,
	_ resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}

	g := &Gateway{
		Named:        conf.ResourceName().AsNamed(),
		logger:       logger,
		lastReadings: map[string]interface{}{},
	}

	// // Start and reset the radio
	gpio.InitGPIO()
	gpio.ResetGPIO()

	errCode := C.setUpGateway(C.int(0))
	if errCode != 0 {
		return nil, errors.New("failed to start the gateway")
	}

	g.devices = make(map[string]*Device)

	for _, device := range cfg.Devices {
		dev := &Device{
			name:        device.Name,
			decoderPath: device.DecoderPath,
		}

		switch device.JoinType {
		case "OTAA", "":
			appKey, err := hex.DecodeString(device.AppKey)
			if err != nil {
				return nil, err
			}
			dev.AppKey = types.AES128Key(appKey)

			devEui, err := hex.DecodeString(device.DevEUI)
			if err != nil {
				return nil, err
			}
			dev.devEui = devEui
		case "ABP":
			devAddr, err := hex.DecodeString(device.DevAddr)
			if err != nil {
				return nil, err
			}

			dev.addr = devAddr

			appSKey, err := hex.DecodeString(device.AppSKey)
			if err != nil {
				return nil, err
			}

			dev.appSKey = types.AES128Key(appSKey)
		}
		g.devices[device.Name] = dev
	}

	g.receivePackets()

	return g, nil
}

func (g *Gateway) receivePackets() {
	// receive the radio packets
	packet := C.createRxPacketArray()
	g.workers = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			numPackets := int(C.receive(packet))
			switch numPackets {
			case 0:
				// no packet received, wait 10 ms to receive again.
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			case 1:
				// received a LORA packet
				var payload []byte
				for i := 0; i < numPackets; i++ {
					if packet.size == 0 {
						continue
					}
					// Convert packet to go byte array
					for i := 0; i < int(packet.size); i++ {
						payload = append(payload, byte(packet.payload[i]))
					}
					g.handlePacket(ctx, payload)
				}
			default:
				g.logger.Errorf("error receiving lora packet")
			}
		}
	})
}

func (g *Gateway) handlePacket(ctx context.Context, payload []byte) {
	// first byte is MHDR - specifies message type
	switch payload[0] {
	case 0x0:
		g.logger.Infof("recieved join request")
		err := g.handleJoin(ctx, payload)
		if err != nil {
			g.logger.Errorf("couldn't handle join request: %w", err)
		}
	case 0x40:
		g.logger.Infof("received data uplink")
		name, readings, err := g.parseDataUplink(payload)
		if err != nil {
			g.logger.Errorf("error parsing uplink message: %w", err)
		}
		g.updateReadings(name, readings)

	default:
		g.logger.Warnf("received unsupported packet type")
	}
}

func (g *Gateway) updateReadings(name string, newReadings map[string]interface{}) {
	readings, ok := g.lastReadings[name].(map[string]interface{})
	if !ok {
		// readings for this device does not exist yet
		g.lastReadings[name] = newReadings
		return
	}

	if readings == nil {
		g.lastReadings[name] = make(map[string]interface{})
	}
	for key, val := range newReadings {
		readings[key] = val
	}

	g.lastReadings[name] = readings
}
func (g *Gateway) Close(ctx context.Context) error {
	g.workers.Stop()
	C.stopGateway()
	return nil
}

func (g *Gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return g.lastReadings, nil
}
