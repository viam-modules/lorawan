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


// the IF chain frequencies allow the gateway to read on multiple frequency channels.
// subtracting main frequenecy - intermediate frequency will give that channel's freq.
const int32_t ifFrequencies[9] = {
	-400000,
	-200000,
	0,
	200000,
	400000,
	-400000,
	-200000,
	0,
};

// This defines what RF chain to use for each of the 8 if chains
const int32_t rfChains [9] = {0, 0, 0, 0, 1, 1, 1};


int setUpGateway(int bus) {

    // the board config defines parameters for the entire gateway HAT.
    struct lgw_conf_board_s boardconf;

    memset( &boardconf, 0, sizeof boardconf);
    boardconf.lorawan_public = true;
    boardconf.clksrc = 0;
    boardconf.full_duplex = false;
    boardconf.com_type =  LGW_COM_SPI; // spi

    const char * com_path;

    switch(bus) {
        case 1:
            com_path = "/dev/spidev0.1";
        default:
            com_path = "/dev/spidev0.0";
    }

    strncpy(boardconf.com_path, com_path, sizeof boardconf.com_path);
    boardconf.com_path[sizeof boardconf.com_path - 1] = '\0';
    if (lgw_board_setconf(&boardconf) != LGW_HAL_SUCCESS) {
        return EXIT_FAILURE;
    }

    // The rfConf configures the two RF chains the gateway HAT has.
    struct lgw_conf_rxrf_s rfconf;

    // set configuration for RF (radio frequency) chains on the gateway.
    // There are two sx1250 radios on the gateway - these can be used to listen on two different frequency bands.
    // We are setting default frequencies for the RF chains to listen on US915 band at two different frequencies.
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

    // set config for intermediate frequency chains to listen for downlink messages.
    // the if (intermediate frequency chain) is used to listen to different frequency channels within the band.
    // the freq_hz field should be set as the difference from the main frequency ie if the rf chain is set to 902.7MHz,
    // to get an if chain for frequency 902.5MHz, set freq_hz to -2 MHz.
    struct lgw_conf_rxif_s ifconf;
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

    // start the gateway.
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

