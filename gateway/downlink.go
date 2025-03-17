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

const (
	joinDelay     = 6         // rx2 delay in seconds for sending join accept message.
	downlinkDelay = 2         // rx2 delay in seconds for downlink messages.
	rx2Frequency  = 923300000 // Frequency to send downlinks on rx2 window, lorawan rx2 default
	rx2SF         = 12        // spreading factor for rx2 window, default for lorawan
	rx2Bandwidth  = 0x06      // 500k bandwidth, default bandwidth for downlinks
)

func (g *gateway) sendDownlink(ctx context.Context, payload []byte, isJoinAccept bool, packetTime time.Time) error {
	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:     C.uint32_t(rx2Frequency),
		freq_offset: C.int8_t(0),
		// tx_mode 0 is immediate, 1 for timestampted with count_us delay
		// doing immediate mode with sleep to exit on context cancelation.
		tx_mode:    C.uint8_t(0),
		rf_chain:   C.uint8_t(0),
		rf_power:   C.int8_t(26),            // tx power in dbm
		modulation: C.uint8_t(0x10),         // LORA modulation
		bandwidth:  C.uint8_t(rx2Bandwidth), // 500k
		datarate:   C.uint32_t(rx2SF),
		coderate:   C.uint8_t(0x01), // code rate 4/5
		invert_pol: C.bool(true),    // Downlinks are always reverse polarity.
		size:       C.uint16_t(len(payload)),
		preamble:   C.uint16_t(8),
		no_crc:     C.bool(true), // CRCs in uplinks only
		no_header:  C.bool(false),
	}

	var cPayload [256]C.uchar
	for i, b := range payload {
		cPayload[i] = C.uchar(b)
	}
	txPkt.payload = cPayload

	// 47709/32*time.Microsecond is the internal delay of sending a packet
	waitDuration := (downlinkDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	if isJoinAccept {
		waitDuration = (joinDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	}

	if !accurateSleep(ctx, waitDuration) {
		return fmt.Errorf("error sending downlink: %w", ctx.Err())
	}

	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink packet")
	}
	// wait for packet to finish sending.
	var status C.uint8_t
	for {
		C.lgw_status(txPkt.rf_chain, 1, &status)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("error sending downlink: %w", ctx.Err())
		}
		// status of 2 indicates send was successful
		if int(status) == 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	return nil
}

// According to lorawan docs, downlinks have a +/- 20 us error window, so regular sleep would not be accurate enough.
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
func (g *gateway) createDownlink(device *node.Node, framePayload []byte, sendAck bool) ([]byte, error) {
	payload := make([]byte, 0)

	// Mhdr unconfirmed data down
	payload = append(payload, unconfirmedDownLinkMHdr)

	devAddrLE := reverseByteArray(device.Addr)

	payload = append(payload, devAddrLE...)

	//  FCtrl: ADR (0), RFU (0), ACK(0/1), FPending (0), FOptsLen (0000)
	fctrl := 0x00
	if sendAck {
		fctrl = 0x20
	}
	payload = append(payload, byte(fctrl))

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, uint16(device.FCntDown)+1)
	payload = append(payload, fCntBytes...)

	// TODO (om) commented for future testing
	// fopts are used for the MAC commands
	// fopts := []byte{
	// 	0b00111001, // data rate and tx power
	// 	0xFF,
	// 	0x00,
	// 	0x01,
	// }

	// fopts := []byte{0x3A, 0xFF, 0x00, 0x01}

	// payload = append(payload, fopts...)

	if framePayload != nil {
		if device.FPort == 0 {
			return nil, errors.New("invalid downlink fport, ensure fport attribute is correctly set in the node config")
		}
		payload = append(payload, device.FPort)
		encrypted, err := crypto.EncryptDownlink(
			types.AES128Key(device.AppSKey), *types.MustDevAddr(device.Addr), device.FCntDown+1, framePayload)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encrypted...)
	}

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

	if err = g.searchAndRemove(g.dataFile, device.DevEui); err != nil {
		return nil, fmt.Errorf("failed to remove device info from file: %w", err)
	}
	if err = g.addDeviceInfoToFile(g.dataFile, deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to add device info to file: %w", err)
	}

	return payload, nil
}

// Helper function to calculate the downlink freq to be used for RX1.
// Not currently being used
func findDownLinkFreq(uplinkFreq int) int {
	// channel number between 0-64
	upLinkFreqNum := (uplinkFreq - 902300000) / 200000
	downLinkChan := upLinkFreqNum % 8
	downLinkFreq := downLinkChan*600000 + 923300000
	return downLinkFreq
}
