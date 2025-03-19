// Package node implements the node model
package node

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// TestKey is a DoCommand key to skip sending commands downstream to the generic node and/or gateway.
const TestKey = "test_only"

// NewSensor creates a new Node struct. This can be used by external implementers.
func NewSensor(conf resource.Config, logger logging.Logger) Node {
	return Node{
		Named:    conf.ResourceName().AsNamed(),
		logger:   logger,
		NodeName: conf.ResourceName().AsNamed().Name().Name,
	}
}

// ReconfigureWithConfig runs the reconfigure logic of a Node using the native Node Config.
// For Specialized sensor implementations this function should be used within Reconfigure.
func (n *Node) ReconfigureWithConfig(ctx context.Context, deps resource.Dependencies, cfg *Config) error {
	switch cfg.JoinType {
	case "OTAA", "":
		appKey, err := hex.DecodeString(cfg.AppKey)
		if err != nil {
			return err
		}
		n.AppKey = appKey

		devEui, err := hex.DecodeString(cfg.DevEUI)
		if err != nil {
			return err
		}
		n.DevEui = devEui
	case "ABP":
		devAddr, err := hex.DecodeString(cfg.DevAddr)
		if err != nil {
			return err
		}

		n.Addr = devAddr

		appSKey, err := hex.DecodeString(cfg.AppSKey)
		if err != nil {
			return err
		}

		n.AppSKey = appSKey
	}

	n.DecoderPath = cfg.Decoder

	// if the decoder path is a url, save the file
	if isValidURL(n.DecoderPath) {
		decoderFilename := path.Base(n.DecoderPath)
		// Check if the extension is .js
		if !strings.HasSuffix(decoderFilename, ".js") {
			// Change extension to .js
			decoderFilename = strings.TrimSuffix(decoderFilename, path.Ext(decoderFilename)) + ".js"
		}

		httpClient := &http.Client{
			Timeout: time.Second * 25,
		}
		decoderFilePath, err := WriteDecoderFileFromURL(ctx, decoderFilename, cfg.Decoder, httpClient, n.logger)
		if err != nil {
			return err
		}
		n.DecoderPath = decoderFilePath
	} else if err := isValidFilePath(n.DecoderPath); err != nil {
		return fmt.Errorf("provided decoder file path is not valid: %w", err)
	}
	n.JoinType = cfg.JoinType

	if n.JoinType == "" {
		n.JoinType = "OTAA"
	}

	if cfg.FPort != "" {
		val, err := hex.DecodeString(cfg.FPort)
		if err != nil {
			return err
		}
		n.FPort = val[0]
	}

	gateway, err := getGateway(ctx, deps)
	if err != nil {
		return err
	}

	cmd := make(map[string]interface{})

	// send the device to the gateway.
	cmd["register_device"] = n

	_, err = gateway.DoCommand(ctx, cmd)
	if err != nil {
		return err
	}

	n.gateway = gateway
	return nil
}

// getCaptureFrequencyHzFromConfig extract the capture_frequency_hz from the device config.
func getCaptureFrequencyHzFromConfig(c resource.Config) (float64, error) {
	var captureFreqHz float64
	var captureMethodFound bool
	for _, assocResourceCfg := range c.AssociatedResourceConfigs {
		if captureMethodsMapInterface := assocResourceCfg.Attributes["capture_methods"]; captureMethodsMapInterface != nil {
			captureMethodFound = true
			for _, captureMethodsInterface := range captureMethodsMapInterface.([]interface{}) {
				captureMethods := captureMethodsInterface.(map[string]interface{})
				if captureMethods["method"].(string) == "Readings" {
					captureFreqHz = captureMethods["capture_frequency_hz"].(float64)
				}
			}
		}
	}
	if captureMethodFound && captureFreqHz <= 0 {
		return 0.0, errors.New("zero or negative capture frequency")
	}
	return captureFreqHz, nil
}

// CheckCaptureFrequency check the resource's capture frequency
// and reports to the user whether a safe value has been configured.
func CheckCaptureFrequency(c resource.Config, interval float64, logger logging.Logger) error {
	// Warn if user's configured capture frequency is more than the expected uplink interval.
	captureFreq, err := getCaptureFrequencyHzFromConfig(c)
	if err != nil {
		return err
	}

	intervalSeconds := (time.Duration(interval) * time.Minute).Seconds()
	expectedFreq := 1 / intervalSeconds

	if captureFreq > expectedFreq {
		logger.Warnf(
			`"configured capture frequency (%v) is greater than the frequency (%v)
			of expected uplink interval for node %v: lower capture frequency to avoid duplicate data"`,
			captureFreq,
			expectedFreq,
			c.ResourceName().AsNamed().Name().Name)
		return nil
	}
	return nil
}

// WriteDecoderFile writes an embedded decoderFile into the data folder of the module.
func WriteDecoderFile(decoderFilename string, decoderFile embed.FS) (string, error) {
	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")
	filePath := filepath.Join(moduleDataDir, decoderFilename)

	// check if the decoder was already written
	//nolint:gosec
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		// the decoder hasn't been written yet, so lets write it.
		// load the decoder from the binary
		decoder, err := decoderFile.ReadFile(decoderFilename)
		if err != nil {
			return "", err
		}
		file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			return "", err
		}
		_, err = file.Write(decoder)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		// an actual error happened, and the file may or may not exist.
		return "", err
	}
	return filePath, nil
}

