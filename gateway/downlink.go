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
	"fmt"
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

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool, packetTime time.Time) error {
	freq := rx2Frequenecy
	dataRate := rx2SF

	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:     C.uint32_t(freq),
		freq_offset: C.int8_t(0),
		tx_mode:     C.uint8_t(0), // immediate
		rf_chain:    C.uint8_t(0),
		rf_power:    C.int8_t(26),    // tx power in dbm
		modulation:  C.uint8_t(0x10), // LORA modulation
		bandwidth:   C.uint8_t(0x06), // 500k
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

	var waitDuration time.Duration
	switch join {
	case true:
		// 47709/32*time.Microsecond is the internal delay of sending a packet
		waitDuration = (6 * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	default:
		waitDuration = (2 * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	}

	if !accurateSleep(ctx, waitDuration) {
		return fmt.Errorf("error sending downlink: %w", ctx.Err())
	}

	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink packet")
	}
	var status C.uint8_t
	for {
		C.lgw_status(txPkt.rf_chain, 1, &status)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("error sending downlink: %w", ctx.Err())
		}
		// status of 2 means send was successful
		if int(status) == 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
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

// Downlink payload structure
// | MHDR | DEV ADDR | FCTRL | FCNTDOWN |  FOPTS (optional)  |  FPORT | encrypted frame payload  |  MIC |
// | 1 B  |   4 B    |  1 B  |    2 B   |       variable     |   1 B  |      variable            | 4 B  |
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

	payload = append(payload, device.FPort)

	// 30 seconds
	// framePayload := []byte{0x01, 0x00, 0x00, 0x1E} //  dragino
	// framePayload := []byte{0xff, 0x10, 0xff} //tilt reset

	encrypted, err := crypto.EncryptDownlink(
		types.AES128Key(device.AppSKey), *types.MustDevAddr(device.Addr), device.FCntDown+1, framePayload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, encrypted...)

	mic, err := crypto.ComputeLegacyDownlinkMIC(
		types.AES128Key(device.NwkSKey), *types.MustDevAddr(device.Addr), device.FCntDown+1, payload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	// increment fCntDown
	device.FCntDown++

	// create new deviceInfo to update the fcntDown in the file.
	deviceInfo := deviceInfo{
		DevEUI:   fmt.Sprintf("%X", device.DevEui),
		DevAddr:  fmt.Sprintf("%X", device.Addr),
		AppSKey:  fmt.Sprintf("%X", device.AppSKey),
		NwkSKey:  fmt.Sprintf("%X", device.NwkSKey),
		FCntDown: device.FCntDown,
	}

	err = g.searchAndRemove(g.dataFile, device.DevEui)
	if err != nil {
		return nil, fmt.Errorf("failed to remove device info from file: %w", err)
	}

	err = g.addDeviceInfoToFile(g.dataFile, deviceInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to add device info to file: %w", err)
	}

	return payload, nil
}
