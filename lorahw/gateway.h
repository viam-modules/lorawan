#pragma once

#include <stdint.h>
#include "../sx1302/libloragw/inc/loragw_hal.h"

extern const int MAX_RX_PKT;

struct lgw_pkt_rx_s* create_rx_packet_array(void);
int receive(struct lgw_pkt_rx_s* packet);
int send(struct lgw_pkt_tx_s* packet);
int stop_gateway(void);
int start_gateway(void);
int set_up_gateway(int bus, int region);
int set_up_gateway2(int type, char* path, int region);
void disable_buffering(void);
void redirect_to_pipe(int fd);
int get_status(uint8_t rf, uint8_t* status);
