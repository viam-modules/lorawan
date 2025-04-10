package gatewaypoc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils"
)

// Model represents a lorawan gateway model.
var Model = node.LorawanFamily.WithModel(string("poc-gateway"))

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
	Bus       int    `json:"spi_bus,omitempty"`
	PowerPin  *int   `json:"power_en_pin,omitempty"`
	ResetPin  *int   `json:"reset_pin"`
	BoardName string `json:"board"`
	Region    string `json:"region_code,omitempty"`
}

// LoggingRoutineStarted is a global variable to track if the captureCOutputToLogs goroutine has
// started for each gateway. If the gateway build errors and needs to build again, we only want to start
// the logging routine once.
var loggingRoutineStarted = make(map[string]bool)

var noReadings = map[string]interface{}{"": "no readings available yet"}

func init() {
	resource.RegisterComponent(
		sensor.API,
		Model,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: NewGateway,
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

	lastReadings []lorahw.RxPacket // map of devices to readings
	readingsMu   sync.Mutex

	started bool
	rstPin  board.GPIOPin
	pwrPin  board.GPIOPin

	logReader *os.File
	logWriter *os.File

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

	if err := g.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}

	return g, nil
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

	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	board, err := board.FromDependencies(deps, cfg.BoardName)
	if err != nil {
		return err
	}

	// capture C log output
	g.startCLogging()

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

		rxPackets := []lorahw.RxPacket{}

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

			rxPackets = append(rxPackets, packet)
		}
		g.readingsMu.Lock()
		g.logger.Infof("adding packet to last readings list: length is %d", len(rxPackets))
		g.lastReadings = append(g.lastReadings, rxPackets...)
		g.readingsMu.Unlock()

	}
}

const (
	joinDelay     = 6  // rx2 delay in seconds for sending join accept message.
	downlinkDelay = 2  // rx2 delay in seconds for downlink messages.
	rx2SF         = 12 // spreading factor for rx2 window, used for both US and EU
	// command identifiers of supported mac commands.
	deviceTimeCID = 0x0D
	linkCheckCID  = 0x02
	dutyCycleCID  = 0x04
)

// According to lorawan docs, downlinks have a +/- 20 us error window, so regular sleep would not be accurate enough.
func accurateSleep(ctx context.Context, duration time.Duration) bool {
	// If we use utils.SelectContextOrWait(), we will wake up sometime after when we're supposed
	// to, which can be hundreds of microseconds later (because the process scheduler in the OS only
	// schedules things every millisecond or two). For use cases like a web server responding to a
	// query, that's fine. but when outputting a PWM signal, hundreds of microseconds can be a big
	// deal. To avoid this, we sleep for less time than we're supposed to, and then busy-wait until
	// the right time. Inspiration for this approach was taken from
	// https://blog.bearcats.nl/accurate-sleep-function/
	// On a raspberry pi 4, naively calling utils.SelectContextOrWait tended to have an error of
	// about 140-300 microseconds, while this version had an error of 0.3-0.6 microseconds.
	startTime := time.Now()
	maxBusyWaitTime := 1500 * time.Microsecond
	if duration > maxBusyWaitTime {
		shorterDuration := duration - maxBusyWaitTime
		if !utils.SelectContextOrWait(ctx, shorterDuration) {
			return false
		}
	}

	for time.Since(startTime) < duration {
		if err := ctx.Err(); err != nil {
			return false
		}
		// Otherwise, busy-wait some more
	}
	return true
}

func (g *gateway) sendDownlink(ctx context.Context, downlink DownlinkInfo) error {
	if len(downlink.Payload) > 256 {
		return fmt.Errorf("error sending downlink, payload size is %d bytes, max size is 256 bytes", len(downlink.Payload))
	}

	txPkt := &lorahw.TxPacket{
		Freq:      g.regionInfo.Rx2Freq,
		DataRate:  rx2SF,
		Bandwidth: g.regionInfo.Rx2Bandwidth,
		Size:      uint(len(downlink.Payload)),
		Payload:   downlink.Payload,
	}
	timeFormat := time.RFC3339Nano
	packetTime, err := time.Parse(timeFormat, downlink.Time)
	if err != nil {
		return fmt.Errorf("error converting time: %w", err)
	}

	// 47709/32*time.Microsecond is the internal delay of sending a packet
	waitDuration := (downlinkDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	if downlink.IsJoinAccept {
		waitDuration = (joinDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	}

	if !accurateSleep(ctx, waitDuration) {
		return fmt.Errorf("error sending downlink: %w", ctx.Err())
	}
	g.logger.Infof("sending packet to lora hardware on gateway %s", g.Name())
	err = lorahw.SendPacket(ctx, txPkt)
	if err != nil {
		return err
	}

	g.logger.Info("sent packet to lora hardware")

	return nil
}

type DownlinkInfo struct {
	Payload      []byte
	Time         string
	IsJoinAccept bool
}

func (g *gateway) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := cmd["get_packets"]; ok {
		g.readingsMu.Lock()
		defer g.readingsMu.Unlock()
		resp := map[string]interface{}{"get_packets": g.lastReadings}
		g.lastReadings = []lorahw.RxPacket{}
		return resp, nil
	}
	if msg, ok := cmd["send_downlink"]; ok {
		g.logger.Info("got a send downlink command")
		m, ok := msg.(map[string]interface{})
		if !ok {
			g.logger.Errorf("error getting message")
		}
		dl, err := convertToDownlinkInfo(m)
		if err != nil {
			g.logger.Errorf("error converting to downlink info %w", err)
		}

		g.logger.Infof("payload to send: %x", dl.Payload)
		err = g.sendDownlink(ctx, dl)
		if err != nil {
			g.logger.Errorf("error sending downlink: %w", err)
		}

	}
	return map[string]interface{}{}, nil

}

func convertToDownlinkInfo(info map[string]interface{}) (DownlinkInfo, error) {
	dlInfo := DownlinkInfo{}
	payload, err := convertToBytes(info["Payload"])
	if err != nil {
		return DownlinkInfo{}, err
	}
	dlInfo.Payload = payload
	dlInfo.IsJoinAccept = info["IsJoinAccept"].(bool)
	dlInfo.Time = info["Time"].(string)

	return dlInfo, nil
}

// convertToBytes converts the interface{} field from the docommand map into a byte array.
func convertToBytes(key interface{}) ([]byte, error) {
	bytes, ok := key.([]interface{})
	if !ok {
		return nil, fmt.Errorf("convertToBytes expected []interface{} but got %v", reflect.TypeOf(key))
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

	return nil
}

// Readings returns all the node's readings.
func (g *gateway) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return noReadings, nil
}
