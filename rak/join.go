package rak

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
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

func (r *rak7391) handleJoin(ctx context.Context, payload []byte, packetTime time.Time, c concentrator) error {
	jr, device, err := r.parseJoinRequestPacket(payload)
	if err != nil {
		return err
	}

	joinAccept, err := r.generateJoinAccept(ctx, jr, device)
	if err != nil {
		return err
	}

	r.logger.Infof("sending join accept to %s", device.NodeName)

	return r.sendDownlink(ctx, joinAccept, true, packetTime, c)
}

// payload of join request consists of
// | MHDR | JOIN EUI | DEV EUI  |   DEV NONCE  | MIC   |
// | 1 B  |   8 B    |    8 B   |     2 B      |  4 B  |
// https://lora-alliance.org/wp-content/uploads/2020/11/lorawan1.0.3.pdf page 34 for more info on join request.
func (r *rak7391) parseJoinRequestPacket(payload []byte) (joinRequest, *node.Node, error) {
	// join request should always contain 23 bytes, if not something went wrong.
	if len(payload) != 23 {
		return joinRequest{}, nil, errInvalidLength
	}
	var jr joinRequest

	// everything in the join request payload is little endian
	jr.joinEUI = payload[1:9]
	jr.devEUI = payload[9:17]
	jr.devNonce = payload[17:19]
	jr.mic = payload[19:23]

	matched := &node.Node{}

	// device.devEUI is in big endian - reverse to compare and find device.
	devEUIBE := reverseByteArray(jr.devEUI)

	// match the dev eui to gateway device
	for _, device := range r.devices {
		if bytes.Equal(device.DevEui, devEUIBE) {
			matched = device
		}
	}

	if matched.NodeName == "" {
		r.logger.Debugf("received join request with dev EUI %x - unknown device, ignoring", devEUIBE)
		return joinRequest{}, nil, errNoDevice
	}

	if bytes.Equal(matched.LastDevNonce, jr.devNonce) {
		r.logger.Debugf("found identical dev nonce - skipping join request")
		return joinRequest{}, nil, errors.New("already handled dev nonce")
	}

	matched.LastDevNonce = jr.devNonce
	matched.FCntUp = math.MaxUint16

	err := validateMIC(types.AES128Key(matched.AppKey), payload)
	if err != nil {
		return joinRequest{}, nil, err
	}

	return jr, matched, nil
}

// Format of Join Accept message:
// | MHDR | JOIN NONCE | NETID |   DEV ADDR  | DL | RX DELAY |   CFLIST   | MIC  |
// | 1 B  |     3 B    |   3 B |     4 B     | 1B |    1B    |  0 or 16   | 4 B  |
// https://lora-alliance.org/wp-content/uploads/2020/11/lorawan1.0.3.pdf page 35 for more info on join accept.
func (r *rak7391) generateJoinAccept(ctx context.Context, jr joinRequest, d *node.Node) ([]byte, error) {
	// generate random join nonce.
	jn, err := generateJoinNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate join nonce: %w", err)
	}

	devEUIBE := reverseByteArray(jr.devEUI)

	// generate a random device address to identify uplinks.
	d.Addr, err = generateDevAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to generate dev addr: %w", err)
	}

	// generate a random device address to identify uplinks.
	d.Addr, err = generateDevAddr()
	if err != nil {
		return nil, fmt.Errorf("failed to generate dev addr: %w", err)
	}

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
	payload = append(payload, r.regionInfo.DlSettings)
	payload = append(payload, 0x01) // rx1 delay: 1 second

	payload = append(payload, r.regionInfo.CfList...)

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
		DevEUI:   strings.ToUpper(hex.EncodeToString(devEUIBE)),
		DevAddr:  strings.ToUpper(hex.EncodeToString(d.Addr)),
		AppSKey:  strings.ToUpper(hex.EncodeToString(d.AppSKey)),
		NwkSKey:  strings.ToUpper(hex.EncodeToString(d.NwkSKey)),
		FCntDown: &d.FCntDown,
		NodeName: d.NodeName,
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err = r.insertOrUpdateDeviceInDB(ctxTimeout, deviceInfo); err != nil {
		// if this errors, log but still return join accept.
		r.logger.Errorf("failed to write device info to db: %v", err)
	}

	// return the encrypted join accept message
	return ja, nil
}

// Generates random 4 byte dev addr. This is used for the network to identify device's data uplinks.
func generateDevAddr() ([]byte, error) {
	devAddr := make([]byte, 4)

	// Generate 4 random byte
	_, err := rand.Read(devAddr)
	if err != nil {
		return nil, err
	}

	// first 7 MSB of devAddr must match the network ID
	devAddr[0] = 1
	devAddr[1] = 2

	return devAddr, nil
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

// generates random 3 byte join nonce.
func generateJoinNonce() ([]byte, error) {
	nonce := make([]byte, 3)

	// Generate 3 random bytes
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	return nonce, nil
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
