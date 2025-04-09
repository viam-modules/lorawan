package networkserver

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string("network-server"))

// Error variables for validation and operations.
var (
	// Config validation errors.
	errInvalidSpiBus = errors.New("spi bus can be 0 or 1 - default 0")

	// Gateway operation errors.
	errUnexpectedJoinType = errors.New("unexpected join type when adding node to gateway")
	errInvalidNodeMapType = errors.New("expected node map val to be type []interface{}, but it wasn't")
	errInvalidByteType    = errors.New("expected node byte array val to be float64, but it wasn't")
	errNoDevice           = errors.New("received packet from unknown device")
	errInvalidMIC         = errors.New("invalid MIC")
	errInvalidRegion      = errors.New("unrecognized region code, valid options are US915 and EU868")
)

// constants for MHDRs of different message types.
const (
	joinRequestMHdr         = 0x00
	joinAcceptMHdr          = 0x20
	unconfirmedUplinkMHdr   = 0x40
	unconfirmedDownLinkMHdr = 0x60
	confirmedUplinkMHdr     = 0x80
)

var noReadings = map[string]interface{}{"": "no readings available yet"}

// Define the map of SF to minimum SNR values in dB.
// Any packet received below the minimum demodulation value will not be parsed.
// Values found at https://www.thethingsnetwork.org/docs/lorawan/rssi-and-snr/
var sfToSNRMin = map[int]float64{
	7:  -7.5,
	8:  -8.5,
	9:  -9.5,
	10: -10.5,
	11: -11.5,
	12: -12.5,
}

// Config describes the configuration of the gateway.
type Config struct {
	Gateways []string `json:"gateways"`
}

// deviceInfo is a struct containing OTAA device information.
// This info is saved across module restarts for each device.
type deviceInfo struct {
	DevEUI            string  `json:"dev_eui"`
	DevAddr           string  `json:"dev_addr"`
	AppSKey           string  `json:"app_skey"`
	NwkSKey           string  `json:"nwk_skey"`
	FCntDown          *uint16 `json:"fcnt_down"`
	NodeName          string  `json:"node_name"`
	MinUplinkInterval float64 `json:"min_uplink_interval"`
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: NewNetworkServer,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	var deps []string

	for _, gateway := range conf.Gateways {
		deps = append(deps, gateway)
	}
	// error if no gateways

	return deps, nil
}

type NetworkServer struct {
	resource.Named
	resource.AlwaysRebuild
	logger logging.Logger
	mu     sync.Mutex

	receivingWorker *utils.StoppableWorkers

	lastReadings map[string]interface{} // map of devices to readings
	readingsMu   sync.Mutex

	devices    map[string]*node.Node // map of node name to node struct
	db         *sql.DB               // store device information/keys for use across restarts in a database
	regionInfo regions.RegionInfo
	region     regions.Region

	gateways []sensor.Sensor
}

// NewGateway creates a new gateway.
func NewNetworkServer(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}

	ns := &NetworkServer{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
	}

	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")

	if err := ns.setupSqlite(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if err := ns.migrateDevicesFromJSONFile(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if len(deps) == 0 {
		return nil, errors.New("must add sx1302-gateway as dependency")
	}

	gateway, err := sensor.FromDependencies(deps, cfg.Gateways[0])
	if err != nil {
		return nil, err
	}

	ns.gateways = append(ns.gateways, gateway)

	// maintain devices and lastReadings through reconfigure.
	if ns.devices == nil {
		ns.devices = make(map[string]*node.Node)
	}

	if ns.lastReadings == nil {
		ns.lastReadings = make(map[string]interface{})
	}

	ns.regionInfo = regions.RegionInfoUS

	ns.receivingWorker = utils.NewBackgroundStoppableWorkers(ns.receivePackets)

	return ns, nil
}

func (ns *NetworkServer) receivePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		for _, gateway := range ns.gateways {
			cmd := map[string]interface{}{"get_packets": 1}
			packets, err := gateway.DoCommand(ctx, cmd)
			if err != nil {
				ns.logger.Errorf("failed to call gateway do command %w", packets)
				continue
			}
			if ps, ok := packets["get_packets"].([]interface{}); ok {
				ns.logger.Infof("NS got a packet length of ps : %d", len(ps))
				for _, p := range ps {
					packet, ok := p.(map[string]interface{})
					if !ok {
						ns.logger.Errorf("expected map[string]interface{} got %v", reflect.TypeOf(p))
						continue
					}
					rxPacket, err := convertToRxPacket(packet)
					if err != nil {
						ns.logger.Errorf("failed to convert to rx packet %w", rxPacket)
					}
					// todo: use the time sent in the c packet
					ns.handlePacket(ctx, rxPacket, time.Now(), gateway)
				}
			} else {
				ns.logger.Errorf("expected []map[string]interface{}, got %v", reflect.TypeOf(packets["get_packets"]))
			}

			// de duplicate packets from dif gateways

		}
	}
}

