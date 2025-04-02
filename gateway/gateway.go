// Package gateway implements the sx1302 gateway module.
package gateway

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// defining model names here to be reused in getNativeConfig.
const (
	oldModelName = "sx1302-gateway"
	genericHat   = "sx1302-hat-generic"
	waveshareHat = "sx1302-waveshare-hat"
)

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

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string(oldModelName))

// ModelGenericHat represents a lorawan gateway hat model.
var ModelGenericHat = node.LorawanFamily.WithModel(string(genericHat))

// ModelSX1302WaveshareHat represents a lorawan SX1302 Waveshare Hat gateway model.
var ModelSX1302WaveshareHat = node.LorawanFamily.WithModel(string(waveshareHat))

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

// LoggingRoutineStarted is a global variable to track if the captureCOutputToLogs goroutine has
// started for each gateway. If the gateway build errors and needs to build again, we only want to start
// the logging routine once.
var loggingRoutineStarted = make(map[string]bool)

var noReadings = map[string]interface{}{"": "no readings available yet"}

// Config describes the configuration of the gateway.
type Config struct {
	Bus       int    `json:"spi_bus,omitempty"`
	PowerPin  *int   `json:"power_en_pin,omitempty"`
	ResetPin  *int   `json:"reset_pin"`
	BoardName string `json:"board"`
	Region    string `json:"region_code,omitempty"`
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
			Constructor: NewGateway,
		})
	resource.RegisterComponent(
		sensor.API,
		ModelGenericHat,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: NewGateway,
		})
	resource.RegisterComponent(
		sensor.API,
		ModelSX1302WaveshareHat,
		resource.Registration[sensor.Sensor, *ConfigSX1302WaveshareHAT]{
			Constructor: newSX1302WaveshareHAT,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	var deps []string
	if conf.ResetPin == nil {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "reset_pin")
	}
	if conf.Bus != 0 && conf.Bus != 1 {
		return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
	}

	if len(conf.BoardName) == 0 {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}
	deps = append(deps, conf.BoardName)

	if conf.Region != "" {
		if regions.GetRegion(conf.Region) == regions.Unspecified {
			return nil, resource.NewConfigValidationError(path, errInvalidRegion)
		}
	}

	return deps, nil
}

// Gateway defines a lorawan gateway.
type gateway struct {
	resource.Named
	logger logging.Logger
	mu     sync.Mutex

	// Using two wait groups so we can stop the receivingWorker at reconfigure
	// but keep the loggingWorker running.
	loggingWorker   *utils.StoppableWorkers
	receivingWorker *utils.StoppableWorkers

	lastReadings map[string]interface{} // map of devices to readings
	readingsMu   sync.Mutex

	devices map[string]*node.Node // map of node name to node struct

	started bool
	rstPin  board.GPIOPin
	pwrPin  board.GPIOPin

	logReader  *os.File
	logWriter  *os.File
	db         *sql.DB // store device information/keys for use across restarts in a database
	regionInfo regions.RegionInfo
	region     regions.Region
}

// NewGateway creates a new gateway.
func NewGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g := &gateway{
		Named:   conf.ResourceName().AsNamed(),
		logger:  logger,
		started: false,
	}

	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")

	if err := g.setupSqlite(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if err := g.migrateDevicesFromJSONFile(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if err := g.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}

	return g, nil
}

// look at the resource.Config to determine which model is being used.
func getNativeConfig(conf resource.Config) (*Config, error) {
	switch conf.Model.Name {
	case genericHat, oldModelName:
		return resource.NativeConfig[*Config](conf)
	case waveshareHat:
		waveshareHatCfg, err := resource.NativeConfig[*ConfigSX1302WaveshareHAT](conf)
		if err != nil {
			return nil, fmt.Errorf("the config %v does not match a supported config type", conf)
		}
		return waveshareHatCfg.getGatewayConfig(), nil
	default:
		return nil, errors.New("build error in module. Unsupported Gateway model")
	}
}

