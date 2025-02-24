package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// USBConfig provides the config for a USB gateway.
type USBConfig struct {
	Path string `json:"serial_path,omitempty"`
}

// ModelUSB represents a lorawan gateway model.
var ModelUSB = resource.NewModel("viam", "lorawan", "sx1302-usb")

// Validate ensures all parts of the config are valid.
func (conf *USBConfig) Validate(path string) ([]string, error) {
	return nil, nil
}

func init() {
	resource.RegisterComponent(
		sensor.API,
		ModelUSB,
		resource.Registration[sensor.Sensor, *USBConfig]{
			Constructor: NewUSBGateway,
		})
}

// NewUSBGateway creates a new USB gateway.
func NewUSBGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g, err := newUSBGateway(ctx, deps, conf, logger, false)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// NewUSBGateway creates a new gateway.
func newUSBGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
	test bool,
) (sensor.Sensor, error) {
	g := &gateway{
		Named:   conf.ResourceName().AsNamed(),
		logger:  logger,
		started: false,
		spi:     false,
	}

	if !test {
		file, err := getDataFile()
		if err != nil {
			return nil, err
		}
		g.dataFile = file
	}

	err := g.Reconfigure(ctx, deps, conf)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// finds the serial path of USB gateway.
func getSerialPath(path string) (string, error) {
	osType := runtime.GOOS
	if osType != "linux" {
		return "", errors.New("the lorawan gateway only supports linux OS")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("path %s does not exist. Ensure a serial device is connected.", path)
	}

	// Read directory contents
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	if len(entries) > 1 {
		return "", fmt.Errorf(
			"more than one serial device connected, unable to determine the serial path. Please provide serial_path in the config.")
	}

	entry := entries[0]
	fullPath := filepath.Join(path, entry.Name())
	target, err := os.Readlink(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to get serial path from symbolic link")
	}

	// Resolve relative path to absolute /dev path
	resolvedTarget := filepath.Join("/dev", filepath.Base(target))

	return resolvedTarget, nil
}
