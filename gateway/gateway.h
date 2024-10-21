#pragma once

#include <stdint.h>
#include "../sx1302/libloragw/inc/loragw_hal.h"

int setBoardConfig(struct lgw_conf_board_s conf);
int setRFConfig(uint8_t rfchain,struct lgw_conf_rxrf_s conf);
int setIFConfig(uint8_t ifChain, struct lgw_conf_rxif_s conf);
int startGateway();
struct lgw_pkt_rx_s* create_rxpkt_array();
int receive(struct lgw_pkt_rx_s* packet);
