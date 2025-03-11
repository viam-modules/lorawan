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
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

func findDownLinkChannel(uplinkFreq int) int {
	// channel number between 0-64
	upLinkFreqNum := (uplinkFreq - 902300000) / 200000
	downLinkChan := upLinkFreqNum % 8
	downLinkFreq := downLinkChan*600000 + 923300000
	return downLinkFreq

}

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool, t time.Time, count int) error {
	freq := rx2Frequenecy
	dataRate := rx2SF

	waitTime := 2 * 10e5
	if join {
		waitTime = 6 * 10e5
	}
	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:     C.uint32_t(freq),
		freq_offset: C.int8_t(0),
		tx_mode:     C.uint8_t(1), // time stamp
		count_us:    C.uint32_t(count + int(waitTime)),
		rf_chain:    C.uint8_t(0),
		rf_power:    C.int8_t(26),    // tx power in dbm
		modulation:  C.uint8_t(0x10), // LORA modulation
		bandwidth:   C.uint8_t(0x06), //500k
		datarate:    C.uint32_t(dataRate),
		coderate:    C.uint8_t(0x01), // code rate 4/5
		invert_pol:  C.bool(true),    // Downlinks are always reverse polarity.
		size:        C.uint16_t(len(payload)),
		preamble:    C.uint16_t(8),
		no_crc:      C.bool(true), // CRCs in uplinks only
		no_header:   C.bool(false),
	}

	var cPayload [256]C.uchar
	for i, b := range payload {
		cPayload[i] = C.uchar(b)
	}
	txPkt.payload = cPayload

	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink packet")
	}
	var status C.uint8_t
	for {
		C.lgw_status(txPkt.rf_chain, 1, &status)
		if err := ctx.Err(); err != nil {
			break
		}
		if int(status) == 2 {
			break
		} else {
			time.Sleep(2 * time.Millisecond)
		}
	}

	g.logger.Infof("sent the downlink packet")

	return nil
}

func (g *gateway) createDownlink(device *node.Node, framePayload []byte) ([]byte, error) {
	payload := make([]byte, 0)

	// Mhdr unconfirmed data down
	payload = append(payload, 0x60)

	devAddrLE := reverseByteArray(device.Addr)

	payload = append(payload, devAddrLE...)

	// 3. FCtrl: ADR (0), RFU (0), ACK (0), FPending (0), FOptsLen (0)
	payload = append(payload, 0x00)

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, uint16(device.FCntDown)+1)
	payload = append(payload, fCntBytes...)

	// fopts := []byte{
	// 	0b00111001, // data rate and tx power
	// 	0xFF,
	// 	0x00,
	// 	0x01,
	// }

	// fopts := []byte{0x3A, 0xFF, 0x00, 0x01}

	// payload = append(payload, fopts...)

	// // Fport
	// Change to 0x00 for MAC-only downlink

	g.logger.Infof("FPORT: %x", device.FPort)

	payload = append(payload, device.FPort) // 0x85 for tilt

	// 30 seconds
	// framePayload := []byte{0x01, 0x00, 0x00, 0x1E} //  dragino
	//framePayload := []byte{0xff, 0x10, 0xff} //tilt reset

	encrypted, err := crypto.EncryptDownlink(types.AES128Key(device.AppSKey), *types.MustDevAddr(device.Addr), uint32(device.FCntDown)+1, framePayload)
	if err != nil {
		return nil, err
	}

	// payload = payload[:len(payload)-len(framePayload)]
	payload = append(payload, encrypted...)

	mic, err := crypto.ComputeLegacyDownlinkMIC(types.AES128Key(device.NwkSKey), *types.MustDevAddr(device.Addr), uint32(device.FCntDown)+1, payload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	// increment fCntDown
	// TODO: write fcntDown to a file to persist across restarts.
	device.FCntDown += 1

	return payload, nil

}
