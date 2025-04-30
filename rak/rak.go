package rak

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	v1 "go.viam.com/api/common/v1"
	pb "go.viam.com/api/component/sensor/v1"

	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/node"
	"github.com/viam-modules/gateway/regions"
	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	rak7391ResetPin1 = 17 // Default reset pin for first concentrator
	rak7391ResetPin2 = 6  // Default reset pin for second concentrator
)

// ConnectionType defines the type of connection for the RAK gateway
type ConnectionType string

const (
	SPIConnection ConnectionType = "spi"
	USBConnection ConnectionType = "usb"
)

// ConcentratorConfig describes the configuration for a single concentrator
type ConcentratorConfig struct {
	Bus  *int   `json:"spi_bus,omitempty"`
	Path string `json:"path,omitempty"`
}

var noReadings = map[string]interface{}{"": "no readings available yet"}

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

// ConfigRak describes the configuration of the RAK gateway
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
	defaultExecutableName   = "cgo"
	tarGzPath               = "/home/rak/lorawan.tar.gz"
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

type RAK7391 struct {
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

	logReader  *os.File
	logWriter  *os.File
	db         *sql.DB // store device information/keys for use across restarts in a database
	regionInfo regions.RegionInfo
	region     regions.Region

	conn       *grpc.ClientConn
	servCancel context.CancelFunc

	concentrators []*concentrator
}

type concentrator struct {
	client  pb.SensorServiceClient
	rstPin  board.GPIOPin
	started bool
}

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string("rak"))

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newRAK,
		})
}

// getGatewayConfigs returns configurations for enabled concentrators
func (conf *Config) getGatewayConfigs() []*gateway.Config {
	var configs []*gateway.Config

	if conf.Concentrator1 != nil {
		resetPin1 := rak7391ResetPin1
		configs = append(configs, &gateway.Config{
			Path:      conf.Concentrator1.Path,
			BoardName: conf.BoardName,
			PowerPin:  nil,
			ResetPin:  &resetPin1,
			Region:    conf.Region,
		})
	}

	if conf.Concentrator2 != nil {
		resetPin2 := rak7391ResetPin2
		configs = append(configs, &gateway.Config{
			Path:      conf.Concentrator2.Path,
			BoardName: conf.BoardName,
			PowerPin:  nil,
			ResetPin:  &resetPin2,
			Region:    conf.Region,
		})
	}

	return configs
}

// validateConcentrator validates a single concentrator configuration
func validateConcentrator(c ConcentratorConfig, path string, num int) error {
	// if c.ConnectionType != "usb" && c.ConnectionType != "spi" {
	// 	return resource.NewConfigValidationError(path, fmt.Errorf("pcie%d connection type must be usb or spi", num))
	// }
	// if c.Path == "" {
	// 	return resource.NewConfigValidationFieldRequiredError(path, fmt.Sprintf("pcie%d.path", num))
	// }
	return nil
}

// Validate ensures all parts of the config are valid
func (conf *Config) Validate(path string) ([]string, error) {
	if conf.Concentrator1 == nil && conf.Concentrator2 == nil {
		return nil, resource.NewConfigValidationError(path, fmt.Errorf("must configure at least one pcie concentrator"))
	}

	if conf.BoardName == "" {
		return nil, resource.NewConfigValidationFieldRequiredError(path, "board")
	}

	// Validate Concentrator1 if configured
	if conf.Concentrator1 != nil {
		if err := validateConcentrator(*conf.Concentrator1, path, 1); err != nil {
			return nil, err
		}
	}

	// Validate Concentrator2 if configured
	if conf.Concentrator2 != nil {
		if err := validateConcentrator(*conf.Concentrator2, path, 2); err != nil {
			return nil, err
		}
	}

	return []string{conf.BoardName}, nil
}

