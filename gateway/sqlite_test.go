package gateway

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestSetupSqlite(t *testing.T) {

	t.Run("Good setup with no previous db", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		t.Setenv("VIAM_MODULE_DATA", dataDirectory1)
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		err := g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, g.db, test.ShouldNotBeNil)
	})

	t.Run("Good setup that migrates a txt file", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		t.Setenv("VIAM_MODULE_DATA", dataDirectory1)
		g := gateway{
			logger: logging.NewTestLogger(t),
		}

		// Device found in file should return device info
		devices := []deviceInfo{
			{
				DevEUI:  fmt.Sprintf("%X", testDevEUI),
				DevAddr: fmt.Sprintf("%X", testDeviceAddr),
				AppSKey: "5572404C694E6B4C6F526132303138323",
			},
		}

		data, err := json.MarshalIndent(devices, "", "  ")
		test.That(t, err, test.ShouldBeNil)

		moduleDataDir := os.Getenv("VIAM_MODULE_DATA")
		// check if the machine has an old devicedata file for us to migrate
		filePathTXT := filepath.Join(moduleDataDir, "devicedata.txt")
		dataFile, err := os.Create(filePathTXT)
		test.That(t, err, test.ShouldBeNil)

		_, err = dataFile.Write(data)
		test.That(t, err, test.ShouldBeNil)

		err = g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 1)
		test.That(t, dbDevices[0], test.ShouldResemble, devices[0])

	})

	t.Run("Good setup that reuses a db", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		t.Setenv("VIAM_MODULE_DATA", dataDirectory1)
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		err := g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeNil)
		// Device found in file should return device info
		devices := []deviceInfo{
			{
				DevEUI:  fmt.Sprintf("%X", testDevEUI),
				DevAddr: fmt.Sprintf("%X", testDeviceAddr),
				AppSKey: "5572404C694E6B4C6F526132303138323",
			},
			{
				DevEUI:  "DEVEUI2",
				DevAddr: "REALDEVADDR",
				AppSKey: "REAL-APPSKEY",
			},
		}
		for _, device := range devices {
			g.insertOrUpdateDeviceInDB(context.Background(), device)
		}
		err = g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 2)
		test.That(t, dbDevices[0], test.ShouldResemble, devices[0])
		test.That(t, dbDevices[1], test.ShouldResemble, devices[1])

	})

	t.Run("bad setup that fails migration because of a bad json file", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		t.Setenv("VIAM_MODULE_DATA", dataDirectory1)
		g := gateway{
			logger: logging.NewTestLogger(t),
		}

		moduleDataDir := os.Getenv("VIAM_MODULE_DATA")
		// check if the machine has an old devicedata file for us to migrate
		filePathTXT := filepath.Join(moduleDataDir, "devicedata.txt")
		dataFile, err := os.Create(filePathTXT)
		test.That(t, err, test.ShouldBeNil)

		_, err = dataFile.WriteString("badData")
		test.That(t, err, test.ShouldBeNil)
		err = g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeError, errTXTMigration)
	})
}

func TestSearchForDeviceInFile(t *testing.T) {
	g := createTestGateway(t)

	// Device found in file should return device info
	devices := []deviceInfo{
		{
			DevEUI:  fmt.Sprintf("%X", testDevEUI),
			DevAddr: fmt.Sprintf("%X", testDeviceAddr),
			AppSKey: "5572404C694E6B4C6F526132303138323",
		},
	}
	for _, device := range devices {
		test.That(t, g.insertOrUpdateDeviceInDB(context.Background(), device), test.ShouldBeNil)
	}

	t.Run("device is found", func(t *testing.T) {
		device, err := g.findDeviceInDB(context.Background(), hex.EncodeToString(testDevEUI))
		test.That(t, err, test.ShouldBeNil)
		test.That(t, device, test.ShouldNotBeNil)
		test.That(t, device.DevAddr, test.ShouldEqual, fmt.Sprintf("%X", testDeviceAddr))
	})
	t.Run("device is not found", func(t *testing.T) {
		device, err := g.findDeviceInDB(context.Background(), "not-real")
		test.That(t, err, test.ShouldBeError, errNoDevice)
		test.That(t, device, test.ShouldBeNil)
	})
	t.Run("no db is present", func(t *testing.T) {
		badGateway := gateway{logger: logging.NewTestLogger(t)}
		device, err := badGateway.findDeviceInDB(context.Background(), "not-real")
		test.That(t, err, test.ShouldBeError, errNoDevice)
		test.That(t, device, test.ShouldBeNil)
	})
}

func insertOrUpdateDeviceInDB(t *testing.T) {
	t.Run("test inserting devices", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		t.Setenv("VIAM_MODULE_DATA", dataDirectory1)
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		err := g.setupSqlite(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, g.db, test.ShouldNotBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 1)

		// Device found in file should return device info
		devices := []deviceInfo{
			{
				DevEUI:  fmt.Sprintf("%X", testDevEUI),
				DevAddr: fmt.Sprintf("%X", testDeviceAddr),
				AppSKey: "5572404C694E6B4C6F526132303138323",
			},
			{
				DevEUI:  "DEVEUI2",
				DevAddr: "REALDEVADDR",
				AppSKey: "REAL-APPSKEY",
			},
		}
		for _, device := range devices {
			g.insertOrUpdateDeviceInDB(context.Background(), device)
		}
		dbDevices, err = g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 2)
		test.That(t, dbDevices[0], test.ShouldResemble, devices[0])
		test.That(t, dbDevices[1], test.ShouldResemble, devices[1])
	})
}
