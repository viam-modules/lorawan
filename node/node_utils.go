// Package node implements the node model
package node

import (
	"context"
	"embed"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

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

	n.DecoderPath = cfg.DecoderPath
	n.JoinType = cfg.JoinType

	if n.JoinType == "" {
		n.JoinType = "OTAA"
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
func CheckCaptureFrequency(c resource.Config, interval float64, logger logging.Logger) (bool, error) {
	// Warn if user's configured capture frequency is more than the expected uplink interval.
	captureFreq, err := getCaptureFrequencyHzFromConfig(c)
	if err != nil {
		return false, err
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
		return false, nil
	}
	return true, nil
}

// WriteDecoderFile writes an embeded decoderFile into the data folder of the module.
func WriteDecoderFile(decoderFilename string, decoderFile embed.FS) (string, error) {
	// Create or open the file used to save device data across restarts.
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
