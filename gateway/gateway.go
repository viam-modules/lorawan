// Package gateway implements the sx1302 gateway module.
package gateway

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/viam-modules/lorawan/lorahw"
	"github.com/viam-modules/lorawan/node"
	"github.com/viam-modules/lorawan/regions"
	v1 "go.viam.com/api/common/v1"
	pb "go.viam.com/api/component/sensor/v1"
	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
	"go.viam.com/utils/pexec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// defining model names here to be reused in getNativeConfig.
const (
	oldModelName  = "sx1302-gateway"
	genericHat    = "sx1302-hat-generic"
	waveshareHat  = "sx1302-waveshare-hat"
	rak7391       = "rak7391"
	SendPacketKey = "send_packet"
	GetPacketsKey = "get_packets"
	// the stop Docommand key tells the managed process to call stop on the gateway hardware.
	StopKey = "stop"
)

// Error variables for validation and operations.
var (
	// Config validation errors.
	errInvalidSpiBus = errors.New("spi bus can be 0 or 1 - default 0")
	errSPIAndUSB     = errors.New("cannot have both spi_bus and path attributes, add path for USB or spi_bus for SPI gateways")

	// Gateway operation errors.
	errUnexpectedJoinType         = errors.New("unexpected join type when adding node to gateway")
	errInvalidNodeMapType         = errors.New("expected node map val to be type []interface{}, but it wasn't")
	errInvalidByteType            = errors.New("expected node byte array val to be float64, but it wasn't")
	errNoDevice                   = errors.New("received packet from unknown device")
	errInvalidMIC                 = errors.New("invalid MIC")
	errInvalidRegion              = errors.New("unrecognized region code, valid options are US915 and EU868")
	errInvalidConcentratorsLength = errors.New("invalid concentrator length - should not happen")
	errTimedOut                   = errors.New("timed out waiting for gateway to start")
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

// ModelRak7391 represents a lorawan RAK7381 model.
var ModelRak7391 = node.LorawanFamily.WithModel(string(rak7391))

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

type comType int

// enum for com types.
const (
	spi comType = iota
	usb
)

var noReadings = map[string]interface{}{"": "no readings available yet"}

// ConcentratorConfig describes the configuration for a single concentrator.
type ConcentratorConfig struct {
	Name        string
	Bus         *int   `json:"spi_bus,omitempty"`
	Path        string `json:"serial_path,omitempty"`
	ResetPin    *int
	PowerPin    *int
	BaseChannel int
}

// ConfigMultiConcentrator describes a config for a gateway with multiple concentrators.
type ConfigMultiConcentrator struct {
	Concentrators []*ConcentratorConfig
	Region        string
	BoardName     string
}

// Config describes the configuration of the gateway.
type Config struct {
	Bus       *int   `json:"spi_bus,omitempty"`
	PowerPin  *int   `json:"power_en_pin,omitempty"`
	ResetPin  *int   `json:"reset_pin"`
	BoardName string `json:"board"`
	Region    string `json:"region_code,omitempty"`
	Path      string `json:"path,omitempty"`
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

type concentrator struct {
	name       string
	client     pb.SensorServiceClient
	rstPin     board.GPIOPin
	pwrPin     board.GPIOPin
	started    bool
	conn       *grpc.ClientConn
	process    pexec.ManagedProcess
	logReader  *bufio.Reader
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	workers    *utils.StoppableWorkers
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
	resource.RegisterComponent(
		sensor.API,
		ModelRak7391,
		resource.Registration[sensor.Sensor, *ConfigRak7391]{
			Constructor: newRak7391,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	var deps []string
	if conf.ResetPin == nil {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "reset_pin")
	}
	if conf.Bus != nil && *conf.Bus != 0 && *conf.Bus != 1 {
		return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
	}
	if conf.Bus != nil && conf.Path != "" {
		return nil, resource.NewConfigValidationError(path, errSPIAndUSB)
	}

	if conf.Path != "" {
		err := validateSerialPath(conf.Path)
		if err != nil {
			return nil, err
		}
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

func validateSerialPath(path string) error {
	_, err := os.Stat(path)
	// serial path does not exist
	if errors.Is(err, os.ErrNotExist) {
		return resource.NewConfigValidationError(path, fmt.Errorf("configured path %s does not exist", path))
	}
	// other issue with serial path
	if err != nil {
		return resource.NewConfigValidationError(path, fmt.Errorf("error getting serial path %s: %w", path, err))
	}
	return nil
}

// Gateway defines a lorawan gateway.
type gateway struct {
	resource.Named
	logger logging.Logger
	mu     sync.Mutex

	workers *utils.StoppableWorkers

	lastReadings map[string]interface{} // map of devices to readings
	readingsMu   sync.Mutex

	devices map[string]*node.Node // map of node name to node struct

	db         *sql.DB // store device information/keys for use across restarts in a database
	regionInfo regions.RegionInfo
	region     regions.Region

	concentrators []*concentrator

	cgoPath string
}

// NewGateway creates a new gateway.
func NewGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g := &gateway{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
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

func getMultiConcentratorConfig(conf resource.Config) (*ConfigMultiConcentrator, error) {
	switch conf.Model.Name {
	case rak7391:
		rak7391Config, err := resource.NativeConfig[*ConfigRak7391](conf)
		if err != nil {
			return nil, fmt.Errorf("the config %v does not match a supported config type", conf)
		}
		return rak7391Config.getGatewayConfig(), nil
	default:
		return nil, errors.New("build error in module. Unsupported Gateway model")
	}
}

// Reconfigure reconfigures the gateway.
func (g *gateway) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.resetConcentrators(ctx); err != nil {
		return err
	}
	// workers and process are restarted during reconfigure
	g.closeProcesses()

	// maintain devices and lastReadings through reconfigure.
	if g.devices == nil {
		g.devices = make(map[string]*node.Node)
	}

	if g.lastReadings == nil {
		g.lastReadings = make(map[string]interface{})
	}

	switch conf.Model {
	case ModelRak7391:
		cfg, err := getMultiConcentratorConfig(conf)
		if err != nil {
			return err
		}
		if err := g.reconfigureMultiConcentrator(ctx, deps, cfg); err != nil {
			return err
		}
	default:
		cfg, err := getNativeConfig(conf)
		if err != nil {
			return err
		}
		if err := g.reconfigureSingleConcentrator(ctx, deps, cfg); err != nil {
			return err
		}
	}

	g.workers = utils.NewBackgroundStoppableWorkers(g.receivePackets)

	return nil
}

func (g *gateway) reconfigureSingleConcentrator(ctx context.Context, deps resource.Dependencies, cfg *Config) error {
	// reset concentrators list
	g.concentrators = make([]*concentrator, 0)

	board, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
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

	concentratorConfig := &ConcentratorConfig{
		PowerPin:    cfg.PowerPin,
		ResetPin:    cfg.ResetPin,
		Bus:         cfg.Bus,
		Path:        cfg.Path,
		BaseChannel: 0,
	}

	err = g.createConcentrator(ctx, g.Name().Name, board, concentratorConfig)
	if err != nil {
		return err
	}
	return nil
}

func (g *gateway) reconfigureMultiConcentrator(ctx context.Context, deps resource.Dependencies, cfg *ConfigMultiConcentrator) error {
	// get the number of concentrators before reconfiguring
	oldNumConcentrators := len(g.concentrators)

	// reset concentrators list
	g.concentrators = make([]*concentrator, 0)

	board, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
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

	for _, c := range cfg.Concentrators {
		if err := g.createConcentrator(ctx, c.Name, board, c); err != nil {
			return err
		}
	}

	// if the number of concentrators has changed, send a new linkADRReq downlink to each device to update frequency channels.
	if g.region == regions.US && oldNumConcentrators != 0 && oldNumConcentrators != len(g.concentrators) {
		chMask, err := g.getChannelMask()
		if err != nil {
			return err
		}
		for _, n := range g.devices {
			fopts := createLinkADRReq(chMask)
			n.FoptsToSend = append(n.FoptsToSend, fopts)
		}
	}
	return nil
}

// |    4 bits  |    4 bits  |  2 B   | 1 B
// | data rate  |  TX power  | CHmask | redundancy.
func createLinkADRReq(chMask []byte) []byte {
	payload := make([]byte, 0)
	payload = append(payload, linkADRCID)
	payload = append(payload, 0x20)      // DR2 / TX power default
	payload = append(payload, chMask[0]) // enable/disable 0-7
	payload = append(payload, chMask[1]) // enable/disable 8-15

	// bits 7–4: ChMaskCtrl - / chmask is for block0 (channels 0-15)
	// bits 3–0: NbTrans - number of transmissions for each uplink message: 1
	payload = append(payload, 0x01)

	return payload
}

func (g *gateway) createConcentrator(ctx context.Context,
	id string,
	b board.Board,
	cfg *ConcentratorConfig,
) error {
	// adding io before the pin allows you to use the GPIO number.
	rstPin, err := b.GPIOPinByName("io" + strconv.Itoa(*cfg.ResetPin))
	if err != nil {
		return err
	}

	c := concentrator{rstPin: rstPin, name: id}

	// not every gateway has a power enable pin so it is optional.
	if cfg.PowerPin != nil {
		pwrPin, err := b.GPIOPinByName("io" + strconv.Itoa(*cfg.PowerPin))
		if err != nil {
			return err
		}
		c.pwrPin = pwrPin
	}

	// defaults if nothing configured
	path := "/dev/spidev0.0"
	comType := spi

	if cfg.Bus != nil {
		path = fmt.Sprintf("/dev/spidev0.%d", *cfg.Bus)
		comType = spi
	}

	if cfg.Path != "" {
		// Resolve the symlink
		if strings.Contains(cfg.Path, "by-path") || strings.Contains(cfg.Path, "by-id") {
			resolvedPath, err := filepath.EvalSymlinks(cfg.Path)
			if err != nil {
				return fmt.Errorf("failed to resolve symlink of path %s: %w", cfg.Path, err)
			}
			cfg.Path = resolvedPath
		}
		path = cfg.Path
		comType = usb
	}

	var comTypeString string
	switch comType {
	case spi:
		comTypeString = "SPI"

	case usb:
		comTypeString = "USB"
	}

	g.logger.Infof("starting %s concentrator connected to path %s", comTypeString, path)

	if err = resetGateway(ctx, rstPin, c.pwrPin); err != nil {
		return fmt.Errorf("error initializing the gateway: %w", err)
	}

	args := []string{
		fmt.Sprintf("--comType=%d", comType),
		"--path=" + path,
		fmt.Sprintf("--region=%d", g.region),
		fmt.Sprintf("--baseChannel=%d", cfg.BaseChannel),
	}

	var cgoexe string
	cgoexe = g.cgoPath
	if g.cgoPath == "" {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("error getting current directory: %w", err)
		}

		cgoexe = dir + "/cgo"
		_, err = os.Stat(cgoexe)
		if err != nil {
			return fmt.Errorf("error getting the cgo exe: %w", err)
		}
	}

	logReader, logWriter := io.Pipe()
	bufferedLogReader := bufio.NewReader(logReader)
	c.pipeWriter = logWriter
	c.pipeReader = logReader
	c.logReader = bufferedLogReader

	config := pexec.ProcessConfig{
		ID:        id,
		Name:      cgoexe,
		Args:      args,
		Log:       false,
		LogWriter: logWriter,
	}

	process := pexec.NewManagedProcess(config, g.logger)
	c.process = process

	if err := process.Start(ctx); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	port, err := c.watchLogs(ctx, g.logger)
	if err != nil {
		return err
	}

	g.logger.Debugf("connecting to port: %s\n", port)
	target := "localhost:" + port
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("error connecting to server: %w", err)
	}

	g.logger.Debugf("connected to %s client", id)

	c.conn = conn
	c.client = pb.NewSensorServiceClient(conn)
	c.started = true

	g.concentrators = append(g.concentrators, &c)
	return nil
}

// watchLogs watches for startup logs and signals when the managed process has started.
// After startup it continues filtering the logs from the managed process.
// watchLogs watches for startup logs and signals when the managed process has started.
// After startup it continues filtering the logs from the managed process.
func (c *concentrator) watchLogs(ctx context.Context, logger logging.Logger) (string, error) {
	cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var port string

	// Spawn goroutine to close reader when ctx times out
	// Need to do this because ReadString blocks until
	// there is a new line or underlying reader is closed
	go func() {
		<-cancelCtx.Done()
		if errors.Is(cancelCtx.Err(), context.DeadlineExceeded) {
			if err := c.pipeReader.Close(); err != nil {
				logger.Warnf("error closing reader: %w", err)
			}
		}
	}()

	for {
		select {
		case <-cancelCtx.Done():
			return "", errTimedOut
		default:
		}
		line, err := c.logReader.ReadString('\n')
		if err != nil {
			logger.Warnf("error reading log line: %s", err.Error())
		}
		filterLog(line, logger)

		if strings.Contains(line, "Server successfully started:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				port = strings.TrimSpace(parts[1])
				logger.Debugf("Captured port: %s", port)
			}
		}

		if strings.Contains(line, "done setting up gateway") && port != "" {
			logger.Debug("successfully initialized gateway")
			// Keep reading logs in the background.
			c.workers = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) {
				c.readLogs(ctx, logger)
			})
			return port, nil
		}
	}
}

