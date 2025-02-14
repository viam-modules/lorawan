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
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/utils"
)

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool) error {
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
		size:       C.uint16_t(len(payload)),
	}

	var cPayload [256]C.uchar
	for i, b := range payload {
		cPayload[i] = C.uchar(b)
	}
	txPkt.payload = cPayload

	// join request and other downlinks have different windows for class A devices.
	var waitTime int
	switch join {
	case true:
		waitTime = joinRx2WindowSec
	default:
		waitTime = 1
	}

	if !utils.SelectContextOrWait(ctx, time.Second*time.Duration(waitTime)) {
		return errors.New("context canceled")
	}

	// lock the mutex to prevent two sends at the same time.
	g.mu.Lock()
	defer g.mu.Unlock()
	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink packet")
	}

	g.logger.Infof("sent the downolink packet")

	return nil
}

var fCntDown uint16 = 0

func createAckDownlink(devAddr []byte, nwkSKey types.AES128Key) ([]byte, error) {
	phyPayload := new(bytes.Buffer)

	// Mhdr confirmed data down
	if err := phyPayload.WriteByte(0xA0); err != nil {
		return nil, fmt.Errorf("failed to write MHDR: %w", err)
	}

	// the payload needs the dev addr to be in LE
	devAddrLE := reverseByteArray(devAddr)
	if _, err := phyPayload.Write(devAddrLE); err != nil {
		return nil, fmt.Errorf("failed to write DevAddr: %w", err)
	}

	// 3. FCtrl: ADR (1), RFU (0), ACK (1), FPending (0), FOptsLen (5)
	if err := phyPayload.WriteByte(0xA5); err != nil {
		return nil, fmt.Errorf("failed to write FCtrl: %w", err)
	}

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
	if _, err := phyPayload.Write(fCntBytes); err != nil {
		return nil, fmt.Errorf("failed to write FCnt: %w", err)
	}

	// Fport
	if err := phyPayload.WriteByte(0); err != nil { // Change to 0x00 for MAC-only downlink
		return nil, fmt.Errorf("failed to write FPort: %w", err)
	}

	mic, err := crypto.ComputeLegacyDownlinkMIC(types.AES128Key(nwkSKey), *types.MustDevAddr(devAddr), uint32(fCntDown), phyPayload.Bytes())
	if err != nil {
		return nil, err
	}

	if _, err := phyPayload.Write(mic[:]); err != nil {
		return nil, fmt.Errorf("failed to write mic: %w", err)
	}

	// increment fCntDownå
	// TODO: write fcntDown to a file to persist across restarts.
	fCntDown += 1

	return phyPayload.Bytes(), nil

}
