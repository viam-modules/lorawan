package gateway

/*
#cgo CFLAGS: -I${SRCDIR}/../sx1302/libloragw/inc -I${SRCDIR}/../sx1302/libtools/inc
#cgo LDFLAGS: -L${SRCDIR}/../sx1302/libloragw -lloragw -L${SRCDIR}/../sx1302/libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdlib.h>

*/
import "C"

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"time"

	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto/cryptoservices"
	"go.thethings.network/lorawan-stack/v3/pkg/ttnpb"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

type joinRequest struct {
	joinEUI  []byte
	devEUI   []byte
	devNonce []byte
	mic      []byte
}

// network id for the device to identify the network. Must be 3 bytes.
var netID = []byte{1, 2, 3}

func (g *gateway) handleJoin(ctx context.Context, payload []byte, packetTime time.Time) error {
	jr, device, err := g.parseJoinRequestPacket(payload)
	if err != nil {
		return err
	}

	joinAccept, err := g.generateJoinAccept(ctx, jr, device)
	if err != nil {
		return err
	}

	g.logger.Infof("sending join accept to %s", device.NodeName)

	return g.sendDownlink(ctx, joinAccept, true, packetTime)
}

// payload of join request consists of
// | MHDR | JOIN EUI | DEV EUI  |   DEV NONCE  | MIC   |
// | 1 B  |   8 B    |    8 B   |     2 B      |  4 B  |
// https://lora-alliance.org/wp-content/uploads/2020/11/lorawan1.0.3.pdf page 34 for more info on join request.
func (g *gateway) parseJoinRequestPacket(payload []byte) (joinRequest, *node.Node, error) {
	var joinRequest joinRequest

	// everything in the join request payload is little endian
	joinRequest.joinEUI = payload[1:9]
	joinRequest.devEUI = payload[9:17]
	joinRequest.devNonce = payload[17:19]
	joinRequest.mic = payload[19:23]

	matched := &node.Node{}

	// device.devEUI is in big endian - reverse to compare and find device.
	devEUIBE := reverseByteArray(joinRequest.devEUI)

	// match the dev eui to gateway device
	for _, device := range g.devices {
		if bytes.Equal(device.DevEui, devEUIBE) {
			matched = device
		}
	}

	if matched.NodeName == "" {
		g.logger.Debugf("received join request with dev EUI %x - unknown device, ignoring", devEUIBE)
		return joinRequest, nil, errNoDevice
	}

	err := validateMIC(types.AES128Key(matched.AppKey), payload)
	if err != nil {
		return joinRequest, nil, err
	}

	return joinRequest, matched, nil
}

