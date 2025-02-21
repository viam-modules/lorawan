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

func (g *gateway) sendDownLink(ctx context.Context, payload []byte, join bool, uplinkFreq int, t time.Time) error {
	//dataRate := 7
	// freq := findDownLinkChannel(uplinkFreq)
	// g.logger.Infof("freq: %d", freq)
	freq := rx2Frequenecy
	if join {
		freq = rx2Frequenecy
		//	dataRate = rx2SF
	}

	txPkt := C.struct_lgw_pkt_tx_s{
		freq_hz:    C.uint32_t(freq),
		tx_mode:    C.uint8_t(0), // immediate
		rf_chain:   C.uint8_t(0),
		rf_power:   C.int8_t(26),            // tx power in dbm
		modulation: C.uint8_t(0x10),         // LORA modulation
		bandwidth:  C.uint8_t(rx2Bandwidth), //500k
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

	g.logger.Infof("time since: %v ", time.Since(t).Seconds())

	// join request and other downlinks have different windows for class A devices.
	var waitTime float64
	switch join {
	case true:
		waitTime = float64(joinRx2WindowSec*time.Second) - time.Since(t).Seconds()
	default:
		waitTime = float64(2*time.Second) - time.Since(t).Seconds()
		// waitTime = 1 // 1 for rx1
	}

	defer g.logger.Infof("time since: %v ", time.Since(t).Seconds())
	defer g.logger.Infof("time in accurate sleep: %v", time.Duration(waitTime))
	defer g.logger.Infof("wait time %v", waitTime)
	// if !utils.SelectContextOrWait(ctx, time.Second*time.Duration(waitTime)) {
	// 	return errors.New("context canceled")
	// }

	if !accurateSleep(ctx, time.Duration(waitTime)) {
		return errors.New("context canceled")
	}

	// lock the mutex to prevent two sends at the same time.
	g.mu.Lock()
	defer g.mu.Unlock()
	errCode := int(C.send(&txPkt))
	if errCode != 0 {
		return errors.New("failed to send downlink packet")
	}
	var status C.uint8_t
	g.logger.Info("here sending")
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

	// do {
	// 	wait_ms(5);
	// 	lgw_status(pkt.rf_chain, TX_STATUS, &tx_status); /* get TX status */
	// } while ((tx_status != TX_FREE) && (quit_sig != 1) && (exit_sig != 1));

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

var fCntDown uint16 = 0

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
	payload = append(payload, 0x80)

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
	payload = append(payload, fCntBytes...)

	// fopts := []byte{0x3A, 0xFF, 0x00, 0x01}

	// payload = append(payload, fopts...)

	// // Fport
	// Change to 0x00 for MAC-only downlink
	payload = append(payload, 85)

	// 30 sec
	//framePayload := []byte{0x01, 0x00, 0x00, 0x1E} //  dragino
	framePayload := []byte{0xff, 0x10, 0xff}

	devAddrBE := reverseByteArray(devAddr)
	encrypted, err := crypto.EncryptDownlink(appSKey, *types.MustDevAddr(devAddrBE), uint32(fCntDown), framePayload)
	if err != nil {
		return nil, err
	}

	// payload = payload[:len(payload)-len(framePayload)]
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
