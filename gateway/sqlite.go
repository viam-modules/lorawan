// Package gateway implements the sx1302 gateway module.
package gateway

import (
	"context"
	"database/sql"
	"encoding/hex"
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
	errDBClosedInternal    = errors.New("sql: database is closed")
	errDBClosed            = errors.New("error gateway is closed")
	errNoTableInternal     = "no such table"
	errDupeColNameInternal = "duplicate column name"
)

// Node defines a lorawan node device.
type gatewayNode struct {
	AppSKey []byte
	NwkSKey []byte
	AppKey  []byte

	Addr              []byte
	DevEui            []byte
	NodeName          string
	MinUplinkInterval float64

	DecoderPath string
	JoinType    string

	FCntDown  uint16
	FPort     byte     // 11 for downlinks, only required when frame payload exists.
	Downlinks [][]byte // 12? list of downlink frame payloads to send
}

const (
	devEUIDBKey            = "devEui"
	appSDBKey              = "appSKey"
	nwkSDBKey              = "nwkSKey"
	devAddrDBKey           = "devAddr"
	fCntDownDBKey          = "fCntDown"
	nodeNameDBKey          = "nodeName"
	minUplinkIntervalDBKey = "minUplinkInterval"
	appKeyDBKey            = "appKey"
	decoderPathDBKey       = "decoderPath"
	joinTypeDBKey          = "joinType"
	fPortDBKey             = "fPort"
	isRegisteredDBKey      = "isRegistered"
)

// supportedCols defines the types of each column we currently support.
// is additionally used to set defaults for the table.
var supportedCols = [][]string{
	{devEUIDBKey, "BLOB NOT NULL PRIMARY KEY"},
	{appSDBKey, "BLOB"},
	{nwkSDBKey, "BLOB"},
	{devAddrDBKey, "BLOB"},
	{fCntDownDBKey, "INTEGER"},
	{nodeNameDBKey, "TEXT"},
	{minUplinkIntervalDBKey, "REAL"},
	{appKeyDBKey, "BLOB"},
	{decoderPathDBKey, "TEXT"},
	{joinTypeDBKey, "TEXT"},
	{fPortDBKey, "INTEGER"},
	{isRegisteredDBKey, "INTEGER DEFAULT 0"}, // bools do not exist
}

