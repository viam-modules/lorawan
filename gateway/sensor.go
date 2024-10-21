package gateway

/*
#cgo CFLAGS: -I./sx1302/libloragw/inc -I./sx1302/libtools/inc  -I./sx1302/packet_forwarder/inc
#cgo LDFLAGS: -L./sx1302/libloragw -lloragw -L./sx1302/libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdlib.h>

*/
import "C"
import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"gateway/gpio"
	"gateway/parser"
	"os"
	"time"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan gateway model.
var Model = resource.NewModel("viam", "lorawan", "sx1302")

const (
	radio0Freq = 902700000
	radio1Freq = 903700000
)

// Config describes the configuration of the gateway
type Config struct {
	Devices []DeviceConfig `json:"devices"`
}

type DeviceConfig struct {
	Name        string `json:"name"`
	DevEUI      string `json:"dev_eui"`
	AppSKey     string `json:"app_s_key"`
	DevAddr     string `json:"dev_addr"`
	DecoderPath string `json:"codec_path"`
}

type Device struct {
	name            string
	appSKey         []byte
	addr            []byte
	decoderPath     string
	lastReadingTime time.Time
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
	return nil, nil
}

type Gateway struct {
	resource.Named
	resource.AlwaysRebuild
	logger logging.Logger

	workers *utils.StoppableWorkers

	lastReadings map[string]interface{} // map devices to their last reading

	// devices map[string]string // map of names and device addresses - gateway will ignore any other devices.
	appSKey string // app session key used to decrypt messages.

	devices map[string]Device
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

	// set gateway HAT config
	err = setGatewayConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to configure gateway board: %w", err)
	}

	// set rf chain 0 config
	err = setRfChainConfig(0, radio0Freq)
	if err != nil {
		return nil, fmt.Errorf("failed to configure rf chain 0: %w", err)
	}

	// set rf chain 1 config
	err = setRfChainConfig(1, radio1Freq)
	if err != nil {
		return nil, fmt.Errorf("failed to configure rf chain 1: %w", err)
	}

	err = setIfChainConfigs()
	if err != nil {
		return nil, fmt.Errorf("failed to configure if chain: %w", err)
	}

	// start the gateway
	errorCode := int(C.startGateway())
	if errorCode != 0 {
		return nil, errors.New("failed to start the gateway")
	}

	g.devices = make(map[string]Device)

	for _, device := range cfg.Devices {
		devAddr, err := hex.DecodeString(device.DevAddr)
		if err != nil {
			return nil, err
		}

		appSKey, err := hex.DecodeString(device.AppSKey)
		if err != nil {
			return nil, err
		}

		dev := Device{
			name:        device.Name,
			appSKey:     appSKey,
			addr:        devAddr,
			decoderPath: device.DecoderPath,
		}
		g.devices[device.Name] = dev
	}

	g.receivePackets()

	return g, nil
}

func (g *Gateway) receivePackets() {
	// receive the radio packets
	packet := C.create_rxpkt_array()
	g.workers = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			numPackets := int(C.receive(packet))
			if numPackets < 0 || numPackets > 1 {
				g.logger.Errorf("error receiving lora packet")
			}
			if numPackets != 0 {
				var payload []byte
				for i := 0; i < numPackets; i++ {
					// Convert packet to go byte array
					for i := 0; i < int(packet.size); i++ {
						payload = append(payload, byte(packet.payload[i]))
					}
					g.handlePacket(payload)
				}
			} else {
				// wait 10 ms to receive another packet
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
	})
}

func (g *Gateway) handlePacket(payload []byte) {
	// first byte is MHDR - specifies message type
	switch payload[0] {
	case 0x0:
		g.logger.Errorf("OTAA not supported - use ABP")
	case 0x40:
		g.logger.Infof("received data uplink")
		name, readings, err := g.parseDataUplink(payload)
		if err != nil {
			g.logger.Errorf("error parsing uplink message: %w", err)
		} else if name != "" {
			// the milesight lora sensor sends two data uplink messages close together, we want both of these in the map.
			g.updateReadings(name, readings)
		}
	default:
		// want to keep track if any other messages are being sent from milesight device so writing to file.
		file, _ := os.OpenFile("/tmp/messages.txt", os.O_APPEND|os.O_RDWR|os.O_CREATE, 0666)
		defer file.Close()
		file.Write(payload)
		file.Write([]byte("\n"))
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
	for key, val := range readings {
		readings[key] = val
	}

	g.lastReadings[name] = readings

	if dev, ok := g.devices[name]; ok {
		dev.lastReadingTime = time.Now()
		g.devices[name] = dev
	}

}
func (g *Gateway) Close(ctx context.Context) error {
	g.workers.Stop()
	C.lgw_stop()
	return nil
}

func (g *Gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return g.lastReadings, nil
}

func matchDevice(devAddr []byte, devices map[string]Device) (Device, error) {
	for _, dev := range devices {
		if string(devAddr) == string(dev.addr) {
			return dev, nil
		}
	}
	return Device{}, errors.New("no match")
}

// Structure of phyPayload:
// | MHDR | DEV ADDR|  FCTL |   FCnt  | FPort   |  FOpts     |  FRM Payload | MIC |
// | 1 B  |   4 B    | 1 B   |  2 B   |   1 B   | variable    |  variable   | 4B  |
func (g *Gateway) parseDataUplink(phyPayload []byte) (string, map[string]interface{}, error) {

	/// devAddr is bytes one to five - little endian
	devAddr := phyPayload[1:5]

	// need to reserve the bytes because packet in LE but TN library expects BE.
	devAddr = parser.ReverseBytes(devAddr)

	device, err := matchDevice(devAddr, g.devices)
	if err != nil {
		g.logger.Infof("received packet from unknown device, ignoring")
		return "", map[string]interface{}{}, nil
	}

	dAddr := types.MustDevAddr(devAddr)

	// Frame control byte contains settings
	// the last 4 bits is the fopts length
	fctrl := phyPayload[5]
	foptsLength := fctrl & 0x0F

	// frame count - should increase by 1 with each packet sent
	frameCnt := binary.LittleEndian.Uint16(phyPayload[6:8])

	// fopts not supported in this module yet.
	if foptsLength != 0 {
		_ = phyPayload[8 : 8+foptsLength]
	}

	// frame port specifies application port - 0 is for MAC commands 1-255 for device messsages.
	fPort := phyPayload[8+foptsLength]

	framePayload := phyPayload[8+foptsLength+1 : len(phyPayload)-4]

	//TODO: validate the MIC - last 4 bytes

	key := types.AES128Key(device.appSKey)

	// decrypt the frame payload
	decryptedPayload, err := crypto.DecryptUplink(key, *dAddr, (uint32)(frameCnt), framePayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("error while decrypting uplink message: %w", err)
	}

	// decode using codec
	readings, err := parser.DecodePayload(fPort, device.decoderPath, decryptedPayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("Error decoding payload: %w", err)
	}

	return device.name, readings, nil
}