// Reconfigure reconfigures the gateway.
func (g *gateway) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If the gateway hardware was already started, stop gateway and the background worker.
	// Make sure to always call stopGateway() before making any changes to the c config or
	// errors will occur.
	// Unexpected behavior will also occur if stopGateway() is called when the gateway hasn't been
	// started, so only call stopGateway if this module already started the gateway.
	if g.started {
		g.reset(ctx)
	}

	cfg, err := getNativeConfig(conf)
	if err != nil {
		return err
	}

	board, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
	}

	// capture C log output
	g.startCLogging()

	// maintain devices and lastReadings through reconfigure.
	if g.devices == nil {
		g.devices = make(map[string]*node.Node)
	}

	if g.lastReadings == nil {
		g.lastReadings = make(map[string]interface{})
	}

	// adding io before the pin allows you to use the GPIO number.
	rstPin, err := board.GPIOPinByName("io" + strconv.Itoa(*cfg.ResetPin))
	if err != nil {
		return err
	}
	g.rstPin = rstPin

	// not every gateway has a power enable pin so it is optional.
	if cfg.PowerPin != nil {
		pwrPin, err := board.GPIOPinByName("io" + strconv.Itoa(*cfg.PowerPin))
		if err != nil {
			return err
		}
		g.pwrPin = pwrPin
	}

	err = resetGateway(ctx, g.rstPin, g.pwrPin)
	if err != nil {
		return fmt.Errorf("error initializing the gateway: %w", err)
	}

	region := regions.GetRegion(cfg.Region)

	switch region {
	case regions.US, regions.Unspecified:
		g.logger.Infof("configuring gateway for US915 band")
		g.regionInfo = regions.RegionInfoUS
		g.region = regions.US
	case regions.EU:
		g.logger.Infof("configuring gateway for EU868 band")
		g.regionInfo = regions.RegionInfoEU
		g.region = regions.EU
	}

	if err := lorahw.SetupGateway(cfg.Bus, region); err != nil {
		return fmt.Errorf("failed to set up the gateway: %w", err)
	}

	g.started = true
	g.receivingWorker = utils.NewBackgroundStoppableWorkers(g.receivePackets)
	return nil
}

// startCLogging starts the goroutine to capture C logs into the logger.
// If loggingRoutineStarted indicates routine has already started, it does nothing.
func (g *gateway) startCLogging() {
	loggingState, ok := loggingRoutineStarted[g.Name().Name]
	if !ok || !loggingState {
		g.logger.Debug("Starting c logger background routine")
		stdoutR, stdoutW, err := os.Pipe()
		if err != nil {
			g.logger.Errorf("unable to create pipe for C logs")
			return
		}
		g.logReader = stdoutR
		g.logWriter = stdoutW

		// Redirect C's stdout to the write end of the pipe
		lorahw.RedirectLogsToPipe(g.logWriter.Fd())
		scanner := bufio.NewScanner(g.logReader)

		g.loggingWorker = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) { g.captureCOutputToLogs(ctx, scanner) })
		loggingRoutineStarted[g.Name().Name] = true
	}
}

// captureOutput is a background routine to capture C's stdout and log to the module's logger.
// This is necessary because the sx1302 library only uses printf to report errors.
func (g *gateway) captureCOutputToLogs(ctx context.Context, scanner *bufio.Scanner) {
	// Need to disable buffering on stdout so C logs can be displayed in real time.
	lorahw.DisableBuffering()

	// loop to read lines from the scanner and log them
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if scanner.Scan() {
				line := scanner.Text()
				switch {
				case strings.Contains(line, "STANDBY_RC mode"):
					// The error setting standby_rc mode indicates a hardware initiaization failure
					// add extra log to instruct user what to do.
					g.logger.Error(line)
					g.logger.Error("gateway hardware is misconfigured: unplug the board and wait a few minutes before trying again")
				case strings.Contains(line, "ERROR"):
					g.logger.Error(line)
				case strings.Contains(line, "WARNING"):
					g.logger.Warn(line)
				default:
					g.logger.Debug(line)
				}
			}
		}
	}
}

