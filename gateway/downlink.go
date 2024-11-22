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

func (g *Gateway) sendDownLink(ctx context.Context, payload []byte, join bool) error {
	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:    C.uint32_t(Rx2Frequency),
		tx_mode:    C.uint8_t(0), // immediate mode
		rf_chain:   C.uint8_t(0),
		rf_power:   C.int8_t(26),    // tx power in dbm
		modulation: C.uint8_t(0x10), // LORA modulation
		bandwidth:  C.uint8_t(Rx2Bandwidth),
		datarate:   C.uint32_t(Rx2SF),
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
	if join {
		waitTime = JoinRx2WindowSec
	} else {
		waitTime = Rx2WindowSec
	}

	if !utils.SelectContextOrWait(ctx, time.Second*time.Duration(waitTime)) {
		return nil
	}

	// lock the mutex so there is not two sends at the same time.
	g.mu.Lock()
	defer g.mu.Unlock()
	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink")
	}

	return nil
}

func createDeviceTimeAns(devAddr []byte, encNwkKey types.AES128Key, downlinkNwkSKey types.AES128Key, fCnt uint32) ([]byte, error) {
	// Create buffer for the complete PHYPayload
	phyPayload := new(bytes.Buffer)

	// Mhdr
	if err := phyPayload.WriteByte(DownLinkMType); err != nil {
		return nil, fmt.Errorf("failed to write MHDR: %w", err)
	}

	if _, err := phyPayload.Write(devAddr); err != nil {
		return nil, fmt.Errorf("failed to write DevAddr: %w", err)
	}

	// fctrl
	if err := phyPayload.WriteByte(0x00); err != nil {
		return nil, fmt.Errorf("failed to write FCtrl: %w", err)
	}

	// frame count 2 bytes
	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, uint16(fCnt))
	if _, err := phyPayload.Write(fCntBytes); err != nil {
		return nil, fmt.Errorf("failed to write FCnt: %w", err)
	}

	// fport is 0 for mac command messages
	if err := phyPayload.WriteByte(0); err != nil {
		return nil, fmt.Errorf("failed to write FPort: %w", err)
	}

	// Create frame payload
	// Time is represented as seconds since GPS epoch
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	secondsSinceGPSEpoch := uint32(now.Sub(gpsEpoch).Seconds())

	// Calculate fractional seconds (1/256 resolution)
	nanoseconds := now.Nanosecond()
	fractionalSeconds := uint8((nanoseconds / 1e6) * 256 / 1000) // Convert ms to 1/256 resolution

	// Create FRMPayload buffer
	frmPayload := new(bytes.Buffer)

	// Write GPS time (4 bytes, big-endian)
	if err := binary.Write(frmPayload, binary.BigEndian, secondsSinceGPSEpoch); err != nil {
		return nil, fmt.Errorf("failed to encode GPS epoch time: %w", err)
	}

	// Write fractional seconds (1 byte)
	if err := frmPayload.WriteByte(fractionalSeconds); err != nil {
		return nil, fmt.Errorf("failed to encode fractional seconds: %w", err)
	}

	// Encrypt FRMPayload
	encryptedPayload, err := crypto.EncryptDownlink(encNwkKey, types.DevAddr(devAddr), fCnt, frmPayload.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt payload: %w", err)
	}

	// Add encrypted FRMPayload to PHYPayload
	if _, err := phyPayload.Write(encryptedPayload); err != nil {
		return nil, fmt.Errorf("failed to write encrypted payload: %w", err)
	}

	// 7. Calculate and add MIC
	// For downlink messages, we need both FCntDown and AFCntDown
	// AFCntDown is used for MIC calculation in LoRaWAN 1.0.x
	// We'll use 0 for AFCntDown as this is a simple DeviceTimeAns
	afCntDown := uint32(0)
	mic, err := crypto.ComputeDownlinkMIC(downlinkNwkSKey, types.DevAddr(devAddr), fCnt, afCntDown, phyPayload.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to compute MIC: %w", err)
	}

	// Add MIC to PHYPayload
	if _, err := phyPayload.Write(mic[:]); err != nil {
		return nil, fmt.Errorf("failed to write MIC: %w", err)
	}

	return phyPayload.Bytes(), nil
}