func (c *concentrator) readLogs(ctx context.Context, logger logging.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := c.logReader.ReadString('\n')
		if err != nil {
			logger.Warnf("error reading log line: %s", err.Error())
		}
		filterLog(line, logger)
	}
}

// captureOutput is a background routine to capture C's stdout and log to the module's logger.
// This is necessary because the sx1302 library only uses printf to report errors.
func filterLog(line string, logger logging.Logger) {
	switch {
	case strings.Contains(line, "STANDBY_RC mode"):
		// The error setting standby_rc mode indicates a hardware initiaization failure
		// add extra log to instruct user what to do.
		logger.Error(line)
		logger.Error("gateway hardware is misconfigured: unplug the board and wait a few minutes before trying again")
	case strings.Contains(line, "ERROR"):
		logger.Error(line)
	case strings.Contains(line, "WARNING"):
		logger.Warn(line)
	default:
		logger.Debug(line)
	}
}

func (g *gateway) receivePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cmdStruct, err := structpb.NewStruct(map[string]interface{}{
			GetPacketsKey: true,
		})
		if err != nil {
			g.logger.Errorf("Failed to create command struct: %v", err)
		}

		req := &v1.DoCommandRequest{
			Command: cmdStruct,
		}

		for _, c := range g.concentrators {
			select {
			case <-ctx.Done():
				return
			default:
			}
			resp, err := c.client.DoCommand(ctx, req)
			if err != nil {
				g.logger.Errorf("error calling do command on client for concentrator %s: %v", c.name, err)
			}
			if resp != nil {
				data := resp.GetResult().AsMap()

				rawPackets, ok := data["packets"]
				if !ok {
					g.logger.Errorf("no packets found in map")
				}

				// Marshal back to JSON to decode into typed struct
				rawJSON, err := json.Marshal(rawPackets)
				if err != nil {
					g.logger.Errorf("failed to get raw json")
				}

				var packets []lorahw.RxPacket
				err = json.Unmarshal(rawJSON, &packets)
				if err != nil {
					g.logger.Errorf("failed to unmarshal into packet")
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

					g.handlePacket(ctx, packet, t, c)
				}
			}
		}
	}
}

