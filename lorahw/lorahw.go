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
	"fmt"
	"time"
	"unsafe"
)

// Region represents the frequency band region
type Region int

const (
	// unspecified represents an unspecified region
	Unspecified Region = iota
	// US represents the US915 frequency band
	US
	// EU represents the EU868 frequency band
	EU
)

func SendPacket(ctx context.Context, pkt *TxPacket) error {
	if pkt == nil {
		return fmt.Errorf("packet cannot be nil")
	}

	// Convert Go packet to C packet
	var cPkt C.struct_lgw_pkt_tx_s
	cPkt.freq_hz = C.uint32_t(pkt.Freq)
	cPkt.tx_mode = C.uint8_t(0)
	cPkt.rf_chain = C.uint8_t(0)
	cPkt.rf_power = C.int8_t(pkt.Power)
	cPkt.modulation = C.uint8_t(0x10)
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
		C.lgw_status(cPkt.rf_chain, 1, &status)
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
	Power     int8
	DataRate  uint8
	Bandwidth uint8
	Size      uint
	Payload   []byte
}

// MaxRxPackets is the maximum number of packets that can be received in one call
var MaxRxPackets = int(C.MAX_RX_PKT)

// Packet represents a received LoRa packet
type RxPacket struct {
	Size     uint
	Payload  []byte
	SNR      float32
	DataRate uint8
}

// SetupGateway initializes the gateway hardware
func SetupGateway(spiBus int, region Region) error {
	errCode := C.setUpGateway(C.int(spiBus), C.int(region))
	if errCode != 0 {
		return fmt.Errorf("failed to set up gateway: %s", parseErrorCode(int(errCode)))
	}
	return nil
}

// StopGateway stops the gateway hardware
func StopGateway() error {
	errCode := C.stopGateway()
	if errCode != 0 {
		return fmt.Errorf("error stopping gateway")
	}
	return nil
}

// ReceivePackets receives packets from the gateway
func ReceivePackets() ([]RxPacket, error) {
	p := C.createRxPacketArray()
	if p == nil {
		return nil, fmt.Errorf("failed to create receive packet array")
	}
	defer C.free(unsafe.Pointer(p))

	numPackets := int(C.receive(p))
	if numPackets == -1 {
		return nil, fmt.Errorf("error receiving packets")
	}
	if numPackets == 0 {
		return nil, nil
	}

	// Convert C packets to Go packets
	packets := unsafe.Slice((*C.struct_lgw_pkt_rx_s)(unsafe.Pointer(p)), MaxRxPackets)
	var result []RxPacket

	for i := 0; i < numPackets; i++ {
		if packets[i].size == 0 {
			continue
		}

		packet := RxPacket{
			Size:     uint(packets[i].size),
			SNR:      float32(packets[i].snr),
			DataRate: uint8(packets[i].datarate),
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

// RedirectLogsToPipe redirects C logs to a pipe
func RedirectLogsToPipe(fd uintptr) {
	C.redirectToPipe(C.int(fd))
}

// DisableBuffering disables buffering on C stdout
func DisableBuffering() {
	C.disableBuffering()
}

func parseErrorCode(errCode int) string {
	switch errCode {
	case 1:
		return "error setting the board config"
	case 2:
		return "error setting the radio frequency config for radio 0"
	case 3:
		return "error setting the radio frequency config for radio 1"
	case 4:
		return "error setting the intermediate frequency chain config"
	case 5:
		return "error configuring the lora STD channel"
	case 6:
		return "error configuring the tx gain settings"
	case 7:
		return "error starting the gateway"
	default:
		return "unknown error"
	}
}