// Format of Join Accept message:
// | MHDR | JOIN NONCE | NETID |   DEV ADDR  | DL | RX DELAY |   CFLIST   | MIC  |
// | 1 B  |     3 B    |   3 B |     4 B     | 1B |    1B    |  0 or 16   | 4 B  |
// https://lora-alliance.org/wp-content/uploads/2020/11/lorawan1.0.3.pdf page 35 for more info on join accept.
func (g *gateway) generateJoinAccept(ctx context.Context, jr joinRequest, d *node.Node) ([]byte, error) {
	// generate random join nonce.
	jn := generateJoinNonce()

	devEUIBE := reverseByteArray(jr.devEUI)

	// Check if this device is already present in the file.
	// If it is, remove it since the join procedure is being redone.

	err := g.searchAndRemove(g.dataFile, devEUIBE)
	if err != nil {
		// If this errors, log and continue as we can still complete the join procedure without the file.
		g.logger.Errorf("failed to search and remove device info from file: %v", err)
	}

	// generate a random device address to identify uplinks.
	d.Addr = generateDevAddr()

	// the join accept payload needs everything to be LE, so reverse the BE fields.
	netIDLE := reverseByteArray(netID)
	jnLE := reverseByteArray(jn)
	dAddrLE := reverseByteArray(d.Addr)

	payload := make([]byte, 0)
	payload = append(payload, joinAcceptMHdr)
	//nolint:all
	payload = append(payload, jnLE[:]...)
	//nolint:all
	payload = append(payload, netIDLE[:]...)
	//nolint:all
	payload = append(payload, dAddrLE[:]...)

	// DLSettings byte:
	// Bit 7: OptNeg (0)
	// Bits 6-4: RX1DROffset
	// Bits 3-0: RX2DR
	// Use data rate 8 for rx2 downlinks
	// DR8 = SF12 BW 500K
	// See lorawan1.0.3 regional specs doc 2.5.3 for a table of data rates to SF/BW
	payload = append(payload, 0x08)
	payload = append(payload, 0x01) // rx1 delay: 1 second

	// CFList for US915 using Channel Mask
	// This tells the device to only transmit on channels 0-7
	cfList := []byte{
		0xFF, // Enable channels 0-7
		0x00, // Disable channels 8-15
		0x00, // Disable channels 16-23
		0x00, // Disable channels 24-31
		0x00, // Disable channels 32-39
		0x00, // Disable channels 40-47
		0x00, // Disable channels 48-55
		0x00, // Disable channels 56-63
		0x01, // Enable channel 64, disable 65-71
		0x00, // Disable channels 72-79
		0x00, // RFU (reserved for future use)
		0x00, // RFU
		0x00, // RFU
		0x00, // RFU
		0x00, // RFU
		0x01, // CFList Type = 1 (Channel Mask)
	}

	payload = append(payload, cfList...)

	// generate MIC
	resMIC, err := crypto.ComputeLegacyJoinAcceptMIC(types.AES128Key(d.AppKey), payload)
	if err != nil {
		return nil, err
	}

	// everything but the mhdr needs to be encrypted.
	payload = payload[1:]

	payload = append(payload, resMIC[:]...)

	enc, err := crypto.EncryptJoinAccept(types.AES128Key(d.AppKey), payload)
	if err != nil {
		return nil, err
	}
	ja := make([]byte, 0)
	// add back mhdr
	ja = append(ja, joinAcceptMHdr)
	ja = append(ja, enc...)

	// generate the session keys
	keys, err := generateKeys(ctx, jr.devNonce, jr.joinEUI, jn, jr.devEUI, netID, types.AES128Key(d.AppKey))
	if err != nil {
		return nil, err
	}

	d.AppSKey = keys.appSKey
	d.NwkSKey = keys.nwkSKey
	d.FCntDown = 0

	// Save the OTAA info to the data file.
	deviceInfo := deviceInfo{
		DevEUI:   fmt.Sprintf("%X", devEUIBE),
		DevAddr:  fmt.Sprintf("%X", d.Addr),
		AppSKey:  fmt.Sprintf("%X", d.AppSKey),
		NwkSKey:  fmt.Sprintf("%X", d.NwkSKey),
		FCntDown: d.FCntDown,
	}

	err = g.addDeviceInfoToFile(g.dataFile, deviceInfo)
	if err != nil {
		// if this errors, log but still return join accept.
		g.logger.Errorf("failed to write device info to file: %v", err)
	}

	// return the encrypted join accept message
	return ja, nil
}

// This function searches for the device in the persistent data file based on the dev EUI sent in the JR.
// If the dev EUI is found, the device info is removed from the file.
// The file will later be updated with the info from the new join procedure.
func (g *gateway) searchAndRemove(file *os.File, devEUI []byte) error {
	g.dataMu.Lock()
	defer g.dataMu.Unlock()
	// Read the device info from the file
	devices, err := readFromFile(file)
	if err != nil {
		return fmt.Errorf("failed to read device info from file: %w", err)
	}

	// Check if the devEUI is already present
	for _, device := range devices {
		dev, err := hex.DecodeString(device.DevEUI)
		if err != nil {
			return fmt.Errorf("failed to decode file's dev addr: %w", err)
		}

		if bytes.Equal(dev, devEUI) {
			err = removeDeviceInfoFromFile(file, device)
			if err != nil {
				return fmt.Errorf("failed to remove old device info from file: %w", err)
			}
			break
		}
	}
	return nil
}

// Generates random 4 byte dev addr. This is used for the network to identify device's data uplinks.
func generateDevAddr() []byte {
	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	num1 := rand.Intn(255)
	num2 := rand.Intn(255)

	// first 7 MSB of devAddr is the network ID.
	return []byte{1, 2, byte(num1), byte(num2)}
}

