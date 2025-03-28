#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>

const int MAX_RX_PKT = 10;

#define US_RADIO_0_FREQ     902700000
#define US_RADIO_1_FREQ     903700000

#define EU_RADIO_0_FREQ     867500000
#define EU_RADIO_1_FREQ     868500000

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
const int32_t rfChains [9] = {0, 0, 0, 0, 0, 1, 1, 1};

int set_up_gateway(int bus, int region) {
    // the board config defines parameters for the entire gateway HAT.
    struct lgw_conf_board_s boardconf;

    memset(&boardconf, 0, sizeof boardconf);
    boardconf.lorawan_public = true;
    boardconf.clksrc = 0;
    boardconf.full_duplex = false;
    boardconf.com_type = LGW_COM_SPI;

    const char * com_path;

    switch(bus) {
        case 0:
            com_path = "/dev/spidev0.0";
            break;
        case 1:
            com_path = "/dev/spidev0.1";
            break;
        default:
            //error invalid spi bus
            return 1;
    }

    strncpy(boardconf.com_path, com_path, sizeof boardconf.com_path);
    boardconf.com_path[sizeof boardconf.com_path - 1] = '\0';
    if (lgw_board_setconf(&boardconf) != LGW_HAL_SUCCESS) {
        return 2;
    }

    int radio0_freq;
    int radio1_freq;
    switch(region) {
        case 2:
            radio0_freq = EU_RADIO_0_FREQ;
            radio1_freq = EU_RADIO_1_FREQ;
            break;
        default:
            radio0_freq = US_RADIO_0_FREQ;
            radio1_freq = US_RADIO_1_FREQ;
            break;
    }

    // set configuration for RF (radio frequency) chains on the gateway.
    // The rfConf configures the two RF chains the gateway HAT has.
    struct lgw_conf_rxrf_s rfconf;

    memset(&rfconf, 0, sizeof rfconf);
    rfconf.enable = true;
    rfconf.freq_hz = radio0_freq;
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
        return 3;
    }

    rfconf.freq_hz = radio1_freq;
    if (lgw_rxrf_setconf(1, &rfconf) != LGW_HAL_SUCCESS) {
        return 4;
    }

    // set config for intermediate frequency chains to listen for uplink messages.
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
            return 5;
        }
    }

    // Configure lora std channel
    ifconf.bandwidth = 0x06;
    ifconf.rf_chain = 0;
    ifconf.freq_hz = 300000;
    ifconf.datarate = DR_LORA_SF8;
    ifconf.implicit_coderate = 1;
    ifconf.implicit_crc_en = false;
    ifconf.implicit_payload_length = 17;
    ifconf.implicit_hdr = false;
    if (lgw_rxif_setconf(8, &ifconf) != LGW_HAL_SUCCESS) {
        return 6;
    }

    // Configure TX gain settings
    // Using the same values as basic station
    struct lgw_tx_gain_lut_s lut;
    struct lgw_tx_gain_s txGain[16];

    // power amplifier gain, 0 means low power gain, 1 mean high power gain.
    uint8_t paGain [16] =  {0,0,0,0,0,0,1,1,1,1,1,1,1,1,1};
    // rf power in dbm - represents all power levels the device can transmit at.
    int8_t rf_power [16] = {12, 13,14,15,16,17,18,19,20,21,22,23,24,25,26,27};
    // maps the rf power levels to sx1302 chip specific power control registers.
    uint8_t pwr_idx [16] = {15,16,17,19,20,22,1,2,3,4,5,6,7,9,11,14};

    // sx1302 supports 16 power levels
    for(int i = 0; i < 16; i++) {
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
        return 7;
    }

    // start the gateway.
    int res = start_gateway();
    if (res != LGW_HAL_SUCCESS) {
        return 8;
    }
    return 0;
}

int stop_gateway() {
    return lgw_stop();
}

struct lgw_pkt_rx_s* create_rx_packet_array() {
    return (struct lgw_pkt_rx_s*)malloc(sizeof(struct lgw_pkt_rx_s) * MAX_RX_PKT);
}

void disable_buffering() {
    setbuf(stdout, NULL);
}

#ifdef TESTING
void redirect_to_pipe(int fd) {
    // Mock implementation for testing - does nothing
}

int send(struct lgw_pkt_tx_s* packet) {
    // for testing, return 0 - sucesssful
    return 0;
}

int start_gateway() {
    // for testing, return no error
    return 0;
}

int receive(struct lgw_pkt_rx_s* packet) {
    // testing - fill the packet with test values
    packet->size = 3;
    uint8_t payload[3] = {0x01, 0x02, 0x03};
    memcpy(packet->payload, payload, packet->size);
    packet->datarate = 7;
    packet->snr = 20;

    return 1; // 1 packet received
}

int get_status(uint8_t rf, uint8_t* status) {
    // status 2 = sucessful for mock testing
    *status = 2;
    return 0;
}

#else
void redirect_to_pipe(int fd) {
    fflush(stdout);          // Flush anything in the current stdout buffer
    dup2(fd, STDOUT_FILENO); // Redirect stdout to the pipe's file descriptor
}

int send(struct lgw_pkt_tx_s* packet) {
    return lgw_send(packet);
}

int start_gateway() {
    return lgw_start();
}

int receive(struct lgw_pkt_rx_s* packet) {
    return lgw_receive(MAX_RX_PKT, packet);
}

int get_status(uint8_t rf, uint8_t* status) {
    return lgw_status(rf, 1, status);
}


#endif