// Create or open a sqlite db file used to save device data across restarts.
func (g *gateway) setupSqlite(ctx context.Context, pathPrefix string) error {
	filePathDB := filepath.Join(pathPrefix, "devicedata.db")
	db, err := sql.Open("sqlite3", filePathDB)
	if err != nil {
		return err
	}

	// check if the table already exists
	rows, err := db.QueryContext(ctx, "SELECT * FROM devices")
	if err != nil {
		if strings.Contains(err.Error(), errNoTableInternal) {
			// create the table if it does not exist
			// if we want to change the fields in the table, a migration function needs to be created
			cmd := `create table devices(`
			for index, fieldAndType := range supportedCols {
				cmd += fmt.Sprintf("%s %s", fieldAndType[0], fieldAndType[1])
				if index == len(supportedCols)-1 {
					cmd += ");"
				} else {
					cmd += ","
				}
			}
			if _, err = db.ExecContext(ctx, cmd); err != nil {
				return err
			}
		}
	} else {
		if rows.Err() != nil {
			return rows.Err()
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		if len(cols) != len(supportedCols) {
			for _, fieldAndType := range supportedCols {
				if _, err = db.ExecContext(ctx, "ALTER TABLE devices ADD COLUMN "+fieldAndType[0], fieldAndType[1]); err != nil {
					if strings.Contains(err.Error(), errDupeColNameInternal) {
						continue
					}
					return err
				}
			}
		}
	}

	// unregister devices
	cmd := "UPDATE devices SET " + isRegisteredDBKey + "=0"
	_, err = db.ExecContext(ctx, cmd)
	if err != nil {
		return err
	}

	g.db = db

	return nil
}

func (g *gateway) insertOrUpdateDeviceInDB(ctx context.Context, device deviceInfo) error {
	if g.db == nil {
		return errNoDB
	}

	cmd := "insert or replace into devices(" +
		devEUIDBKey + ", " +
		appSDBKey + ", " +
		nwkSDBKey + ", " +
		devAddrDBKey + ", " +
		fCntDownDBKey + ", " +
		nodeNameDBKey + ", " +
		minUplinkIntervalDBKey +
		") VALUES(?, ?, ?, ?, ?, ?, ?);"
	_, err := g.db.ExecContext(ctx, cmd,
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

func (g *gateway) findDeviceInDB(ctx context.Context, devEui []byte) (deviceInfo, error) {
	if g.db == nil {
		return deviceInfo{}, errNoDB
	}
	var zero deviceInfo
	newDevice := deviceInfo{}
	cmd := "select " +
		devEUIDBKey + ", " +
		appSDBKey + ", " +
		nwkSDBKey + ", " +
		devAddrDBKey + ", " +
		fCntDownDBKey + ", " +
		nodeNameDBKey + ", " +
		minUplinkIntervalDBKey +
		" from devices where devEui = ?;"

	if err := g.db.QueryRowContext(ctx, cmd,
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

func (g *gateway) getAllDevicesFromDB(ctx context.Context) ([]deviceInfo, error) {
	if g.db == nil {
		return nil, errNoDB
	}
	queryAll := "select " +
		devEUIDBKey + ", " +
		appSDBKey + ", " +
		nwkSDBKey + ", " +
		devAddrDBKey + ", " +
		fCntDownDBKey + ", " +
		nodeNameDBKey + ", " +
		minUplinkIntervalDBKey +
		" FROM devices;"
	rows, err := g.db.QueryContext(ctx, queryAll)
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
func (g *gateway) migrateDevicesFromJSONFile(ctx context.Context, pathPrefix string) error {
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

		var devices []deviceInfoOld
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
			newDeviceInfo, err := convertOldInfoToNew(device)
			if err != nil {
				return errTXTMigration
			}
			if err = g.insertOrUpdateDeviceInDB(ctx, newDeviceInfo); err != nil {
				return err
			}
		}
		if err := os.Remove(filePathTXT); err != nil {
			return err
		}
	}
	return nil
}

func convertOldInfoToNew(old deviceInfoOld) (deviceInfo, error) {
	// Update the fields in the map with the info from the file.
	devEUI, err := hex.DecodeString(old.DevEUI)
	if err != nil {
		return deviceInfo{}, err
	}
	// Update the fields in the map with the info from the file.
	appsKey, err := hex.DecodeString(old.AppSKey)
	if err != nil {
		return deviceInfo{}, err
	}

	savedAddr, err := hex.DecodeString(old.DevAddr)
	if err != nil {
		return deviceInfo{}, err
	}

	nwksKey, err := hex.DecodeString(old.NwkSKey)
	if err != nil {
		return deviceInfo{}, err
	}
	return deviceInfo{
		DevEUI:            devEUI,
		AppSKey:           appsKey,
		DevAddr:           savedAddr,
		NwkSKey:           nwksKey,
		FCntDown:          old.FCntDown,
		NodeName:          old.NodeName,
		MinUplinkInterval: old.MinUplinkInterval,
	}, nil
}

// deviceInfo is a struct containing OTAA device information.
// This info is saved across module restarts for each device.
type deviceInfo struct {
	DevEUI            []byte
	DevAddr           []byte
	AppSKey           []byte
	NwkSKey           []byte
	FCntDown          *uint16
	NodeName          string
	MinUplinkInterval float64
}

// deviceInfoOld is a struct containing OTAA device information.
// this info is saved across module restarts for each device.
// we can remove this once we are sure users no longer have the devicedata.txt files
// and we do not want to support adding devices via json.
type deviceInfoOld struct {
	DevEUI            string  `json:"dev_eui"`
	DevAddr           string  `json:"dev_addr"`
	AppSKey           string  `json:"app_skey"`
	NwkSKey           string  `json:"nwk_skey"`
	FCntDown          *uint16 `json:"fcnt_down"`
	NodeName          string  `json:"node_name"`
	MinUplinkInterval float64 `json:"min_uplink_interval"`
}