func convertToRxPacket(packet map[string]interface{}) (lorahw.RxPacket, error) {

	rxPacket := lorahw.RxPacket{}
	var err error
	rxPacket.Payload, err = convertToBytes(packet["Payload"])
	if err != nil {
		return lorahw.RxPacket{}, err
	}
	rxPacket.SNR = packet["SNR"].(float64)
	rxPacket.DataRate = int(packet["DataRate"].(float64))
	rxPacket.Size = uint(packet["Size"].(float64))

	return rxPacket, nil

}

func (ns *NetworkServer) handlePacket(ctx context.Context, packet lorahw.RxPacket, packetTime time.Time, gateway sensor.Sensor) {
	// first byte is MHDR - specifies message type
	switch packet.Payload[0] {
	case joinRequestMHdr:
		ns.logger.Debugf("received join request")
		if err := ns.handleJoin(ctx, packet.Payload, packetTime, gateway); err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			ns.logger.Errorf("couldn't handle join request: %v", err)
		}
	case unconfirmedUplinkMHdr:
		name, readings, err := ns.parseDataUplink(ctx, packet.Payload, packetTime, packet.SNR, packet.DataRate, gateway)
		if err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			ns.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		ns.updateReadings(name, readings)
	case confirmedUplinkMHdr:
		name, readings, err := ns.parseDataUplink(ctx, packet.Payload, packetTime, packet.SNR, packet.DataRate, gateway)
		if err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			ns.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		ns.updateReadings(name, readings)

	default:
		ns.logger.Warnf("received unsupported packet type with mhdr %x", packet.Payload[0])
	}
}

func (ns *NetworkServer) updateReadings(name string, newReadings map[string]interface{}) {
	ns.readingsMu.Lock()
	defer ns.readingsMu.Unlock()
	readings, ok := ns.lastReadings[name].(map[string]interface{})
	if !ok {
		// readings for this device does not exist yet
		ns.lastReadings[name] = newReadings
		return
	}

	if readings == nil {
		ns.lastReadings[name] = make(map[string]interface{})
	}
	for key, val := range newReadings {
		readings[key] = val
	}

	ns.lastReadings[name] = readings
}

// DoCommand validates that the dependency is a gateway, and adds and removes nodes from the device maps.
func (g *NetworkServer) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Validate that the dependency is correct, returns the gateway's region.
	if _, ok := cmd["validate"]; ok {
		return map[string]interface{}{"validate": 1}, nil
	}

	// Add the nodes to the list of devices.
	if newNode, ok := cmd["register_device"]; ok {
		if newN, ok := newNode.(map[string]interface{}); ok {
			node, err := convertToNode(newN)
			if err != nil {
				return nil, err
			}

			oldNode, exists := g.devices[node.NodeName]
			switch exists {
			case false:
				g.devices[node.NodeName] = node
			case true:
				// node with that name already exists, merge them
				mergedNode, err := mergeNodes(node, oldNode)
				if err != nil {
					return nil, err
				}
				g.devices[node.NodeName] = mergedNode
			}
			// Check if the device is in the persistent data file, if it is add the OTAA info.
			deviceInfo, err := g.findDeviceInDB(ctx, hex.EncodeToString(g.devices[node.NodeName].DevEui))
			if err != nil {
				if !errors.Is(err, errNoDeviceInDB) {
					return nil, fmt.Errorf("error while searching for device in file: %w", err)
				}
			}
			// device was found in the file, update the gateway's device map with the device info.
			err = g.updateDeviceInfo(g.devices[node.NodeName], &deviceInfo)
			if err != nil {
				return nil, fmt.Errorf("error while updating device info: %w", err)
			}
		}
	}
	// Remove a node from the device map and readings map.
	if name, ok := cmd["remove_device"]; ok {
		if n, ok := name.(string); ok {
			delete(g.devices, n)
			g.readingsMu.Lock()
			delete(g.lastReadings, n)
			g.readingsMu.Unlock()
		}
	}
	if payload, ok := cmd[node.GatewaySendDownlinkKey]; ok {
		downlinks, ok := payload.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected a map[string]interface{} but got %v", reflect.TypeOf(payload))
		}

		for name, payload := range downlinks {
			dev, ok := g.devices[name]
			if !ok {
				return nil, fmt.Errorf("node with name %s not found", name)
			}

			strPayload, ok := payload.(string)
			if !ok {
				return nil, fmt.Errorf("expected a string value but got %v", reflect.TypeOf(strPayload))
			}

			payloadBytes, err := hex.DecodeString(strPayload)
			if err != nil {
				return nil, errors.New("failed to decode string to bytes")
			}

			dev.Downlinks = append(dev.Downlinks, payloadBytes)
		}

		return map[string]interface{}{node.GatewaySendDownlinkKey: "downlink added"}, nil
	}

	// return all devices that have been registered on the gateway
	if _, ok := cmd["return_devices"]; ok {
		resp := map[string]interface{}{}
		// Read the device info from the file
		devices, err := g.getAllDevicesFromDB(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to read device info from db: %w", err)
		}
		for _, device := range devices {
			resp[device.DevEUI] = device
		}
		return resp, nil
	}

	if devEUI, ok := cmd[node.GetDeviceKey]; ok {
		resp := map[string]interface{}{}
		deveui, ok := (devEUI).(string)
		if !ok {
			return nil, fmt.Errorf("expected a string but got %v", reflect.TypeOf(devEUI))
		}
		// Read the device info from the db
		device, err := g.findDeviceInDB(ctx, deveui)
		if err != nil {
			if errors.Is(err, errNoDeviceInDB) {
				// the device hasn't joined yet - return nil to device
				resp[node.GetDeviceKey] = nil
				return resp, nil
			}
			return nil, fmt.Errorf("failed to read device info from db: %w", err)
		}
		resp[node.GetDeviceKey] = device
		return resp, nil
	}

	return map[string]interface{}{}, nil
}

