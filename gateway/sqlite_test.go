package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestSetupSqlite(t *testing.T) {

	// cmd := `create table if not exists devices(devEui TEXT NOT NULL PRIMARY KEY,` +
	// 		`appSKey TEXT,nwkSKey TEXT,devAddr TEXT,fCntDown INTEGER,nodeName TEXT,minUplinkInterval REAL);`
	// 	if _, err = db.ExecContext(ctx, cmd); err != nil {
	// 		return err
	// 	}
	t.Run("Good setup with no previous db", func(t *testing.T) {
		dataDirectory1 := t.TempDir()
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		err := g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		err = g.migrateDevicesFromJSONFile(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, g.db, test.ShouldNotBeNil)
	})

	t.Run("Good setup that migrates a txt file", func(t *testing.T) {
		g := gateway{
			logger: logging.NewTestLogger(t),
		}

		// Device found in file should return device info
		devices := []deviceInfoOld{
			{
				DevEUI:  fmt.Sprintf("%X", testDevEUI),
				DevAddr: fmt.Sprintf("%X", testDeviceAddr),
				AppSKey: fmt.Sprintf("%X", testAppSKey),
				NwkSKey: fmt.Sprintf("%X", testNwkSKey),
			},
		}

		expectedNewDevice, err := convertOldInfoToNew(devices[0])
		test.That(t, err, test.ShouldBeNil)
		test.That(t, devices[0].DevEUI, test.ShouldEqual, fmt.Sprintf("%X", expectedNewDevice.DevEUI))
		test.That(t, devices[0].DevAddr, test.ShouldEqual, fmt.Sprintf("%X", expectedNewDevice.DevAddr))
		test.That(t, devices[0].AppSKey, test.ShouldEqual, fmt.Sprintf("%X", expectedNewDevice.AppSKey))

		data, err := json.MarshalIndent(devices, "", "  ")
		test.That(t, err, test.ShouldBeNil)

		dataDirectory1 := t.TempDir()
		// check if the machine has an old devicedata file for us to migrate
		filePathTXT := filepath.Join(dataDirectory1, "devicedata.txt")
		dataFile, err := os.Create(filePathTXT)
		test.That(t, err, test.ShouldBeNil)

		_, err = dataFile.Write(data)
		test.That(t, err, test.ShouldBeNil)

		err = g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		err = g.migrateDevicesFromJSONFile(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 1)
		test.That(t, dbDevices[0], test.ShouldResemble, expectedNewDevice)
	})

	t.Run("Good setup that reuses a db", func(t *testing.T) {
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		dataDirectory1 := t.TempDir()
		err := g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		// Device found in file should return device info
		devices := []deviceInfo{
			{
				DevEUI:  testDevEUI,
				DevAddr: testDeviceAddr,
				AppSKey: testAppSKey,
			},
			{
				DevEUI:  []byte("DEVEUI2"),
				DevAddr: []byte("REALDEVADDR"),
				AppSKey: []byte("REAL-APPSKEY"),
			},
		}
		for _, device := range devices {
			g.insertOrUpdateDeviceInDB(context.Background(), device)
		}
		err = g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 2)
		test.That(t, dbDevices[0], test.ShouldResemble, devices[0])
		test.That(t, dbDevices[1], test.ShouldResemble, devices[1])
	})

	t.Run("bad setup that fails migration because of a bad json file", func(t *testing.T) {
		g := gateway{
			logger: logging.NewTestLogger(t),
		}

		dataDirectory1 := t.TempDir()
		// check if the machine has an old devicedata file for us to migrate
		filePathTXT := filepath.Join(dataDirectory1, "devicedata.txt")
		dataFile, err := os.Create(filePathTXT)
		test.That(t, err, test.ShouldBeNil)

		_, err = dataFile.WriteString("badData")
		test.That(t, err, test.ShouldBeNil)
		err = g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		err = g.migrateDevicesFromJSONFile(context.Background(), dataDirectory1)
		test.That(t, err.Error(), test.ShouldContainSubstring, errTXTMigration.Error())
	})
}