func newRAK(ctx context.Context, deps resource.Dependencies,
	conf resource.Config, logger logging.Logger,
) (sensor.Sensor, error) {
	r := &RAK7391{
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

func (r *RAK7391) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	// If the gateway hardware was already started, stop gateway and the background worker.
	// Make sure to always call stopGateway() before making any changes to the c config or
	// errors will occur.
	// Unexpected behavior will also occur if stopGateway() is called when the gateway hasn't been
	// started, so only call stopGateway if this module already started the gateway.
	if r.started {
		r.reset(ctx)
	}

	// disconnect the client and stop server at each reconfigure
	if r.conn != nil {
		r.conn.Close()
	}

	if r.servCancel != nil {
		r.servCancel()
	}

	board, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
	}

	// // capture C log output
	// r.startCLogging()

	// maintain devices and lastReadings through reconfigure.
	if r.devices == nil {
		r.devices = make(map[string]*node.Node)
	}

	if r.lastReadings == nil {
		r.lastReadings = make(map[string]interface{})
	}

	if r.concentrators == nil {
		r.concentrators = make([]*concentrator, 0)
	}

	if cfg.Concentrator1 != nil {
		rstPin, err := board.GPIOPinByName("io17")
		if err != nil {
			return err
		}

		fmt.Println("here rst pin")
		fmt.Println(rstPin)

		c1 := &concentrator{rstPin: rstPin}
		r.concentrators = append(r.concentrators, c1)

		err = resetGateway(ctx, rstPin, nil)
		if err != nil {
			return fmt.Errorf("error initializing the gateway: %w", err)
		}

		var extractedDir = "/tmp/shared_binary"
		var extractedBinaryName = "cgo"
		var extractedBinaryPath = filepath.Join(extractedDir, extractedBinaryName)

		// only extract module.tar.gz once
		if _, err := os.Stat(extractedBinaryPath); err == nil {
		} else {
			if err := extractTarGz(tarGzPath, extractedDir); err != nil {
				return fmt.Errorf("failed to extract archive: %v", err)
			}
		}

		// Create isolated temp dir for this run
		extractDir, err := os.MkdirTemp("", fmt.Sprintf("lorawan_server_%s", r.Name().ShortName()))
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}

		// Copy binary into temp dir
		dstPath := filepath.Join(extractDir, extractedBinaryName)
		input, err := os.ReadFile(extractedBinaryPath)
		if err != nil {
			return fmt.Errorf("read shared binary failed: %w", err)
		}
		if err := os.WriteFile(dstPath, input, 0755); err != nil {
			return fmt.Errorf("write temp binary failed: %w", err)
		}

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

		var path string
		var comType int

		if cfg.Concentrator1.Bus != nil {
			path = fmt.Sprintf("/dev/spidev0.%d", *cfg.Concentrator1.Bus)
			comType = 0
		}

		if cfg.Concentrator1.Path != "" {
			path = cfg.Concentrator1.Path
			comType = 1
		}

		args := []string{
			fmt.Sprintf("--comType=%d", comType),
			fmt.Sprintf("--path=%s", path),
			fmt.Sprintf("--region=%d", region),
		}

		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, dstPath, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		r.servCancel = cancel

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start binary: %w", err)
		}

		var port string
		go func() {
			scanner := bufio.NewScanner(stdout)
			var port string
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Println("Server output:", line)

				if strings.Contains(line, "Server successfully started:") {
					parts := strings.Split(line, ":")
					if len(parts) == 2 {
						port = strings.TrimSpace(parts[1])
						fmt.Println("Captured port:", port)
						break
					}

				}
			}
			if err := scanner.Err(); err != nil {
				log.Println("Error reading stdout:", err)
			}
		}()

		conn, err := grpc.NewClient(fmt.Sprintf("localhost:%s", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("error connecting to server")
		}

		r.conn = conn
		r.concentrators[0].client = pb.NewSensorServiceClient(conn)

		r.concentrators[0].started = true
		r.receivingWorker = utils.NewBackgroundStoppableWorkers(r.receivePackets)
	}
	return nil

	// 	// adding io before the pin allows you to use the GPIO number.
	// 	rstPin, err := board.GPIOPinByName("io17")
	// 	if err != nil {
	// 		return err
	// 	}
	// 	r.rstPin = rstPin

	// 	err = resetGateway(ctx, r.rstPin, nil)
	// 	if err != nil {
	// 		return fmt.Errorf("error initializing the gateway: %w", err)
	// 	}

	// 	var extractedDir = "/tmp/shared_binary"
	// 	var extractedBinaryName = "cgo"
	// 	var extractedBinaryPath = filepath.Join(extractedDir, extractedBinaryName)

	// 	// only extract module.tar.gz once
	// 	if _, err := os.Stat(extractedBinaryPath); err == nil {
	// 	} else {
	// 		if err := extractTarGz(tarGzPath, extractedDir); err != nil {
	// 			return fmt.Errorf("failed to extract archive: %v", err)
	// 		}
	// 	}

	// 	// Create isolated temp dir for this run
	// 	extractDir, err := os.MkdirTemp("", fmt.Sprintf("lorawan_server_%s", r.Name().ShortName()))
	// 	if err != nil {
	// 		return fmt.Errorf("failed to create temp dir: %w", err)
	// 	}

	// 	// Copy binary into temp dir
	// 	dstPath := filepath.Join(extractDir, extractedBinaryName)
	// 	input, err := os.ReadFile(extractedBinaryPath)
	// 	if err != nil {
	// 		return fmt.Errorf("read shared binary failed: %w", err)
	// 	}
	// 	if err := os.WriteFile(dstPath, input, 0755); err != nil {
	// 		return fmt.Errorf("write temp binary failed: %w", err)
	// 	}

	// 	var path string
	// 	var comType comType

	// 	if cfg.Bus != nil {
	// 		path = fmt.Sprintf("/dev/spidev0.%d", *cfg.Bus)
	// 		comType = spi
	// 	}

	// 	if cfg.Path != "" {
	// 		path = cfg.Path
	// 		if strings.Contains(path, "spi") {
	// 			comType = spi
	// 		} else {
	// 			comType = usb
	// 		}
	// 	}

	// 	args := []string{
	// 		fmt.Sprintf("--comType=%d", comType),
	// 		fmt.Sprintf("--path=%s", path),
	// 		fmt.Sprintf("--region=%d", region),
	// 	}

	// 	ctx, cancel := context.WithCancel(context.Background())
	// 	cmd := exec.CommandContext(ctx, dstPath, args...)
	// 	stdout, err := cmd.StdoutPipe()
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	cmd.Stderr = os.Stderr
	// 	r.servCancel = cancel

	// 	if err := cmd.Start(); err != nil {
	// 		return fmt.Errorf("failed to start binary: %w", err)
	// 	}

	// 	portChan := make(chan string)

	// 	go func() {
	// 		scanner := bufio.NewScanner(stdout)
	// 		var port string
	// 		for scanner.Scan() {
	// 			line := scanner.Text()
	// 			fmt.Println("Server output:", line)

	// 			if strings.Contains(line, "Server successfully started:") {
	// 				parts := strings.Split(line, ":")
	// 				if len(parts) == 2 {
	// 					port = strings.TrimSpace(parts[1])
	// 					fmt.Println("Captured port:", port)
	// 					portChan <- port
	// 				}

	// 			}
	// 		}
	// 		if err := scanner.Err(); err != nil {
	// 			log.Println("Error reading stdout:", err)
	// 		}
	// 	}()

	// 	var port string
	// 	select {
	// 	case port = <-portChan:
	// 		g.logger.Infof("Received port from server: %s", port)
	// 	case <-time.After(10 * time.Second):
	// 		return fmt.Errorf("timeout waiting for server to start")
	// 	}

	// 	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%s", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	// 	if err != nil {
	// 		return fmt.Errorf("error connecting to server")
	// 	}

	// 	g.conn = conn
	// 	g.concentratorClient = pb.NewSensorServiceClient(conn)

	// 	g.started = true
	// 	g.receivingWorker = utils.NewBackgroundStoppableWorkers(g.receivePackets)
	// 	return nil
	// }

	// func (r *RAK7391) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	// 	readings1, err1 := r.gw1.Readings(ctx, extra)
	// 	readings2, err2 := r.gw2.Readings(ctx, extra)

	// 	// Combine readings from both gateways
	// 	combined := make(map[string]interface{})
	// 	if err1 == nil {
	// 		for k, v := range readings1 {
	// 			combined["gw1"+k] = v
	// 		}
	// 	}
	// 	if err2 == nil {
	// 		for k, v := range readings2 {
	// 			combined["gw2"+k] = v
	// 		}
	// 	}

	// 	if err1 != nil && err2 != nil {
	// 		return combined, fmt.Errorf("both gateways failed: %v, %v", err1, err2)
	// 	}

	// return combined, nil
}