func (g *gateway) handlePacket(ctx context.Context, packet lorahw.RxPacket, packetTime time.Time, c *concentrator) {
	// first byte is MHDR - specifies message type
	payload := packet.Payload
	switch payload[0] {
	case joinRequestMHdr:
		g.logger.Debugf("received join request")
		if err := g.handleJoin(ctx, payload, packetTime, c); err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			g.logger.Errorf("couldn't handle join request: %v", err)
		}
	case unconfirmedUplinkMHdr:
		name, readings, err := g.parseDataUplink(ctx, packet, packetTime, c)
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
		name, readings, err := g.parseDataUplink(ctx, packet, packetTime, c)
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

	// Add the node to gateway's list of devices.
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

			// if the device has already joined the network, we should send a linkADRReq
			// to ensure the device uses the right frequency channels.
			// we know the device has already joined or is ABP mode if dev addr is populated.
			n := g.devices[node.NodeName]
			if len(n.Addr) != 0 && g.region == regions.US {
				chMask, err := g.getChannelMask()
				if err != nil {
					return nil, fmt.Errorf("error getting gateway's channel mask: %w", err)
				}
				fopts := createLinkADRReq(chMask)
				n.FoptsToSend = append(n.FoptsToSend, fopts)
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

	// Send a downlink
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

	// Return info about one device
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

func (g *gateway) getChannelMask() ([]byte, error) {
	var chMask []byte
	switch len(g.concentrators) {
	case 1:
		chMask = []byte{0xFF, 0x00}
	case 2:
		chMask = []byte{0xFF, 0xFF}
	default:
		return nil, errInvalidConcentratorsLength
	}
	return chMask, nil
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

// Readings returns all the node's readings.
func (g *gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	g.readingsMu.Lock()
	defer g.readingsMu.Unlock()

	// no readings available yet
	if len(g.lastReadings) == 0 || g.lastReadings == nil {
		// Tell the collector not to capture the empty data.
		if extra != nil && extra[data.FromDMString] == true {
			return map[string]interface{}{}, data.ErrNoCaptureToStore
		}
		return noReadings, nil
	}

	return g.lastReadings, nil
}

// If the concentrator was already started, stop gateway and the background worker.
// Make sure to always call stop on the concentrator before making any changes to the c config or
// errors will occur.
// Unexpected behavior will also occur if stop is called when the concentrator hasn't been
// started, so only call stop if c.started is true.
func (g *gateway) resetConcentrators(ctx context.Context) error {
	// close the routine that receives lora packets - otherwise this will error when the gateway is stopped.
	if g.workers != nil {
		g.workers.Stop()
		g.workers = nil
	}

	// create stop command
	cmdStruct, err := structpb.NewStruct(map[string]interface{}{
		StopKey: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create command struct: %w", err)
	}

	req := &v1.DoCommandRequest{
		Command: cmdStruct,
	}

	for _, c := range g.concentrators {
		if c.started {
			// stop the concentrator
			_, err = c.client.DoCommand(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to stop concentrator: %w", err)
			}
			// reset using GPIO pin
			if c.rstPin != nil {
				if err = resetGateway(ctx, c.rstPin, nil); err != nil {
					return fmt.Errorf("error initializing the gateway: %w", err)
				}
			}
			c.started = false
		}
	}
	return nil
}

func (g *gateway) closeProcesses() {
	for _, c := range g.concentrators {
		if err := c.Close(); err != nil {
			g.logger.Errorf("error closing concentrator %s: %w", c.name, err)
		}
	}
}

func (c *concentrator) Close() error {
	// Close pipe files before workers to avoid a hang on shutdown.
	if c.pipeReader != nil {
		if err := c.pipeReader.Close(); err != nil {
			return fmt.Errorf("error closing log reader: %s", err.Error())
		}
	}
	if c.pipeWriter != nil {
		if err := c.pipeWriter.Close(); err != nil {
			return fmt.Errorf("error closing log writer: %s", err.Error())
		}
	}
	if c.workers != nil {
		c.workers.Stop()
	}

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("error closing client conn: %w", err)
		}
	}

	if c.process != nil {
		if err := c.process.Stop(); err != nil {
			return fmt.Errorf("error stopping process: %s", err.Error())
		}
	}
	c.started = false
	return nil
}

// Close closes the gateway.
func (g *gateway) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.workers != nil {
		g.workers.Stop()
	}

	g.closeProcesses()

	if g.db != nil {
		if err := g.db.Close(); err != nil {
			g.logger.Errorf("error closing data db: %w", err)
		}
	}

	return nil
}
