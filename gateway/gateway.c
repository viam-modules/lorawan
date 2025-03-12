#include "../sx1302/libloragw/inc/loragw_hal.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>

#define RADIO_0_FREQ     902700000
#define RADIO_1_FREQ     903700000
int MAX_RX_PKT = 10;

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
        return 1;
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
    rfconf.rssi_offset = -215.4;
    rfconf.tx_enable = true;
    struct lgw_rssi_tcomp_s tcomp;
    tcomp.coeff_a = 0;
    tcomp.coeff_b = 0;
    tcomp.coeff_c = 21.41;
    tcomp.coeff_d = 2162.56;
    tcomp.coeff_e = 0;
    rfconf.rssi_tcomp = tcomp;

    if (lgw_rxrf_setconf(0, &rfconf) != LGW_HAL_SUCCESS) {
        return 2;
    }

    rfconf.freq_hz = RADIO_1_FREQ;
    if (lgw_rxrf_setconf(1, &rfconf) != LGW_HAL_SUCCESS) {
        return 3;

    }

    // set config for intermediate frequency chains to listen for downlink messages.
    // the if (intermediate frequency chain) is used to listen to different frequency channels within the band.
    // the freq_hz field should be set as the difference from the main frequency ie if the rf chain is set to 902.7MHz,
    // to get an if chain for frequency 902.5MHz, set freq_hz to -2 MHz.
    struct lgw_conf_rxif_s ifconf;
    memset(&ifconf, 0, sizeof(ifconf));
    ifconf.enable = true;
    for (int i = 0; i < 8; i++) {
        ifconf.rf_chain = rfChains[i];
        ifconf.freq_hz = ifFrequencies[i];
        if (lgw_rxif_setconf(i, &ifconf) != LGW_HAL_SUCCESS) {
            return 4;
        }
    }


    // Configure lora std channel.
    ifconf.bandwidth = 0x06;
    ifconf.rf_chain = 0;
    ifconf.freq_hz = 300000;
    ifconf.datarate = DR_LORA_SF8;
    ifconf.implicit_coderate = 1;
    ifconf.implicit_crc_en = false;
    ifconf.implicit_payload_length = 17;
    ifconf.implicit_hdr = false;
    if (lgw_rxif_setconf(8, &ifconf) != LGW_HAL_SUCCESS) {
        return 5;
    }

    // the tx gain config contains transmission gain settings for downlinks.
    // Using the same values as basic station
    struct lgw_tx_gain_lut_s lut;
    struct lgw_tx_gain_s txGain[16];

    uint8_t paGain [16] =  {0,0,0,0,0,0,1,1,1,1,1,1,1,1,1};
    int8_t rf_power [16] = {12, 13,14,15,16,17,18,19,20,21,22,23,24,25,26,27};
    uint8_t pwr_idx [16] = {15,16,17,19,20,22,1,2,3,4,5,6,7,9,11,14};

    for(int i = 0; i<16; i++) {
        txGain[i].pa_gain = paGain[i];
        txGain[i].rf_power = rf_power[i];
        txGain[i].pwr_idx = pwr_idx[i];
        txGain[i].dig_gain = 0;
        txGain[i].dac_gain = 3;
        txGain[i].mix_gain = 5;
    }

    memcpy(lut.lut, txGain, sizeof(txGain));
    lut.size = 16;

    if(lgw_txgain_setconf(0, &lut) != LGW_HAL_SUCCESS) {
        return 6;
    }

    // start the gateway.
    int res = lgw_start();
    if (res != LGW_HAL_SUCCESS) {
        return 7;
    }
    return 0;
 }

int stopGateway() {
    return lgw_stop();
}

struct lgw_pkt_rx_s* createRxPacketArray() {
    return (struct lgw_pkt_rx_s*)malloc(sizeof(struct lgw_pkt_rx_s) * MAX_RX_PKT);
}

int receive(struct lgw_pkt_rx_s* packet)  {
    return lgw_receive(MAX_RX_PKT, packet);
}

int send(struct lgw_pkt_tx_s* packet) {
    return lgw_send(packet);
}

void disableBuffering() {
    setbuf(stdout, NULL);
}

#ifdef TESTING
void redirectToPipe(int fd) {
    // Mock implementation for testing - does nothing
}

#else
void redirectToPipe(int fd) {
    fflush(stdout);          // Flush anything in the current stdout buffer
    dup2(fd, STDOUT_FILENO); // Redirect stdout to the pipe's file descriptor
}
#endif