func (g *gateway) receivePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		packets, err := lorahw.ReceivePackets()
		if err != nil {
			g.logger.Errorf("error receiving lora packet: %w", err)
			continue
		}

		if len(packets) == 0 {
			// no packet received, wait 10 ms to receive again
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}

		t := time.Now()
		for i, packet := range packets {
			if packet.Size == 0 {
				continue
			}

			// don't process duplicates
			isDuplicate := false
			for j := i - 1; j >= 0; j-- {
				// two packets identical if payload is the same
				if slices.Equal(packets[j].Payload, packets[i].Payload) {
					g.logger.Debugf("skipped duplicate packet")
					isDuplicate = true
					break
				}
			}
			if isDuplicate {
				continue
			}
			minSNR := sfToSNRMin[packet.DataRate]
			if float64(packet.SNR) < minSNR {
				g.logger.Warnf("packet skipped due to low signal noise ratio: %v, min is %v", packet.SNR, minSNR)
				continue
			}

			g.handlePacket(ctx, packet.Payload, t, packet.SNR, packet.DataRate)
		}
	}
}

func (g *gateway) handlePacket(ctx context.Context, payload []byte, packetTime time.Time, snr float64, sf int) {
	// first byte is MHDR - specifies message type
	switch payload[0] {
	case joinRequestMHdr:
		g.logger.Debugf("received join request")
		if err := g.handleJoin(ctx, payload, packetTime); err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			g.logger.Errorf("couldn't handle join request: %v", err)
		}
	case unconfirmedUplinkMHdr:
		name, readings, err := g.parseDataUplink(ctx, payload, packetTime, snr, sf)
		if err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			g.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		g.updateReadings(name, readings)
	case confirmedUplinkMHdr:
		name, readings, err := g.parseDataUplink(ctx, payload, packetTime, snr, sf)
		if err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			g.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		g.updateReadings(name, readings)

	default:
		g.logger.Warnf("received unsupported packet type with mhdr %x", payload[0])
	}
}

func (g *gateway) updateReadings(name string, newReadings map[string]interface{}) {
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

// DoCommand validates that the dependency is a gateway, and adds and removes nodes from the device maps.
func (g *gateway) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Validate that the dependency is correct, returns the gateway's region.
	if _, ok := cmd["validate"]; ok {
		return map[string]interface{}{"validate": g.region}, nil
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
			return nil, fmt.Errorf("failed to read device info from db: %w", err)
		}
		resp[node.GetDeviceKey] = device
		return resp, nil
	}

	return map[string]interface{}{}, nil
}

// updateDeviceInfo adds the device info from the db into the gateway's device map.
func (g *gateway) updateDeviceInfo(device *node.Node, d *deviceInfo) error {
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
func (g *gateway) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reset(ctx)

	if g.loggingWorker != nil {
		if g.logReader != nil {
			if err := g.logReader.Close(); err != nil {
				g.logger.Errorf("error closing log reader: %s", err)
			}
		}
		if g.logWriter != nil {
			if err := g.logWriter.Close(); err != nil {
				g.logger.Errorf("error closing log writer: %s", err)
			}
		}
		g.loggingWorker.Stop()
		delete(loggingRoutineStarted, g.Name().Name)
	}

	if g.db != nil {
		if err := g.db.Close(); err != nil {
			g.logger.Errorf("error closing data db: %s", err)
		}
	}

	return nil
}

func (g *gateway) reset(ctx context.Context) {
	// close the routine that receives lora packets - otherwise this will error when the gateway is stopped.
	if g.receivingWorker != nil {
		g.receivingWorker.Stop()
	}
	if err := lorahw.StopGateway(); err != nil {
		g.logger.Error("error stopping gateway: %v", err)
	}
	if g.rstPin != nil {
		err := resetGateway(ctx, g.rstPin, g.pwrPin)
		if err != nil {
			g.logger.Error("error resetting the gateway")
		}
	}
	g.started = false
}

// Readings returns all the node's readings.
func (g *gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	g.readingsMu.Lock()
	defer g.readingsMu.Unlock()

	// no readings available yet
	if len(g.lastReadings) == 0 || g.lastReadings == nil {
		// Tell the collector not to capture the empty data.
		if extra[data.FromDMString] == true {
			return map[string]interface{}{}, data.ErrNoCaptureToStore
		}
		return noReadings, nil
	}

	return g.lastReadings, nil
}
