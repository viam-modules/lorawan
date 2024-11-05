#include "../sx1302/libloragw/inc/loragw_hal.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>

#define MAX_RX_PKT 8

#define RADIO_0_FREQ     902700000
#define RADIO_1_FREQ     903700000
#define RX2_FREQ  923300000
#define RX2_SF  12
#define RX2_BANDWIDTH 0x06


// the If chain frequencies allow the gateway to read on multiple frequency channels.
// subtracting main frequenecy - intermediate frequency
const int32_t ifFrequencies[9] = {
	-400000,
	-200000,
	0,
	200000,
	400000,
	-400000,
	-200000,
	0,
	300000, // lora service channel
};

// This defines what RF chain to use for each of the 9 if chains
const int32_t rfChains [9] = {0, 0, 0, 0, 1, 1, 1, 0};


int setUpGateway(int bus) {
    struct lgw_conf_board_s boardconf;
    struct lgw_conf_rxrf_s rfconf;
    struct lgw_conf_rxif_s ifconf;

    memset( &boardconf, 0, sizeof boardconf);
    boardconf.lorawan_public = true;
    boardconf.clksrc = 0;
    boardconf.full_duplex = false;
    boardconf.com_type =  LGW_COM_SPI; // spi

    const char * com_path;
    if (bus == 1) {
        com_path = "/dev/spidev0.1";
    } else {
        com_path = "/dev/spidev0.0";
    }

    strncpy(boardconf.com_path, com_path, sizeof boardconf.com_path);
    boardconf.com_path[sizeof boardconf.com_path - 1] = '\0'; /* ensure string termination */
    if (lgw_board_setconf(&boardconf) != LGW_HAL_SUCCESS) {
        return EXIT_FAILURE;
    }

  // set configuration for RF chains
    memset( &rfconf, 0, sizeof rfconf);
    rfconf.enable = true;
    rfconf.freq_hz = RADIO_0_FREQ;
    rfconf.radio_type = LGW_RADIO_TYPE_SX1250;
    rfconf.rssi_offset = -215;
    rfconf.tx_enable = true;

    if (lgw_rxrf_setconf(0, &rfconf) != LGW_HAL_SUCCESS) {
        return EXIT_FAILURE;
    }

    rfconf.freq_hz = RADIO_1_FREQ;
    if (lgw_rxrf_setconf(1, &rfconf) != LGW_HAL_SUCCESS) {
        return EXIT_FAILURE;

    }


    memset(&ifconf, 0, sizeof(ifconf));
    ifconf.enable = true;
    ifconf.datarate = DR_LORA_SF7;
    ifconf.bandwidth = 0x04; //125k
    for (int i = 0; i < 8; i++) {
        ifconf.rf_chain = rfChains[i];
        ifconf.freq_hz = ifFrequencies[i];
        if (lgw_rxif_setconf(i, &ifconf) != LGW_HAL_SUCCESS) {
            return EXIT_FAILURE;
        }
    }

    if (lgw_start() != LGW_HAL_SUCCESS) {
            return EXIT_FAILURE;
        }
 }

int stopGateway() {
    return lgw_stop();
}

struct lgw_pkt_rx_s* createRxPacketArray() {
    return (struct lgw_pkt_rx_s*)malloc(sizeof(struct lgw_pkt_rx_s) * MAX_RX_PKT);
}

int receive(struct lgw_pkt_rx_s* packet)  {
    return lgw_receive(1, packet);
}

int send(struct lgw_pkt_tx_s* packet) {
    return lgw_send(packet);
}

