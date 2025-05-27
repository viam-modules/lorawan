// Package rak7391 provides a model for the rak7391
package rak7391

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
	"strings"
	"sync"
	"time"

	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
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

const (
	rak7391ResetPin1 = "io17" // Default reset pin for first concentrator
	rak7391ResetPin2 = "io6"  // Default reset pin for second concentrator
)

type comType int

// enum for com types.
const (
	spi comType = iota
	usb
)

// ConcentratorConfig describes the configuration for a single concentrator.
type ConcentratorConfig struct {
	Bus  *int   `json:"spi_bus,omitempty"`
	Path string `json:"serial_path,omitempty"`
}

var noReadings = map[string]interface{}{"": "no readings available yet"}

// Error variables for validation and operations.
var (
	// Config validation errors.
	errConcentrators = errors.New("must configure at least one pcie concentrator")
	errInvalidSpiBus = errors.New("spi bus can be 0 or 1 - default 0")

	// Gateway operation errors.
	errUnexpectedJoinType = errors.New("unexpected join type when adding node to gateway")
	errInvalidNodeMapType = errors.New("expected node map val to be type []interface{}, but it wasn't")
	errInvalidByteType    = errors.New("expected node byte array val to be float64, but it wasn't")
	errNoDevice           = errors.New("received packet from unknown device")
	errInvalidMIC         = errors.New("invalid MIC")
	errInvalidRegion      = errors.New("unrecognized region code, valid options are US915 and EU868")
	errTimedOut           = errors.New("timed out waiting for gateway to start")
)

// Config describes the configuration of the rak gateway.
type Config struct {
	Concentrator1 *ConcentratorConfig `json:"pcie1,omitempty"`
	Concentrator2 *ConcentratorConfig `json:"pcie2,omitempty"`
	BoardName     string              `json:"board"`
	Region        string              `json:"region_code,omitempty"`
}

// constants for MHDRs of different message types.
const (
	joinRequestMHdr         = 0x00
	joinAcceptMHdr          = 0x20
	unconfirmedUplinkMHdr   = 0x40
	unconfirmedDownLinkMHdr = 0x60
	confirmedUplinkMHdr     = 0x80
)

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

type rak7391 struct {
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
	cgoPath       string // path to managed process exe to be used in tests.
}

type concentrator struct {
	client     pb.SensorServiceClient
	rstPin     board.GPIOPin
	started    bool
	conn       *grpc.ClientConn
	process    pexec.ManagedProcess
	logReader  *bufio.Reader
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	workers    *utils.StoppableWorkers
}

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string("rak7391"))

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newrak7391,
		})
}

// Validate ensures all parts of the config are valid.
func (conf *Config) Validate(path string) ([]string, error) {
	if conf.Concentrator1 == nil && conf.Concentrator2 == nil {
		return nil, resource.NewConfigValidationError(path, errConcentrators)
	}

	if conf.BoardName == "" {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}

	if conf.Concentrator1 != nil {
		if conf.Concentrator1.Bus != nil && *conf.Concentrator1.Bus != 0 && *conf.Concentrator1.Bus != 1 {
			return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
		}
	}

	if conf.Concentrator2 != nil {
		if conf.Concentrator2.Bus != nil && *conf.Concentrator2.Bus != 0 && *conf.Concentrator2.Bus != 1 {
			return nil, resource.NewConfigValidationError(path, errInvalidSpiBus)
		}
	}

	if conf.Region != "" {
		if regions.GetRegion(conf.Region) == regions.Unspecified {
			return nil, resource.NewConfigValidationError(path, errInvalidRegion)
		}
	}

	return []string{conf.BoardName}, nil
}

func newrak7391(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	r := &rak7391{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
	}

	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")

	if err := r.setupSqlite(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if err := r.migrateDevicesFromJSONFile(ctx, moduleDataDir); err != nil {
		return nil, err
	}

	if err := r.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}

	return r, nil
}

