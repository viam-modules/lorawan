package gateway

/*
#cgo CFLAGS: -I./sx1302/libloragw/inc -I./sx1302/libtools/inc  -I./sx1302/packet_forwarder/inc
#cgo LDFLAGS: -L./sx1302/libloragw -lloragw -L./sx1302libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include <stdlib.h>
#include <stdint.h>
*/
import "C"

func gatewayConfigtoC(conf gatewayConfig) C.struct_lgw_conf_board_s {
	cConfig := C.struct_lgw_conf_board_s{
		lorawan_public: C.bool(true),
		clksrc:         C.uint8_t(conf.clkSrc),
		full_duplex:    C.bool(false),       // Default value
		com_type:       C.lgw_com_type_t(0), //spi
	}

	for i, b := range conf.SPIPath {
		cConfig.com_path[i] = C.char(b)
		if b == 0 { // Stop at the null terminator
			break
		}
	}
	return cConfig
}

func rfChainConfigtoC(conf rfChainConfig) C.struct_lgw_conf_rxrf_s {
	cConfig := C.struct_lgw_conf_rxrf_s{
		enable:      C.bool(true),
		freq_hz:     C.uint32_t(conf.freqHz),
		rssi_offset: C.float(conf.rssiOffset),
		radio_type:  C.lgw_radio_type_t(conf.radioType),
		tx_enable:   C.bool(true),
		// not doing tcomp rn
	}

	return cConfig
}

func ifConfigtoC(conf ifChainConfig) C.struct_lgw_conf_rxif_s {
	cConfig := C.struct_lgw_conf_rxif_s{
		enable:    C.bool(true),
		rf_chain:  C.uint8_t(conf.rfChain),
		freq_hz:   C.int32_t(conf.freqHz),
		bandwidth: C.uint8_t(conf.bandwidth),
		datarate:  C.uint32_t(conf.datarate),
	}

	return cConfig
}
