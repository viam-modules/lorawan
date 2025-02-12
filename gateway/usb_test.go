package gateway

import (
	"context"
	"os"
	"runtime"
	"testing"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

func TestNewUSBGateway(t *testing.T) {
	conf := resource.Config{
		Name: "test-gateway",

		API:   sensor.API,
		Model: ModelUSB,
		ConvertedAttributes: &USBConfig{
			Path: "/path",
		},
	}

	deps := resource.Dependencies{}
	logger := logging.NewTestLogger(t)

	gw, err := newUSBGateway(context.Background(), deps, conf, logger, true)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, gw, test.ShouldNotBeNil)

	g := gw.(*gateway)
	test.That(t, g.spi, test.ShouldBeFalse)

	// Test that constructor tries to find serial path, if its not there error
	conf = resource.Config{
		Name: "test-gateway",

		API:                 sensor.API,
		Model:               ModelUSB,
		ConvertedAttributes: &USBConfig{},
	}

	gw, err = newUSBGateway(context.Background(), deps, conf, logger, true)
	test.That(t, err.Error(), test.ShouldContainSubstring, "does not exist")
	test.That(t, gw, test.ShouldBeNil)
}

func TestGetSerialPath(t *testing.T) {
	// Test non-Linux OS
	if runtime.GOOS != "linux" {
		path, err := getSerialPath("/dev/serial")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldEqual, "only linux is supported")
		test.That(t, path, test.ShouldBeEmpty)
		return
	}

	// Test path doesn't exist
	path, err := getSerialPath("/dev/serial")
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "does not exist")
	test.That(t, path, test.ShouldBeEmpty)

	tempDir, err := os.MkdirTemp("", "tmpdir")
	test.That(t, err, test.ShouldBeNil)

	defer os.RemoveAll(tempDir)

	// Create two device entries
	_, err = os.CreateTemp(tempDir, "device1")
	test.That(t, err, test.ShouldBeNil)
	_, err = os.CreateTemp(tempDir, "device2")
	test.That(t, err, test.ShouldBeNil)

	path, err = getSerialPath(tempDir)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "more than one serial device connected")
	test.That(t, path, test.ShouldBeEmpty)

	// Clean up and test single device case
	err = os.RemoveAll(tempDir)
	test.That(t, err, test.ShouldBeNil)

	deviceName := "pci-0000:00:14.0-usb-0:1:1.0"
	tempDir, err = os.MkdirTemp("", "tmpdir")
	test.That(t, err, test.ShouldBeNil)
	f, err := os.CreateTemp(tempDir, deviceName)
	test.That(t, err, test.ShouldBeNil)

	path, err = getSerialPath(tempDir)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, path, test.ShouldEqual, f.Name())
}
