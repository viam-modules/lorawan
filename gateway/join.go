package gateway

/*
#cgo CFLAGS: -I./sx1302/libloragw/inc -I./sx1302/libtools/inc
#cgo LDFLAGS: -L./sx1302/libloragw -lloragw -L./sx1302/libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdlib.h>

*/
import "C"
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"gateway/node"
	"math/rand"
	"time"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto/cryptoservices"
	"go.thethings.network/lorawan-stack/v3/pkg/ttnpb"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/utils"
)

type joinRequest struct {
	joinEUI  []byte
	devEUI   []byte
	devNonce []byte
	mic      []byte
}

const (
	joinRx2WindowSec = 6         // rx2 delay for sending join accept message.
	rx2Frequenecy    = 923300000 // Frequency to send downlinks on rx2 window
	rx2SF            = 12        // spreading factor for rx2 window
	rx2Bandwidth     = 0x06      // 500k bandwidth
)

var errNoDevice = errors.New("received join request from unknown device")

// network id for the device to identify the network. Must be 3 bytes.
var netID = []byte{1, 2, 3}

func (g *Gateway) handleJoin(ctx context.Context, payload []byte) error {
	jr, device, err := g.parseJoinRequestPacket(payload)
	if err != nil {
		return err
	}

	joinAccept, err := generateJoinAccept(ctx, jr, device)
	if err != nil {
		return err
	}

	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:    C.uint32_t(rx2Frequenecy),
		tx_mode:    C.uint8_t(0), // immediate mode
		rf_chain:   C.uint8_t(0),
		rf_power:   C.int8_t(26),    // tx power in dbm
		modulation: C.uint8_t(0x10), // LORA modulation
		bandwidth:  C.uint8_t(rx2Bandwidth),
		datarate:   C.uint32_t(rx2SF),
		coderate:   C.uint8_t(0x01), // code rate 4/5
		invert_pol: C.bool(true),    // Downlinks are always reverse polarity.
		size:       C.uint16_t(len(joinAccept)),
	}

	var cPayload [256]C.uchar
	for i, b := range joinAccept {
		cPayload[i] = C.uchar(b)
	}
	txPkt.payload = cPayload

	// send on rx2 window - opens 6 seconds after join request.
	if !utils.SelectContextOrWait(ctx, time.Second*joinRx2WindowSec) {
		return nil
	}

	// lock so there is not two sends at the same time.
	g.mu.Lock()
	defer g.mu.Unlock()
	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send join accept packet")
	}

	return nil
}

// payload of join request consists of
// | MHDR | JOIN EUI | DEV EUI  |   DEV NONCE  | MIC   |
// | 1 B  |   8 B    |    8 B   |     2 B      |  4 B  |
// https://lora-alliance.org/wp-content/uploads/2020/11/lorawan1.0.3.pdf page 34 for more info on join request.
func (g *Gateway) parseJoinRequestPacket(payload []byte) (joinRequest, *node.Node, error) {
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
		g.logger.Debugf("received join requested with dev EUI %x - unknown device, ignoring", devEUIBE)
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
func generateJoinAccept(ctx context.Context, jr joinRequest, d *node.Node) ([]byte, error) {
	// generate random join nonce.
	jn := generateJoinNonce()

	// generate a random device address to identify uplinks.
	d.Addr = generateDevAddr()

	// the join accept payload needs everything to be LE, so reverse the BE fields.
	netIDLE := reverseByteArray(netID)
	jnLE := reverseByteArray(jn)
	dAddrLE := reverseByteArray(d.Addr)

	payload := make([]byte, 0)
	payload = append(payload, 0x20)
	payload = append(payload, jnLE[:]...)
	payload = append(payload, netIDLE[:]...)
	payload = append(payload, dAddrLE[:]...)
	payload = append(payload, 0x00) // dl settings: default
	payload = append(payload, 0x01) // rx delay: 1 second

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
	//add back mhdr
	ja = append(ja, 0x20)
	ja = append(ja, enc...)

	// generate the session keys
	appsKey, err := generateKeys(ctx, jr.devNonce, jr.joinEUI, jn, jr.devEUI, netID, types.AES128Key(d.AppKey))
	if err != nil {
		return nil, err
	}

	d.AppSKey = appsKey[:]

	// return the encrypted join accept message
	return ja, nil

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
		return errors.New("invalid MIC")
	}
	return nil

}

func generateKeys(ctx context.Context, devNonce, joinEUI, jn, devEUI, networkID []byte, appKey types.AES128Key) (types.AES128Key, error) {
	cryptoDev := &ttnpb.EndDevice{
		Ids: &ttnpb.EndDeviceIdentifiers{JoinEui: joinEUI, DevEui: devEUI},
	}

	// TTN expects big endian dev nonce
	devNonceBE := reverseByteArray(devNonce)
	applicationCryptoService := cryptoservices.NewMemory(nil, &appKey)

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
		return types.AES128Key{}, fmt.Errorf("failed to generate AppSKey: %w", err)
	}

	return appsKey, nil

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