// WriteDecoderFileFromURL writes a decoder file from a url into the data folder of the module.
func WriteDecoderFileFromURL(ctx context.Context, decoderFilename, url string,
	httpClient *http.Client, logger logging.Logger,
) (string, error) {
	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")
	filePath := filepath.Join(moduleDataDir, decoderFilename)

	// check if the decoder was already written
	//nolint:gosec
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		// the decoder hasn't been written yet, so lets write it.
		// create an http request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}

		res, err := httpClient.Do(req)
		if err != nil {
			return "", err
		}
		// check that the request was successful.
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf(ErrBadDecoderURL, res.StatusCode)
		}
		// get the decoder data.
		decoderData, err := io.ReadAll(res.Body)
		if err != nil {
			return "", err
		}
		//nolint:errcheck
		defer res.Body.Close()

		logger.Debugf("Writing decoder to file %s", filePath)
		//nolint:all
		err = os.WriteFile(filePath, decoderData, 0755)
		if err != nil {
			return "", err
		}

		return filePath, nil
	} else if err != nil {
		// an actual error happened, and the file may or may not exist.
		return "", err
	}

	return filePath, nil
}

func isValidURL(str string) bool {
	parsedURL, err := url.ParseRequestURI(str)
	if err != nil {
		return false
	}
	return parsedURL.Scheme != "" && parsedURL.Host != ""
}

func isValidFilePath(path string) error {
	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("error checking file: %w", err)
	}

	if info.IsDir() {
		return errors.New("path is a directory, not a file")
	}

	if filepath.Ext(path) != ".js" {
		return errors.New("decoder must be a .js file")
	}
	return nil
}

// CheckTestKey checks if a map has the testKey set.
func CheckTestKey(cmd map[string]interface{}) bool {
	_, ok := cmd[TestKey]
	return ok
}

// SendDownlink sends a downlink command to the gateway via the gateway's DoCommand.
func (n *Node) SendDownlink(ctx context.Context, payload string, testOnly bool) (map[string]interface{}, error) {
	req := map[string]interface{}{}
	downlinks := map[string]interface{}{}
	downlinks[n.NodeName] = payload
	// return the expected message if testOnly is set.
	if testOnly {
		req[DownlinkKey] = downlinks
		return req, nil
	}

	// lock to prevent sending downlinks to the gateway while reconfigure occurs.
	n.reconfigureMu.Lock()
	defer n.reconfigureMu.Unlock()

	req[GatewaySendDownlinkKey] = downlinks
	return n.gateway.DoCommand(ctx, req)
}

// IntervalRequest is the information needed to generate an interval downlink for a sensor.
type IntervalRequest struct {
	IntervalMin     float64
	PayloadUnits    Units
	UseLittleEndian bool
	Header          string
	NumBytes        int
	TestOnly        bool
}

// Units is an enum to specify units to be used by a downlink.
type Units int

const (
	unspecified Units = iota
	// Seconds is seconds.
	Seconds
	// Minutes is minutes.
	Minutes
	// IntervalKey is the key for an interval DoCommand.
	IntervalKey = "send_interval"
)

// SendIntervalDownlink formats a payload to send to the gateway using an IntervalRequest.
// The function does not support interval downlinks with more than 8 bytes.
func (n *Node) SendIntervalDownlink(ctx context.Context, req IntervalRequest) (map[string]interface{}, error) {
	var formattedInterval uint64
	if req.Header == "" {
		return nil, errors.New("cannot send interval downlink, downlink header is empty")
	}
	switch req.PayloadUnits {
	case Minutes:
		// round to nearest minute
		formattedInterval = uint64(math.Round(req.IntervalMin))
	case Seconds:
		// convert to the nearest second.
		formattedInterval = uint64(math.Round(req.IntervalMin * 60))
	case unspecified:
		return nil, errors.New("cannot send interval downlink, units unspecified")
	default:
		return nil, fmt.Errorf("cannot send interval downlink, unit %v unsupported", req.PayloadUnits)
	}

	if req.NumBytes > 8 || req.NumBytes < 1 {
		return nil, fmt.Errorf("cannot send interval downlink, NumBytes must be between 1 and 8, got %v", req.NumBytes)
	}
	if formattedInterval >= uint64(math.Pow(2, 8*float64(req.NumBytes))) {
		return nil, fmt.Errorf("cannot send interval downlink, interval of %v minutes exceeds maximum number of bytes %v",
			req.IntervalMin, req.NumBytes)
	}

	// we format the hex with uppercase, ensure the header is too.
	intervalString := strings.ToUpper(req.Header)
	if req.UseLittleEndian {
		// 8 is def not the max technically but sensors really shouldn't go that big
		bs := make([]byte, 8)
		binary.LittleEndian.PutUint64(bs, formattedInterval)

		// only loop for the number of bytes we actually need
		for i := range req.NumBytes {
			intervalString = fmt.Sprintf("%s%02X", intervalString, bs[i])
		}
	} else {
		// is not little endian
		intervalString += fmt.Sprintf(formatStringWithBytes(req.NumBytes), formattedInterval)
	}

	if req.TestOnly {
		return map[string]interface{}{IntervalKey: intervalString}, nil
	}
	return n.SendDownlink(ctx, intervalString, false)
}

func formatStringWithBytes(numBytes int) string {
	// 2 hex digits per byte
	return fmt.Sprintf("%%0%dX", 2*numBytes)
}
