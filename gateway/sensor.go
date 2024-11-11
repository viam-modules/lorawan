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
	Bus      int `json:"spi_bus,omitempty"`
	PowerPin int `json:"power_en_pin,omitempty"`
	ResetPin int `json:"reset_pin"`
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
	if conf.ResetPin == 0 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("reset pin is required"))
	}
	if conf.Bus != 0 && conf.Bus != 1 {
		return nil, resource.NewConfigValidationError(path,
			errors.New("spi bus can be 0 or 1 - default 0"))
	}
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

	devices map[string]*node.Node // map of device address to node struct

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

	// reset and start the gateway.
	gpio.InitGateway(cfg.ResetPin, cfg.PowerPin)

	errCode := C.setUpGateway(C.int(cfg.Bus))
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
			g.logger.Infof("received join request")
			err := g.handleJoin(ctx, payload)
			if err != nil {
				// don't log as error if it was a request from unknown device.
				if errors.Is(errNoDevice, err) {
					return
				}
				g.logger.Errorf("couldn't handle join request: %w", err)
			}
		case 0x40:
			g.logger.Infof("received data uplink")
			name, readings, err := g.parseDataUplink(ctx, payload)
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

	// Validate that the dependency is correct.
	if _, ok := cmd["validate"]; ok {
		return map[string]interface{}{"validate": 1}, nil
	}

	// Add the nodes to the list of devices.
	if newNode, ok := cmd["register_device"]; ok {
		if newN, ok := newNode.(map[string]interface{}); ok {
			node := convertToNode(newN)
			g.devices[node.NodeName] = node
		}
	}

	return nil, nil

}

// convertToNode converts the map from the docommand into the node struct.
func convertToNode(mapNode map[string]interface{}) *node.Node {
	node := &node.Node{DecoderPath: mapNode["DecoderPath"].(string)}

	node.AppKey = convertToBytes(mapNode["AppKey"])
	node.AppSKey = convertToBytes(mapNode["AppSKey"])
	node.DevEui = convertToBytes(mapNode["DevEui"])
	node.Addr = convertToBytes(mapNode["Addr"])
	node.NodeName = mapNode["NodeName"].(string)

	return node
}

// convertToBytes converts the interface{} field from the docommand map into a byte array.
func convertToBytes(key interface{}) []byte {
	bytes := key.([]interface{})
	res := make([]byte, 0)

	if len(bytes) > 0 {
		for _, b := range bytes {
			val, _ := b.(float64)
			res = append(res, byte(val))
		}
	}

	return res

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
