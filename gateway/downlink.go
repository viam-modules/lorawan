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
	"go.viam.com/utils"
)

func findDownLinkChannel(uplinkFreq int) int {
	// channel number between 0-64
	upLinkFreqNum := (uplinkFreq - 902300000) / 200000
	downLinkChan := upLinkFreqNum % 8
	downLinkFreq := downLinkChan*600000 + 923300000
	return downLinkFreq

}

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool, uplinkFreq int, t time.Time, count int) error {
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

	// lock the mutex to prevent two sends at the same time.
	g.mu.Lock()
	defer g.mu.Unlock()
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

func accurateSleep(ctx context.Context, duration time.Duration) bool {
	// If we use utils.SelectContextOrWait(), we will wake up sometime after when we're supposed
	// to, which can be hundreds of microseconds later (because the process scheduler in the OS only
	// schedules things every millisecond or two). For use cases like a web server responding to a
	// query, that's fine. but when outputting a PWM signal, hundreds of microseconds can be a big
	// deal. To avoid this, we sleep for less time than we're supposed to, and then busy-wait until
	// the right time. Inspiration for this approach was taken from
	// https://blog.bearcats.nl/accurate-sleep-function/
	// On a raspberry pi 4, naively calling utils.SelectContextOrWait tended to have an error of
	// about 140-300 microseconds, while this version had an error of 0.3-0.6 microseconds.
	startTime := time.Now()
	maxBusyWaitTime := 1500 * time.Microsecond
	if duration > maxBusyWaitTime {
		shorterDuration := duration - maxBusyWaitTime
		if !utils.SelectContextOrWait(ctx, shorterDuration) {
			return false
		}
	}

	for time.Since(startTime) < duration {
		if err := ctx.Err(); err != nil {
			return false
		}
		// Otherwise, busy-wait some more
	}
	return true
}

// func createAckDownlink(devAddr []byte, nwkSKey types.AES128Key) ([]byte, error) {
// 	phyPayload := new(bytes.Buffer)

// 	// Mhdr confirmed data down
// 	if err := phyPayload.WriteByte(0xA0); err != nil {
// 		return nil, fmt.Errorf("failed to write MHDR: %w", err)
// 	}

// 	// the payload needs the dev addr to be in LE
// 	if _, err := phyPayload.Write(devAddr); err != nil {
// 		return nil, fmt.Errorf("failed to write DevAddr: %w", err)
// 	}

// 	// 3. FCtrl: ADR (1), RFU (0), ACK (1), FPending (0), FOptsLen (0)
// 	if err := phyPayload.WriteByte(0xA0); err != nil {
// 		return nil, fmt.Errorf("failed to write FCtrl: %w", err)
// 	}

// 	fCntBytes := make([]byte, 2)
// 	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
// 	if _, err := phyPayload.Write(fCntBytes); err != nil {
// 		return nil, fmt.Errorf("failed to write FCnt: %w", err)
// 	}

// 	devAddrBE := reverseByteArray(devAddr)
// 	mic, err := crypto.ComputeLegacyDownlinkMIC(types.AES128Key(nwkSKey), *types.MustDevAddr(devAddrBE), uint32(fCntDown), phyPayload.Bytes())
// 	if err != nil {
// 		return nil, err
// 	}

// 	if _, err := phyPayload.Write(mic[:]); err != nil {
// 		return nil, fmt.Errorf("failed to write mic: %w", err)
// 	}

// 	// increment fCntDown
// 	// TODO: write fcntDown to a file to persist across restarts.
// 	fCntDown += 1

// 	return phyPayload.Bytes(), nil
// }

func (g *gateway) createIntervalDownlink(device *node.Node, framePayload []byte) ([]byte, error) {
	payload := make([]byte, 0)

	// Mhdr unconfirmed data down
	payload = append(payload, 0x60)

	devAddrLE := reverseByteArray(device.Addr)

	payload = append(payload, devAddrLE...)

	// 3. FCtrl: ADR (0), RFU (0), ACK (0), FPending (0), FOptsLen (0)
	payload = append(payload, 0x00)

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, uint16(device.FCntDown))
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

	payload = append(payload, device.FPort) // 0x85 for tilt

	// 30 seconds
	// framePayload := []byte{0x01, 0x00, 0x00, 0x1E} //  dragino
	//framePayload := []byte{0xff, 0x10, 0xff} //tilt reset

	encrypted, err := crypto.EncryptDownlink(types.AES128Key(device.AppSKey), *types.MustDevAddr(device.Addr), uint32(device.FCntDown), framePayload)
	if err != nil {
		return nil, err
	}

	// payload = payload[:len(payload)-len(framePayload)]
	payload = append(payload, encrypted...)

	mic, err := crypto.ComputeLegacyDownlinkMIC(types.AES128Key(device.NwkSKey), *types.MustDevAddr(device.Addr), uint32(device.FCntDown), payload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	// increment fCntDown
	// TODO: write fcntDown to a file to persist across restarts.
	device.FCntDown += 1

	return payload, nil

}
