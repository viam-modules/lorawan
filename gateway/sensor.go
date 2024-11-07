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
	"errors"
	"gateway/gpio"
	"gateway/node"
	"sync"
	"time"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan gateway model.
var Model = resource.NewModel("viam", "lorawan", "sx1302-gateway")

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
	mu      sync.Mutex

	lastReadings map[string]interface{} // map of devices to readings
	readingsMu   sync.Mutex

	devices map[string]*node.Node // map of name to devices

}

func newGateway(
	ctx context.Context,
	_ resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	_, err := resource.NativeConfig[*Config](conf)
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

	g.devices = make(map[string]*node.Node)
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
	g.workers.Add(func(ctx context.Context) {
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
	})
}

func (g *Gateway) updateReadings(name string, newReadings map[string]interface{}) {
	g.readingsMu.Lock()
	defer g.readingsMu.Unlock()
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

func (g *Gateway) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := cmd["validate"]; ok {
		return map[string]interface{}{"validate": 1}, nil
	}

	// if newNode, ok := cmd["validate"]; ok {
	// 	return map[string]interface{}{"validate": 1}, nil
	// }

	// fmt.Println("IN DO COMMAND")
	// // Register the nodes
	// newNode, ok := cmd["register_device"]
	// fmt.Println(newNode)
	// if !ok {
	// 	return map[string]interface{}{}, errors.New("couldnt get node")
	// }

	// newN, ok := newNode.(map[string]interface{})

	// fmt.Println(newN)
	// fmt.Println(ok)
	// fmt.Println("Type:", reflect.TypeOf(newNode))

	// fmt.Println(newN["Addr"])

	// // map[Addr:[226 115 101 102] AlwaysRebuild:map[] AppKey:[] AppSKey:[85 114 64 76 105 110 107 76 111 82 97 50 48 49 56 35] DecoderPath:/home/olivia/decoder.js DevEui:[] Named:map[]]

	// node := &node.Node{DecoderPath: newN["DecoderPath"].(string)}

	// node.AppSKey = []byte(newN["AppSKey"].(string))
	// node.AppKey = []byte(newN["AppKey"].(string))

	// // node.DevEui = newN["DevEui"].([]byte)
	// // node.Addr = newN["Addr"].([]byte)

	// g.devices["dragino"] = node

	return nil, nil

}
func (g *Gateway) Close(ctx context.Context) error {
	g.workers.Stop()
	C.stopGateway()
	return nil
}

func (g *Gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	g.readingsMu.Lock()
	defer g.readingsMu.Unlock()
	return g.lastReadings, nil
}