// updateDeviceInfo adds the device info from the db into the gateway's device map.
func (g *NetworkServer) updateDeviceInfo(device *node.Node, d *deviceInfo) error {
	// Update the fields in the map with the info from the file.
	appsKey, err := hex.DecodeString(d.AppSKey)
	if err != nil {
		return fmt.Errorf("failed to decode file's app session key: %w", err)
	}

	savedAddr, err := hex.DecodeString(d.DevAddr)
	if err != nil {
		return fmt.Errorf("failed to decode file's dev addr: %w", err)
	}

	nwksKey, err := hex.DecodeString(d.NwkSKey)
	if err != nil {
		return fmt.Errorf("failed to decode file's nwk session key: %w", err)
	}

	device.AppSKey = appsKey
	device.Addr = savedAddr
	device.NwkSKey = nwksKey
	device.MinIntervalSeconds = d.MinUplinkInterval

	// if we don't have an FCntDown in the device file, set it to a max number so we can tell.
	device.FCntDown = math.MaxUint16
	if d.FCntDown != nil {
		device.FCntDown = *d.FCntDown
	}

	// Update the device in the map.
	g.devices[device.NodeName] = device
	return nil
}

// mergeNodes merge the fields from the oldNode and the newNode sent from reconfigure.
func mergeNodes(newNode, oldNode *node.Node) (*node.Node, error) {
	mergedNode := &node.Node{}
	mergedNode.DecoderPath = newNode.DecoderPath
	mergedNode.NodeName = newNode.NodeName
	mergedNode.JoinType = newNode.JoinType
	mergedNode.FPort = newNode.FPort
	mergedNode.MinIntervalSeconds = newNode.MinIntervalSeconds

	switch mergedNode.JoinType {
	case "OTAA":
		// if join type is OTAA - keep the appSKey, dev addr from the old node.
		// These fields were determined by the gateway if the join procedure was done.
		mergedNode.Addr = oldNode.Addr
		mergedNode.AppSKey = oldNode.AppSKey
		// The appkey and deveui are obtained by the config in OTAA,
		// if these were changed during reconfigure the join procedure needs to be redone
		mergedNode.AppKey = newNode.AppKey
		mergedNode.DevEui = newNode.DevEui
	case "ABP":
		// if join type is ABP get the new appSKey and addr from the new config.
		// Don't need appkey and DevEui for ABP.
		mergedNode.Addr = newNode.Addr
		mergedNode.AppSKey = newNode.AppSKey
	default:
		return nil, errUnexpectedJoinType
	}

	return mergedNode, nil
}

// convertToNode converts the map from the docommand into the node struct.
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

	node.FPort = byte(mapNode["FPort"].(float64))
	node.NodeName = mapNode["NodeName"].(string)
	node.JoinType = mapNode["JoinType"].(string)

	return node, nil
}

// convertToBytes converts the interface{} field from the docommand map into a byte array.
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

// Close closes the gateway.
func (ns *NetworkServer) Close(ctx context.Context) error {
	if ns.db != nil {
		if err := ns.db.Close(); err != nil {
			ns.logger.Errorf("error closing data db: %s", err)
		}
	}

	if ns.receivingWorker != nil {
		ns.receivingWorker.Stop()
	}
	return nil
}

// Readings returns all the node's readings.
func (ns *NetworkServer) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	ns.readingsMu.Lock()
	defer ns.readingsMu.Unlock()

	// no readings available yet
	if len(ns.lastReadings) == 0 || ns.lastReadings == nil {
		// Tell the collector not to capture the empty data.
		if extra[data.FromDMString] == true {
			return map[string]interface{}{}, data.ErrNoCaptureToStore
		}
		return noReadings, nil
	}

	return ns.lastReadings, nil
}