// Reconfigure reconfigures the rak.
func (r *rak7391) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	// If the concentrators were already started, stop gateway and the background worker.
	// Make sure to always call stopGateway() before making any changes to the c config or
	// errors will occur.
	// Unexpected behavior will also occur if stopGateway() is called when the gateway hasn't been
	// started, so only call stopGateway if this module already started the gateway.
	err = r.resetConcentrators(ctx)
	if err != nil {
		return err
	}

	r.closeProcesses()

	// maintain devices and lastReadings through reconfigure.
	if r.devices == nil {
		r.devices = make(map[string]*node.Node)
	}

	if r.lastReadings == nil {
		r.lastReadings = make(map[string]interface{})
	}

	r.concentrators = make([]*concentrator, 0)

	region := regions.GetRegion(cfg.Region)

	switch region {
	case regions.US, regions.Unspecified:
		r.logger.Infof("configuring gateway for US915 band")
		r.regionInfo = regions.RegionInfoUS
		r.region = regions.US
	case regions.EU:
		r.logger.Infof("configuring gateway for EU868 band")
		r.regionInfo = regions.RegionInfoEU
		r.region = regions.EU
	}

	b, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
	}

	if cfg.Concentrator1 != nil {
		if err := r.createConcentrator(ctx, "rak1", rak7391ResetPin1, b, cfg.Concentrator1); err != nil {
			return err
		}
		r.logger.Debugf("created concentrator 1")
	}

	if cfg.Concentrator2 != nil {
		if err := r.createConcentrator(ctx, "rak2", rak7391ResetPin2, b, cfg.Concentrator2); err != nil {
			return err
		}
		r.logger.Debugf("created concentrator 2")
	}
	r.workers = utils.NewBackgroundStoppableWorkers(r.receivePackets)
	return nil
}

func (r *rak7391) createConcentrator(ctx context.Context,
	id, restPinName string,
	b board.Board,
	cfg *ConcentratorConfig,
) error {
	rstPin, err := b.GPIOPinByName(restPinName)
	if err != nil {
		return err
	}
	c := concentrator{rstPin: rstPin}

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

	err = resetGateway(ctx, rstPin, nil)
	if err != nil {
		return fmt.Errorf("error initializing the gateway: %w", err)
	}

	args := []string{
		fmt.Sprintf("--comType=%d", comType),
		"--path=" + path,
		fmt.Sprintf("--region=%d", r.region),
	}

	var cgoexe string
	cgoexe = r.cgoPath
	if r.cgoPath == "" {
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

	process := pexec.NewManagedProcess(config, r.logger)
	c.process = process

	if err := process.Start(ctx); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	port, err := c.watchLogs(ctx, r.logger)
	if err != nil {
		return err
	}

	r.logger.Debugf("connecting to port: %s\n", port)
	target := "localhost:" + port
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("error connecting to server: %w", err)
	}

	r.logger.Info("connected to client")

	c.conn = conn
	c.client = pb.NewSensorServiceClient(conn)
	c.started = true
	r.concentrators = append(r.concentrators, &c)
	return nil
}