func (r *RAK7391) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
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
	return map[string]interface{}{}, nil
}

// updateDeviceInfo adds the device info from the db into the gateway's device map.
func (r *RAK7391) updateDeviceInfo(device *node.Node, d *deviceInfo) error {
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

func (r *RAK7391) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reset(ctx)

	if r.loggingWorker != nil {
		if r.logReader != nil {
			if err := r.logReader.Close(); err != nil {
				r.logger.Errorf("error closing log reader: %s", err)
			}
		}
		if r.logWriter != nil {
			if err := r.logWriter.Close(); err != nil {
				r.logger.Errorf("error closing log writer: %s", err)
			}
		}
		r.loggingWorker.Stop()
		//delete(loggingRoutineStarted, r.Name().Name)
	}

	if r.db != nil {
		if err := r.db.Close(); err != nil {
			r.logger.Errorf("error closing data db: %s", err)
		}
	}

	if r.conn != nil {
		r.conn.Close()
	}
	if r.servCancel != nil {
		r.servCancel()
	}

	return nil
}

func (r *RAK7391) reset(ctx context.Context) {
	// close the routine that receives lora packets - otherwise this will error when the gateway is stopped.
	if r.receivingWorker != nil {
		r.receivingWorker.Stop()
	}

	for _, c := range r.concentrators {
		if err := lorahw.StopGateway(); err != nil {
			r.logger.Error("error stopping gateway: %v", err)
		}
		if c.rstPin != nil {
			err := resetGateway(ctx, c.rstPin, nil)
			if err != nil {
				r.logger.Error("error resetting the gateway")
			}
		}
		c.started = false
	}
}