func TestSearchForDeviceInDB(t *testing.T) {
	g := createTestGateway(t)

	// Device found in file should return device info
	devices := []deviceInfo{
		{
			DevEUI:  testDevEUI,
			DevAddr: testDeviceAddr,
			AppSKey: testAppSKey,
		},
	}
	for _, device := range devices {
		test.That(t, g.insertOrUpdateDeviceInDB(context.Background(), device), test.ShouldBeNil)
	}

	t.Run("device is found", func(t *testing.T) {
		device, err := g.findDeviceInDB(context.Background(), testDevEUI)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, device, test.ShouldNotBeNil)
		test.That(t, device.DevAddr, test.ShouldResemble, testDeviceAddr)
	})
	t.Run("device is not found", func(t *testing.T) {
		_, err := g.findDeviceInDB(context.Background(), []byte("not-real"))
		test.That(t, err, test.ShouldBeError, errNoDeviceInDB)
	})
	t.Run("gateway is closed", func(t *testing.T) {
		g.Close(context.Background())
		_, err := g.findDeviceInDB(context.Background(), testDevEUI)
		test.That(t, err, test.ShouldBeError, errDBClosed)
	})
	t.Run("no db is present", func(t *testing.T) {
		badGateway := gateway{logger: logging.NewTestLogger(t)}
		_, err := badGateway.findDeviceInDB(context.Background(), testDevEUI)
		test.That(t, err, test.ShouldBeError, errNoDB)
	})
}

func TestInsertOrUpdateDeviceInDBAndgetAllDevicesFromDB(t *testing.T) {
	t.Run("test inserting devices and getting them", func(t *testing.T) {
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		dataDirectory1 := t.TempDir()
		err := g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, g.db, test.ShouldNotBeNil)
		dbDevices, err := g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 0)

		// Device found in file should return device info
		devices := []deviceInfo{
			{
				DevEUI:  testDevEUI,
				DevAddr: testDeviceAddr,
				AppSKey: testAppSKey,
			},
			{
				DevEUI:  []byte("DEVEUI2"),
				DevAddr: []byte("REALDEVADDR"),
				AppSKey: []byte("REAL-APPSKEY"),
			},
			{
				DevEUI:  []byte("DEVEUI3"),
				DevAddr: []byte("0000REAL00"),
				AppSKey: []byte("REAL-APPSKEY"),
			},
			{
				DevEUI:  []byte("DEVEUI2"),
				DevAddr: []byte("REPEAT"),
				AppSKey: []byte("REAL-APPSKEY"),
			},
		}
		for _, device := range devices {
			err = g.insertOrUpdateDeviceInDB(context.Background(), device)
			test.That(t, err, test.ShouldBeNil)
		}
		dbDevices, err = g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dbDevices), test.ShouldEqual, 3)
		test.That(t, dbDevices[0], test.ShouldResemble, devices[0])
		test.That(t, dbDevices[1], test.ShouldResemble, devices[2])
		test.That(t, dbDevices[2], test.ShouldResemble, devices[3])
	})
	t.Run("gateway is closed", func(t *testing.T) {
		g := gateway{
			logger: logging.NewTestLogger(t),
		}
		dataDirectory1 := t.TempDir()
		err := g.setupSqlite(context.Background(), dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, g.db, test.ShouldNotBeNil)
		err = g.Close(context.Background())
		test.That(t, err, test.ShouldBeNil)
		device := deviceInfo{
			DevEUI:  testDevEUI,
			DevAddr: testDeviceAddr,
			AppSKey: testAppSKey,
		}
		err = g.insertOrUpdateDeviceInDB(context.Background(), device)
		test.That(t, err, test.ShouldBeError, errDBClosed)
		_, err = g.getAllDevicesFromDB(context.Background())
		test.That(t, err, test.ShouldBeError, errDBClosed)
	})
}