// Docommand provides extra functionality.
func (r *rak7391) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Validate that the dependency is correct, returns the gateway's region.
	if _, ok := cmd["validate"]; ok {
		return map[string]interface{}{"validate": r.region}, nil
	}

	// Add the nodes to the list of devices.
	if newNode, ok := cmd["register_device"]; ok {
		if newN, ok := newNode.(map[string]interface{}); ok {
			node, err := convertToNode(newN)
			if err != nil {
				return nil, err
			}

			oldNode, exists := r.devices[node.NodeName]
			switch exists {
			case false:
				r.devices[node.NodeName] = node
			case true:
				// node with that name already exists, merge them
				mergedNode, err := mergeNodes(node, oldNode)
				if err != nil {
					return nil, err
				}
				r.devices[node.NodeName] = mergedNode
			}
			// Check if the device is in the persistent data file, if it is add the OTAA info.
			deviceInfo, err := r.findDeviceInDB(ctx, hex.EncodeToString(r.devices[node.NodeName].DevEui))
			if err != nil {
				if !errors.Is(err, errNoDeviceInDB) {
					return nil, fmt.Errorf("error while searching for device in file: %w", err)
				}
			}
			// device was found in the file, update the gateway's device map with the device info.
			err = r.updateDeviceInfo(r.devices[node.NodeName], &deviceInfo)
			if err != nil {
				return nil, fmt.Errorf("error while updating device info: %w", err)
			}
		}
	}
	// Remove a node from the device map and readings map.
	if name, ok := cmd["remove_device"]; ok {
		if n, ok := name.(string); ok {
			delete(r.devices, n)
			r.readingsMu.Lock()
			delete(r.lastReadings, n)
			r.readingsMu.Unlock()
		}
	}

	// return all devices that have been registered on the gateway
	if _, ok := cmd["return_devices"]; ok {
		resp := map[string]interface{}{}
		// Read the device info from the file
		devices, err := r.getAllDevicesFromDB(ctx)
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
		device, err := r.findDeviceInDB(ctx, deveui)
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

	if devEUI, ok := cmd[node.GetDeviceKey]; ok {
		resp := map[string]interface{}{}
		deveui, ok := (devEUI).(string)
		if !ok {
			return nil, fmt.Errorf("expected a string but got %v", reflect.TypeOf(devEUI))
		}
		// Read the device info from the db
		device, err := r.findDeviceInDB(ctx, deveui)
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

	if payload, ok := cmd[node.GatewaySendDownlinkKey]; ok {
		downlinks, ok := payload.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected a map[string]interface{} but got %v", reflect.TypeOf(payload))
		}

		for name, payload := range downlinks {
			dev, ok := r.devices[name]
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

	// Remove a node from the device map and readings map.
	if _, ok := cmd["test"]; ok {
		cmdStruct, err := structpb.NewStruct(map[string]interface{}{
			gateway.GetPacketsKey: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create command struct: %w", err)
		}

		req := &v1.DoCommandRequest{
			Command: cmdStruct,
		}

		_, err = r.concentrators[0].client.DoCommand(ctx, req)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{}, err
	}
	return map[string]interface{}{}, nil
}

// updateDeviceInfo adds the device info from the db into the gateway's device map.
func (r *rak7391) updateDeviceInfo(device *node.Node, d *deviceInfo) error {
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
	r.devices[device.NodeName] = device
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

// Close closes the rak.
func (r *rak7391) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers != nil {
		r.workers.Stop()
	}

	if r.db != nil {
		if err := r.db.Close(); err != nil {
			r.logger.Errorf("error closing data db: %w", err)
		}
	}

	r.closeProcesses()

	return nil
}

func (r *rak7391) closeProcesses() {
	for _, c := range r.concentrators {
		// Close pipe files before workers to avoid a hang on shutdown.
		if c.pipeReader != nil {
			if err := c.pipeReader.Close(); err != nil {
				r.logger.Errorf("error closing log reader: %s", err)
			}
		}
		if c.pipeWriter != nil {
			if err := c.pipeWriter.Close(); err != nil {
				r.logger.Errorf("error closing log writer: %s", err)
			}
		}
		if c.workers != nil {
			c.workers.Stop()
		}

		if c.conn != nil {
			if err := c.conn.Close(); err != nil {
				r.logger.Errorf("error closing client conn: %w", err)
			}
		}

		if c.process != nil {
			if err := c.process.Stop(); err != nil {
				r.logger.Errorf("error stopping process: %s", err.Error())
			}
		}
		c.started = false
	}
}

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

func filterLog(line string, logger logging.Logger) {
	switch {
	case strings.Contains(line, "STANDBY_RC mode"):
		// The error setting standby_rc mode indicates a hardware initiaization failure
		// add extra log to instruct user what to do
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

func (r *rak7391) resetConcentrators(ctx context.Context) error {
	// close the routine that receives lora packets - otherwise this will error when the gateway is stopped.
	if r.workers != nil {
		r.workers.Stop()
		r.workers = nil
	}

	// create stop command
	cmdStruct, err := structpb.NewStruct(map[string]interface{}{
		gateway.StopKey: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create command struct: %w", err)
	}

	req := &v1.DoCommandRequest{
		Command: cmdStruct,
	}

	for _, c := range r.concentrators {
		if c.started {
			// stop the concentrator
			_, err = c.client.DoCommand(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to stop concentrator: %w", err)
			}
			// reset using GPIO pin
			if c.rstPin != nil {
				err := resetGateway(ctx, c.rstPin, nil)
				if err != nil {
					return fmt.Errorf("failed to reset concentrator: %w", err)
				}
			}
			c.started = false
		}
	}
	return nil
}

func (r *rak7391) receivePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cmdStruct, err := structpb.NewStruct(map[string]interface{}{
			gateway.GetPacketsKey: true,
		})
		if err != nil {
			r.logger.Errorf("Failed to create command struct: %v", err)
		}

		req := &v1.DoCommandRequest{
			Command: cmdStruct,
		}

		for i, c := range r.concentrators {
			select {
			case <-ctx.Done():
				return
			default:
			}
			resp, err := c.client.DoCommand(ctx, req)
			if err != nil {
				r.logger.Errorf("error calling do command on client for concentrator %d: %v", i+1, err)
			}
			if resp != nil {
				data := resp.GetResult().AsMap()

				rawPackets, ok := data["packets"]
				if !ok {
					r.logger.Errorf("no packets found in map")
				}

				// Marshal back to JSON to decode into typed struct
				rawJSON, err := json.Marshal(rawPackets)
				if err != nil {
					r.logger.Errorf("failed to get raw json")
				}

				var packets []lorahw.RxPacket
				err = json.Unmarshal(rawJSON, &packets)
				if err != nil {
					r.logger.Errorf("failed to unmarshal into packet")
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
							r.logger.Debugf("skipped duplicate packet")
							isDuplicate = true
							break
						}
					}
					if isDuplicate {
						continue
					}
					minSNR := sfToSNRMin[packet.DataRate]
					if float64(packet.SNR) < minSNR {
						r.logger.Warnf("packet skipped due to low signal noise ratio: %v, min is %v", packet.SNR, minSNR)
						continue
					}

					r.handlePacket(ctx, packet.Payload, t, packet.SNR, packet.DataRate, c)
				}
			}
		}
	}
}

func (r *rak7391) handlePacket(ctx context.Context, payload []byte, packetTime time.Time, snr float64, sf int, c *concentrator) {
	// r *first byte is MHDR - specifies message type
	switch payload[0] {
	case joinRequestMHdr:
		r.logger.Debugf("received join request")
		if err := r.handleJoin(ctx, payload, packetTime, c); err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			r.logger.Errorf("couldn't handle join request: %v", err)
		}
	case unconfirmedUplinkMHdr:
		name, readings, err := r.parseDataUplink(ctx, payload, packetTime, snr, sf, c)
		if err != nil {
			// don't log as error if it was a request from unknown device or duplicate packet.
			if errors.Is(errNoDevice, err) || errors.Is(errAlreadyParsed, err) {
				return
			}
			r.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		r.updateReadings(name, readings)
	case confirmedUplinkMHdr:
		name, readings, err := r.parseDataUplink(ctx, payload, packetTime, snr, sf, c)
		if err != nil {
			// don't log as error if it was a request from unknown device.
			if errors.Is(errNoDevice, err) {
				return
			}
			r.logger.Errorf("error parsing uplink message: %v", err)
			return
		}
		r.updateReadings(name, readings)
	default:
		r.logger.Warnf("received unsupported packet type with mhdr %x", payload[0])
	}
}

func (r *rak7391) updateReadings(name string, newReadings map[string]interface{}) {
	r.readingsMu.Lock()
	defer r.readingsMu.Unlock()
	readings, ok := r.lastReadings[name].(map[string]interface{})
	if !ok {
		// readings for this device does not exist yet
		r.lastReadings[name] = newReadings
		return
	}

	if readings == nil {
		r.lastReadings[name] = make(map[string]interface{})
	}
	for key, val := range newReadings {
		readings[key] = val
	}

	r.lastReadings[name] = readings
}

// Readings returns all the node's readings.
func (r *rak7391) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	r.readingsMu.Lock()
	defer r.readingsMu.Unlock()

	// no readings available yet
	if len(r.lastReadings) == 0 || r.lastReadings == nil {
		// Tell the collector not to capture the empty data.
		if extra[data.FromDMString] == true {
			return map[string]interface{}{}, data.ErrNoCaptureToStore
		}
		return noReadings, nil
	}

	return r.lastReadings, nil
}