func (r *RAK7391) receivePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cmdStruct, err := structpb.NewStruct(map[string]interface{}{
			"get_packets": true,
		})
		if err != nil {
			log.Fatalf("Failed to create command struct: %v", err)
		}

		req := &v1.DoCommandRequest{
			Command: cmdStruct,
		}

		resp, err := r.concentrators[0].client.DoCommand(ctx, req)
		if err != nil {
			log.Fatalf("DoCommand error: %v", err)
		}

		data := resp.Result.AsMap()

		rawPackets, ok := data["packets"]
		if !ok {
			r.logger.Errorf("no packets found in map")
		}

		//Marshal back to JSON to decode into typed struct
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

			r.handlePacket(ctx, packet.Payload, t, packet.SNR, packet.DataRate)
		}
	}
}

func (r *RAK7391) handlePacket(ctx context.Context, payload []byte, packetTime time.Time, snr float64, sf int) {
	// //r *first byte is MHDR - specifies message type
	// switch payload[0] {
	// case joinRequestMHdr:
	// 	g.logger.Debugf("received join request")
	// 	if err := g.handleJoin(ctx, payload, packetTime); err != nil {
	// 		// don't log as error if it was a request from unknown device.
	// 		if errors.Is(errNoDevice, err) {
	// 			return
	// 		}
	// 		g.logger.Errorf("couldn't handle join request: %v", err)
	// 	}
	// case unconfirmedUplinkMHdr:
	// 	name, readings, err := r.parseDataUplink(ctx, payload, packetTime, snr, sf)
	// 	if err != nil {
	// 		// don't log as error if it was a request from unknown device.
	// 		if errors.Is(errNoDevice, err) {
	// 			return
	// 		}
	// 		g.logger.Errorf("error parsing uplink message: %v", err)
	// 		return
	// 	}
	// 	g.updateReadings(name, readings)
	// case confirmedUplinkMHdr:
	// 	name, readings, err := r.parseDataUplink(ctx, payload, packetTime, snr, sf)
	// 	if err != nil {
	// 		// don't log as error if it was a request from unknown device.
	// 		if errors.Is(errNoDevice, err) {
	// 			return
	// 		}
	// 		g.logger.Errorf("error parsing uplink message: %v", err)
	// 		return
	// 	}
	// 	g.updateReadings(name, readings)

	// default:
	// 	g.logger.Warnf("received unsupported packet type with mhdr %x", payload[0])
	// }
}

func extractTarGz(gzipPath string, dest string) error {
	fmt.Println("IN EXTRACT TAR GZ")
	f, err := os.Open(gzipPath)
	if err != nil {
		fmt.Println("eror here opening")
		fmt.Println(gzipPath)
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Println("eror here 0")
		return err
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("eror here 1")
			return err
		}

		targetPath := filepath.Join(dest, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				fmt.Println("eror here 2")
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				fmt.Println("eror here 2.5 (making file's parent dirs)")
				return err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				fmt.Println("eror here 3")
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			// Make sure it's executable
			if err := os.Chmod(targetPath, 0755); err != nil {
				return err
			}
		}
	}
	return nil
}

// Readings returns all the node's readings.
func (r *RAK7391) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
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
