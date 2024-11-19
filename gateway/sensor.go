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

// Error variables for validation and operations
var (
	// Config validation errors
	errResetPinRequired = errors.New("reset pin is required")
	errInvalidSpiBus    = errors.New("spi bus can be 0 or 1 - default 0")

	// Gateway operation errors
	errStartGateway       = errors.New("failed to start the gateway")
	errUnexpectedJoinType = errors.New("unexpected join type when adding node to gateway")
	errInvalidNodeMapType = errors.New("expected node map val to be type []interface{}, but it wasn't")
	errInvalidByteType    = errors.New("expected node byte array val to be float64, but it wasn't")
	errNoDevice           = errors.New("received packet from unknown device")
	errInvalidMIC         = errors.New("invalid MIC")
	errSendJoinAccept     = errors.New("failed to send join accept packet")
)

// Model represents a lorawan gateway model.
var Model = resource.NewModel("viam", "lorawan", "sx1302-gateway")

// Config describes the configuration of the gateway
type Config struct {
	Bus      int  `json:"spi_bus,omitempty"`
	PowerPin *int `json:"power_en_pin,omitempty"`
	ResetPin *int `json:"reset_pin"`
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
	if conf.ResetPin == nil {
		return nil, resource.NewConfigValidationError(path, errResetPinRequired)
	}
	if conf.Bus != 0 && conf.Bus != 1 {
		return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
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

	devices map[string]*node.Node // map of node name to node struct

	started bool
}

func newGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g := &Gateway{
		Named:   conf.ResourceName().AsNamed(),
		logger:  logger,
		started: false,
	}

	err := g.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return g, nil
}

func (g *Gateway) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	if g.started {
		err = g.Close(ctx)
		if err != nil {
			return err
		}
		g.started = false
	}

	if g.devices == nil {
		g.devices = make(map[string]*node.Node)
	}

	if g.lastReadings == nil {
		g.lastReadings = make(map[string]interface{})
	}

	gpio.InitGateway(cfg.ResetPin, cfg.PowerPin)

	errCode := C.setUpGateway(C.int(cfg.Bus))
	if errCode != 0 {
		return errStartGateway
	}

	g.started = true
	g.receivePackets()

	return nil
}

func (g *Gateway) receivePackets() {
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
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			case 1:
				var payload []byte
				for i := 0; i < numPackets; i++ {
					if packet.size == 0 {
						continue
					}
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
		switch payload[0] {
		case 0x0:
			g.logger.Infof("received join request")
			err := g.handleJoin(ctx, payload)
			if err != nil {
				if errors.Is(errNoDevice, err) {
					return
				}
				g.logger.Errorf("couldn't handle join request: %s", err)
			}
		case 0x40:
			g.logger.Infof("received data uplink")
			name, readings, err := g.parseDataUplink(ctx, payload)
			if err != nil {
				if errors.Is(errNoDevice, err) {
					return
				}
				g.logger.Errorf("error parsing uplink message: %s", err)
				return
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

	if newNode, ok := cmd["register_device"]; ok {
		if newN, ok := newNode.(map[string]interface{}); ok {
			node, err := convertToNode(newN)
			if err != nil {
				return nil, err
			}

			oldNode, exists := g.devices[node.NodeName]
			if !exists {
				g.devices[node.NodeName] = node
				return map[string]interface{}{}, nil
			}
			mergedNode, err := mergeNodes(node, oldNode)
			if err != nil {
				return nil, err
			}
			g.devices[node.NodeName] = mergedNode
		}
	}

	if name, ok := cmd["remove_device"]; ok {
		if n, ok := name.(string); ok {
			delete(g.devices, n)
			g.readingsMu.Lock()
			delete(g.lastReadings, n)
			g.readingsMu.Unlock()
		}
	}

	return map[string]interface{}{}, nil
}

func mergeNodes(newNode, oldNode *node.Node) (*node.Node, error) {
	mergedNode := &node.Node{}
	mergedNode.DecoderPath = newNode.DecoderPath
	mergedNode.NodeName = newNode.NodeName
	mergedNode.JoinType = newNode.JoinType

	switch mergedNode.JoinType {
	case "OTAA":
		mergedNode.Addr = oldNode.Addr
		mergedNode.AppSKey = oldNode.AppSKey
		mergedNode.AppKey = newNode.AppKey
		mergedNode.DevEui = newNode.DevEui
	case "ABP":
		mergedNode.Addr = newNode.Addr
		mergedNode.AppSKey = newNode.AppSKey
	default:
		return nil, errUnexpectedJoinType
	}

	return mergedNode, nil
}

func convertToNode(mapNode map[string]interface{}) (*node.Node, error) {
	node := &node.Node{DecoderPath: mapNode["DecoderPath"].(string)}

	var err error
	node.AppKey, err = convertToBytes(mapNode["AppKey"])
	if err != nil {
		return nil, err
	}
	node.AppSKey, err = convertToBytes(mapNode["AppSKey"])
	if err != nil {
		return nil, err
	}
	node.DevEui, err = convertToBytes(mapNode["DevEui"])
	if err != nil {
		return nil, err
	}
	node.Addr, err = convertToBytes(mapNode["Addr"])
	if err != nil {
		return nil, err
	}

	node.NodeName = mapNode["NodeName"].(string)
	node.JoinType = mapNode["JoinType"].(string)

	return node, nil
}

func convertToBytes(key interface{}) ([]byte, error) {
	bytes, ok := key.([]interface{})
	if !ok {
		return nil, errInvalidNodeMapType
	}
	res := make([]byte, 0)

	if len(bytes) > 0 {
		for _, b := range bytes {
			val, ok := b.(float64)
			if !ok {
				return nil, errInvalidByteType
			}
			res = append(res, byte(val))
		}
	}

	return res, nil
}

func (g *Gateway) Close(ctx context.Context) error {
	if g.workers != nil {
		g.workers.Stop()
	}
	errCode := C.stopGateway()
	if errCode != 0 {
		g.logger.Errorf("error stopping gateway")
	}
	return nil
}

func (g *Gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	g.readingsMu.Lock()
	defer g.readingsMu.Unlock()
	return g.lastReadings, nil
}
