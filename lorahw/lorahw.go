// Package lorahw interacts directly with the sx1302 LoRa concentrator HAL library
package lorahw

/*
#cgo CFLAGS: -I${SRCDIR}/../sx1302/libloragw/inc -I${SRCDIR}/../sx1302/libtools/inc
#cgo LDFLAGS: -L${SRCDIR}/../sx1302/libloragw -lloragw -L${SRCDIR}/../sx1302/libtools -lbase64 -lparson -ltinymt32 -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "./gateway.h"
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/viam-modules/gateway/regions"
)

// Error variables for gateway setup errors
var (
	errBoardConfig            = errors.New("error setting the board config")
	errRadio0Config           = errors.New("error setting the radio frequency config for radio 0")
	errRadio1Config           = errors.New("error setting the radio frequency config for radio 1")
	errIntermediateFreqConfig = errors.New("error setting the intermediate frequency chain config")
	errLoraStdChannel         = errors.New("error configuring the lora STD channel")
	errTxGainSettings         = errors.New("error configuring the tx gain settings")
	errGatewayStart           = errors.New("error starting the gateway")
)

// SendPacket sends a lora packet using the sx1302 concentrator
func SendPacket(ctx context.Context, pkt *TxPacket) error {
	if pkt == nil {
		return errors.New("packet cannot be nil")
	}

	// Convert Go packet to C packet
	var cPkt C.struct_lgw_pkt_tx_s
	cPkt.freq_hz = C.uint32_t(pkt.Freq)
	// tx_mode 0 is immediate, 1 for timestampted with count_us delay
	// doing immediate mode with sleep to exit on context cancelation.
	cPkt.tx_mode = C.uint8_t(0)
	cPkt.rf_chain = C.uint8_t(0)
	cPkt.rf_power = C.int8_t(26)      // in dbm
	cPkt.modulation = C.uint8_t(0x10) // LoRa moduleation
	cPkt.datarate = C.uint32_t(pkt.DataRate)
	cPkt.bandwidth = C.uint8_t(pkt.Bandwidth)
	cPkt.coderate = C.uint8_t(0x01) // code rate 4/5
	cPkt.invert_pol = C.bool(true)  // Downlinks are always reverse polarity.
	cPkt.no_crc = C.bool(true)      // CRCs in uplinks only.
	cPkt.no_header = C.bool(false)
	cPkt.size = C.uint16_t(pkt.Size)

	// Copy payload
	if len(pkt.Payload) > 0 {
		for i, b := range pkt.Payload {
			cPkt.payload[i] = C.uint8_t(b)
		}
	}

	if errCode := C.send(&cPkt); errCode != 0 {
		return fmt.Errorf("failed to send packet: %d", errCode)
	}

	// wait for packet to finish sending.
	var status C.uint8_t
	for {
		C.get_status(cPkt.rf_chain, &status)
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

// TxPacket represents a packet to be transmitted
type TxPacket struct {
	Freq      uint32
	DataRate  uint8
	Bandwidth uint8
	Size      uint
	Payload   []byte
}

// MaxRxPackets is the maximum number of packets that can be received in one call
var MaxRxPackets = int(C.MAX_RX_PKT)

// RxPacket represents a received LoRa packet
type RxPacket struct {
	Size     uint
	Payload  []byte
	SNR      float64
	DataRate int
	Freq     int
}

// SetupGateway initializes the gateway hardware
func SetupGateway(comType int, path string, region regions.Region, baseChannel int) error {
	errCode := C.set_up_gateway(C.int(comType), C.CString(path), C.int(region), C.int(baseChannel))
	if errCode != 0 {
		return fmt.Errorf("failed to set up gateway: %w", parseErrorCode(int(errCode)))
	}
	return nil
}

// StopGateway stops the gateway hardware
func StopGateway() error {
	errCode := C.stop_gateway()
	if errCode != 0 {
		return errors.New("error stopping gateway")
	}
	return nil
}

// ReceivePackets receives packets from the gateway and converts them to go []RxPacket array.
func ReceivePackets() ([]RxPacket, error) {
	p := C.create_rx_packet_array()
	if p == nil {
		return nil, errors.New("failed to create rx packet array")
	}
	defer C.free(unsafe.Pointer(p))

	numPackets := int(C.receive(p))
	if numPackets == -1 {
		return nil, errors.New("error receiving packets")
	}
	if numPackets == 0 {
		return nil, nil
	}

	// Convert C packets to Go packets
	packets := unsafe.Slice((*C.struct_lgw_pkt_rx_s)(unsafe.Pointer(p)), MaxRxPackets)
	var result []RxPacket

	for i := range numPackets {
		if packets[i].size == 0 {
			continue
		}

		packet := RxPacket{
			Size:     uint(packets[i].size),
			SNR:      float64(packets[i].snr),
			DataRate: int(packets[i].datarate),
		}

		// Convert payload
		packet.Payload = make([]byte, packet.Size)
		for j := range packet.Size {
			packet.Payload[j] = byte(packets[i].payload[j])
		}

		result = append(result, packet)
	}

	return result, nil
}

// DisableBuffering disables buffering on C stdout
func DisableBuffering() {
	C.disable_buffering()
}

func parseErrorCode(errCode int) error {
	switch errCode {
	case 2:
		return errBoardConfig
	case 3:
		return errRadio0Config
	case 4:
		return errRadio1Config
	case 5:
		return errIntermediateFreqConfig
	case 6:
		return errLoraStdChannel
	case 7:
		return errTxGainSettings
	case 8:
		return errGatewayStart
	default:
		return errors.New("unknown error")
	}
}
