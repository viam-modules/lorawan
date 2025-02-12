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

type USBConfig struct {
	Path string `json:"serial_path,omitempty"`
}

// Model represents a lorawan gateway model.
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
			Constructor: newUSBGateway,
		})
}

// NewUSBGateway creates a new gateway
func newUSBGateway(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (sensor.Sensor, error) {
	g, err := NewUSBGateway(ctx, deps, conf, logger, false)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// NewUSBGateway creates a new gateway
func NewUSBGateway(
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

// called in gateway reconigure to find the serial path of USB gateway.
func getSerialPath(path string) (string, error) {
	osType := runtime.GOOS
	if osType != "linux" {
		return "", errors.New("only linux is supported")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("path %s does not exist. Ensure a serial device is connected.", path)
	}

	// Read directory contents
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("Failed to read directory %s: %v", path, err)
	}

	if len(entries) > 1 {
		return "", fmt.Errorf("more than one serial device connected - unable to determine serial path. Please provide serial_path in the config.")
	}
	return filepath.Join(path, entries[0].Name()), nil
}