// Validates the message integrity code sent in the join request.
// the MIC is used to verify authenticity of the message.
func validateMIC(appKey types.AES128Key, payload []byte) error {
	mic, err := crypto.ComputeJoinRequestMIC(appKey, payload[:19])
	if err != nil {
		return err
	}

	if !bytes.Equal(payload[19:], mic[:]) {
		return errInvalidMIC
	}
	return nil
}

type sessionKeys struct {
	appSKey []byte
	nwkSKey []byte
}

func generateKeys(ctx context.Context, devNonce, joinEUI, jn, devEUI, networkID []byte, appKey types.AES128Key) (sessionKeys, error) {
	cryptoDev := &ttnpb.EndDevice{
		Ids: &ttnpb.EndDeviceIdentifiers{JoinEui: joinEUI, DevEui: devEUI},
	}

	// TTN expects big endian dev nonce
	devNonceBE := reverseByteArray(devNonce)
	applicationCryptoService := cryptoservices.NewMemory(nil, &appKey)

	var keys sessionKeys

	// generate the appSKey!
	// all inputs here are big endian.
	appsKey, err := applicationCryptoService.DeriveAppSKey(
		ctx,
		cryptoDev,
		ttnpb.MACVersion_MAC_V1_0_3,
		types.JoinNonce(jn),
		types.DevNonce(devNonceBE),
		types.NetID(networkID),
	)
	if err != nil {
		return sessionKeys{}, fmt.Errorf("failed to generate AppSKey: %w", err)
	}

	keys.appSKey = appsKey[:]

	nwkSKey := crypto.DeriveLegacyNwkSKey(
		appKey,
		types.JoinNonce(jn),
		types.NetID(networkID),
		types.DevNonce(devNonceBE))

	keys.nwkSKey = nwkSKey[:]

	return keys, nil
}

// generates random 3 byte join nonce
func generateJoinNonce() []byte {
	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	num1 := rand.Intn(255)
	num2 := rand.Intn(255)
	num3 := rand.Intn(255)

	return []byte{byte(num1), byte(num2), byte(num3)}
}

// reverseByteArray creates a new array reversed of the input.
// Used to convert little endian fields to big endian and vice versa.
func reverseByteArray(arr []byte) []byte {
	reversed := make([]byte, len(arr))

	for i, j := 0, len(arr)-1; i < len(arr); i, j = i+1, j-1 {
		reversed[i] = arr[j]
	}
	return reversed
}

func removeDeviceInfoFromFile(file *os.File, devToRemove deviceInfo) error {
	// Read the existing data from the file
	devices, err := readFromFile(file)
	if err != nil {
		return err
	}

	if devices == nil {
		return errors.New("no devices to remove")
	}

	// Filter out the device based on dev EUI
	var updatedDevices []deviceInfo
	for _, device := range devices {
		if device.DevEUI != devToRemove.DevEUI {
			updatedDevices = append(updatedDevices, device)
		}
	}

	err = writeToFile(file, updatedDevices)
	if err != nil {
		return err
	}

	return nil
}

// Function to write the device info from the persitent data file.
func (g *gateway) addDeviceInfoToFile(file *os.File, newDevice deviceInfo) error {
	g.dataMu.Lock()
	defer g.dataMu.Unlock()
	// Read the existing data from the file
	devices, err := readFromFile(file)
	if err != nil {
		return err
	}

	if devices == nil {
		devices = []deviceInfo{}
	}
	// Append the new device to the existing array
	devices = append(devices, newDevice)

	err = writeToFile(file, devices)
	if err != nil {
		return err
	}
	return nil
}

func writeToFile(file *os.File, devices []deviceInfo) error {
	// Reset the file pointer and truncate the file to overwrite it
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to the beginning of the file: %w", err)
	}

	err = file.Truncate(0)
	if err != nil {
		return fmt.Errorf("failed to truncate the file: %w", err)
	}

	// Write the updated array back to the file
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device info to JSON: %w", err)
	}

	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write updated data to file: %w", err)
	}
	return nil
}

// Function to read the device info from the persitent data file.
func readFromFile(file *os.File) ([]deviceInfo, error) {
	// Reset file pointer to the beginning
	_, err := file.Seek(0, io.SeekStart)
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
