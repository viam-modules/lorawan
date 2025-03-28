// Package gateway implements the sx1302 gateway module.
package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	// github.com/mattn/go-sqlite3 is for sqlite.
	_ "github.com/mattn/go-sqlite3"
)

var (
	errTXTMigration = errors.New("error migrating device backup to sqlite. please reset your sensors")
	errNoDeviceInDB = errors.New("error device not found")
	errNoDB         = errors.New("error device file not found")
)

// Create or open a sqlite db file used to save device data across restarts.
func (g *gateway) setupSqlite(ctx context.Context) error {
	moduleDataDir := os.Getenv("VIAM_MODULE_DATA")

	var txtDevices []deviceInfo
	// check if the machine has an old devicedata file for us to migrate
	filePathTXT := filepath.Join(moduleDataDir, "devicedata.txt")
	if _, err := os.Stat(filePathTXT); err == nil {
		txtDevices, err = readFromFile(filePathTXT)
		if err != nil {
			return errTXTMigration
		}
	}

	filePathDB := filepath.Join(moduleDataDir, "devicedata.db")
	db, err := sql.Open("sqlite3", filePathDB)
	if err != nil {
		panic(err)
	}
	// create the table if it does not exist
	sqlStmt := `
	create table if not exists devices(devEui STRING NOT NULL PRIMARY KEY, appSKey STRING, nwkSKey STRING, devAddr STRING, fCntDown INTEGER);
	`
	if _, err = db.ExecContext(ctx, sqlStmt); err != nil {
		return err
	}
	g.db = db

	// move devices found from the old backup logic into the sqlite db
	for _, device := range txtDevices {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = g.insertOrUpdateDeviceInDB(ctx, device)
		if err != nil {
			return errTXTMigration
		}
	}

	// if we had a txt file, delete it now that we are finished migrating the data.
	if len(txtDevices) > 0 {
		if err := os.Remove(filePathTXT); err != nil {
			return errTXTMigration
		}
	}

	return nil
}

func (g *gateway) insertOrUpdateDeviceInDB(ctx context.Context, device deviceInfo) error {
	if g.db == nil {
		return errNoDB
	}
	_, err := g.db.ExecContext(ctx, "insert or replace into devices (devEui, appSKey, nwkSKey, devAddr, fCntDown) VALUES(?, ?, ?, ?, ?);",
		device.DevEUI,
		device.AppSKey,
		device.NwkSKey,
		device.DevAddr,
		device.FCntDown)
	return err
}

func (g *gateway) findDeviceInDB(ctx context.Context, devEui string) (*deviceInfo, error) {
	if g.db == nil {
		return nil, errNoDB
	}
	devEui = strings.ToUpper(devEui)
	newDevice := deviceInfo{}
	if err := g.db.QueryRowContext(ctx, "select * from devices where devEui = ?",
		devEui).Scan(&newDevice.DevEUI, &newDevice.AppSKey, &newDevice.NwkSKey, &newDevice.DevAddr, &newDevice.FCntDown); err != nil {
		if strings.Contains(err.Error(), "no rows in result set") {
			return &deviceInfo{}, errNoDeviceInDB
		}
		return &deviceInfo{}, err
	}
	return &newDevice, nil
}

func (g *gateway) getAllDevicesFromDB(ctx context.Context) ([]deviceInfo, error) {
	if g.db == nil {
		return nil, errNoDB
	}
	queryAll := `SELECT * FROM devices`
	rows, err := g.db.QueryContext(ctx, queryAll)
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	//nolint:errcheck
	defer rows.Close()

	devices := []deviceInfo{}
	for rows.Next() {
		if ctx.Err() != nil {
			return []deviceInfo{}, ctx.Err()
		}
		// var devEuiOut, appSKeyOut, nwkSKeyOut, devAddrOut string
		// var fCntDownOut int
		device := deviceInfo{}
		err = rows.Scan(&device.DevEUI, &device.AppSKey, &device.NwkSKey, &device.DevAddr, &device.FCntDown)
		if err != nil {
			return nil, err
		}
		// fmt.Print(device)
		devices = append(devices, device)
	}
	return devices, nil
}

// Function to read the device info from the persitent data file.
func readFromFile(filePath string) ([]deviceInfo, error) {
	filePath = filepath.Clean(filePath)
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck
	defer file.Close()
	// Reset file pointer to the beginning
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to the beginning of the file: %w", err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var devices []deviceInfo
	err = json.Unmarshal(data, &devices)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return devices, nil
}
