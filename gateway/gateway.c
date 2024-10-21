#include "../sx1302_hal/libloragw/inc/loragw_hal.h"
#include <stdint.h>
#include <stdlib.h>

#define MAX_RX_PKT 8

int setBoardConfig(struct lgw_conf_board_s conf) {
    return lgw_board_setconf(&conf);
}

int setRFConfig(uint8_t rfchain, struct lgw_conf_rxrf_s conf) {
    	return lgw_rxrf_setconf(rfchain, &conf);
}

int setIFConfig(uint8_t ifChain, struct lgw_conf_rxif_s conf) {
    return lgw_rxif_setconf(ifChain, &conf);
}

int startGateway() {
    return lgw_start();
}

struct lgw_pkt_rx_s* create_rxpkt_array() {
    return (struct lgw_pkt_rx_s*)malloc(sizeof(struct lgw_pkt_rx_s) * MAX_RX_PKT);
}

int receive(struct lgw_pkt_rx_s* packet)  {
    return lgw_receive(1, packet);
}

