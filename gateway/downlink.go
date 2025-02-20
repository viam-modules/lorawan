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

func findDownLinkChannel(uplinkFreq int) int {
	// channel number between 0-64
	upLinkFreqNum := (uplinkFreq - 902300000) / 200000
	downLinkChan := upLinkFreqNum % 8
	downLinkFreq := downLinkChan*600000 + 923300000
	return downLinkFreq

}

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool, uplinkFreq int) error {
	dataRate := 10
	// freq := findDownLinkChannel(uplinkFreq)
	// g.logger.Infof("freq: %d", freq)
	freq := uplinkFreq
	if join {
		freq = rx2Frequenecy
		dataRate = rx2SF
	}

	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:    C.uint32_t(freq),
		tx_mode:    C.uint8_t(0), // immediate mode
		rf_chain:   C.uint8_t(0),
		rf_power:   C.int8_t(26),            // tx power in dbm
		modulation: C.uint8_t(0x10),         // LORA modulation
		bandwidth:  C.uint8_t(rx2Bandwidth), //500k
		datarate:   C.uint32_t(dataRate),
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
		waitTime = 1 // 1 for rx1
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

var fCntDown uint16 = 1

func createAckDownlink(devAddr []byte, nwkSKey types.AES128Key) ([]byte, error) {
	phyPayload := new(bytes.Buffer)

	// Mhdr confirmed data down
	if err := phyPayload.WriteByte(0xA0); err != nil {
		return nil, fmt.Errorf("failed to write MHDR: %w", err)
	}

	// the payload needs the dev addr to be in LE
	if _, err := phyPayload.Write(devAddr); err != nil {
		return nil, fmt.Errorf("failed to write DevAddr: %w", err)
	}

	// 3. FCtrl: ADR (1), RFU (0), ACK (1), FPending (0), FOptsLen (0)
	if err := phyPayload.WriteByte(0xA0); err != nil {
		return nil, fmt.Errorf("failed to write FCtrl: %w", err)
	}

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
	if _, err := phyPayload.Write(fCntBytes); err != nil {
		return nil, fmt.Errorf("failed to write FCnt: %w", err)
	}

	devAddrBE := reverseByteArray(devAddr)
	mic, err := crypto.ComputeLegacyDownlinkMIC(types.AES128Key(nwkSKey), *types.MustDevAddr(devAddrBE), uint32(fCntDown), phyPayload.Bytes())
	if err != nil {
		return nil, err
	}

	if _, err := phyPayload.Write(mic[:]); err != nil {
		return nil, fmt.Errorf("failed to write mic: %w", err)
	}

	// increment fCntDown
	// TODO: write fcntDown to a file to persist across restarts.
	fCntDown += 1

	return phyPayload.Bytes(), nil
}

func (g *gateway) createIntervalDownlink(devAddr []byte, nwkSKey, appSKey types.AES128Key) ([]byte, error) {
	payload := make([]byte, 0)

	// Mhdr unconfirmed data down
	payload = append(payload, 0x60)

	payload = append(payload, devAddr...)

	// 3. FCtrl: ADR (1), RFU (0), ACK (0), FPending (0), FOptsLen (4)
	payload = append(payload, 0x84)

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
	payload = append(payload, fCntBytes...)

	fopts := []byte{0x3A, 0xFF, 0x00, 0x01}

	payload = append(payload, fopts...)

	// // Fport
	// Change to 0x00 for MAC-only downlink
	payload = append(payload, 0x01)

	// 30 sec
	framePayload := []byte{0x01, 0x00, 0x00, 0x1E}

	devAddrBE := reverseByteArray(devAddr)

	encrypted, err := crypto.EncryptDownlink(appSKey, *types.MustDevAddr(devAddrBE), uint32(fCntDown), framePayload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, encrypted...)

	mic, err := crypto.ComputeLegacyDownlinkMIC(nwkSKey, *types.MustDevAddr(devAddrBE), uint32(fCntDown), payload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	// increment fCntDown
	// TODO: write fcntDown to a file to persist across restarts.
	fCntDown += 1

	return payload, nil

}

// 60 54ea0201 80 0100 01 fb18282b 78d0fa1b
