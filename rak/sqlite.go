package rak

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
	goutils "go.viam.com/utils"
)

var (
	errTXTMigration = errors.New("error migrating device backup to sqlite. please reset your sensors")
	errNoDeviceInDB = errors.New("error device not found")
	errNoDB         = errors.New("error device file not found")
	// errDBClosedInternal is needed because this error is not exported by sql. this can potentially break in the far future.
	errDBClosedInternal = errors.New("sql: database is closed")
	errDBClosed         = errors.New("error gateway is closed")
)

// Create or open a sqlite db file used to save device data across restarts.
func (r *rak7391) setupSqlite(ctx context.Context, pathPrefix string) error {
	filePathDB := filepath.Join(pathPrefix, "devicedata.db")
	db, err := sql.Open("sqlite3", filePathDB)
	if err != nil {
		return err
	}
	// create the table if it does not exist
	// if we want to change the fields in the table, a migration function needs to be created
	cmd := `create table if not exists devices(devEui TEXT NOT NULL PRIMARY KEY,` +
		` appSKey TEXT, nwkSKey TEXT, devAddr TEXT, fCntDown INTEGER, nodeName TEXT, minUplinkInterval REAL);`
	if _, err = db.ExecContext(ctx, cmd); err != nil {
		return err
	}
	r.db = db

	return nil
}

func (r *rak7391) insertOrUpdateDeviceInDB(ctx context.Context, device deviceInfo) error {
	if r.db == nil {
		return errNoDB
	}

	cmd := `insert or replace into ` +
		`devices(devEui, appSKey, nwkSKey, devAddr, fCntDown, nodeName, minUplinkInterval) VALUES(?, ?, ?, ?, ?, ?, ?);`
	_, err := r.db.ExecContext(ctx, cmd,
		device.DevEUI,
		device.AppSKey,
		device.NwkSKey,
		device.DevAddr,
		device.FCntDown,
		device.NodeName,
		device.MinUplinkInterval,
	)
	if err != nil && err.Error() == errDBClosedInternal.Error() {
		return errDBClosed
	}
	return err
}

func (r *rak7391) findDeviceInDB(ctx context.Context, devEui string) (deviceInfo, error) {
	if r.db == nil {
		return deviceInfo{}, errNoDB
	}
	var zero deviceInfo
	devEui = strings.ToUpper(devEui)
	newDevice := deviceInfo{}
	if err := r.db.QueryRowContext(ctx, "select * from devices where devEui = ?;",
		devEui).Scan(&newDevice.DevEUI, &newDevice.AppSKey, &newDevice.NwkSKey,
		&newDevice.DevAddr, &newDevice.FCntDown, &newDevice.NodeName, &newDevice.MinUplinkInterval); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, errNoDeviceInDB
		}
		if err.Error() == errDBClosedInternal.Error() {
			return zero, errDBClosed
		}
		return zero, err
	}
	return newDevice, nil
}

func (r *rak7391) getAllDevicesFromDB(ctx context.Context) ([]deviceInfo, error) {
	if r.db == nil {
		return nil, errNoDB
	}
	queryAll := `SELECT * FROM devices;`
	rows, err := r.db.QueryContext(ctx, queryAll)
	if err != nil {
		if err.Error() == errDBClosedInternal.Error() {
			return nil, errDBClosed
		}
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	defer func() { goutils.UncheckedError(rows.Close()) }()

	devices := []deviceInfo{}
	for rows.Next() {
		if ctx.Err() != nil {
			return []deviceInfo{}, ctx.Err()
		}
		device := deviceInfo{}
		err = rows.Scan(&device.DevEUI,
			&device.AppSKey,
			&device.NwkSKey,
			&device.DevAddr,
			&device.FCntDown,
			&device.NodeName,
			&device.MinUplinkInterval)
		if err != nil {
			return nil, err
		}

		devices = append(devices, device)
	}
	// check for any errors just in case
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return devices, nil
}

// Function to migrate the device info from the persitent data file into a sqlite db.
func (r *rak7391) migrateDevicesFromJSONFile(ctx context.Context, pathPrefix string) error {
	// check if the machine has an old devicedata file for us to migrate
	filePathTXT := filepath.Join(pathPrefix, "devicedata.txt")
	filePath := filepath.Clean(filePathTXT)
	if _, err := os.Stat(filePathTXT); err == nil {
		file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return errors.Join(errTXTMigration, err)
		}

		defer func() { goutils.UncheckedError(file.Close()) }()
		// Reset file pointer to the beginning
		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
			return errors.Join(errTXTMigration,
				fmt.Errorf("failed to seek to the beginning of the file: %w", err))
		}

		data, err := io.ReadAll(file)
		if err != nil {
			return errors.Join(errTXTMigration,
				fmt.Errorf("failed to read file: %w", err))
		}
		// if we have no devices in the file, just return
		if len(data) == 0 {
			return nil
		}

		var devices []deviceInfo
		err = json.Unmarshal(data, &devices)
		if err != nil {
			return errors.Join(errTXTMigration,
				fmt.Errorf("failed to unmarshal JSON: %w", err))
		}

		// move devices found from the old backup logic into the sqlite db
		for _, device := range devices {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err = r.insertOrUpdateDeviceInDB(ctx, device); err != nil {
				return errTXTMigration
			}
		}
		if err := os.Remove(filePathTXT); err != nil {
			return errTXTMigration
		}
	}
	return nil
}
