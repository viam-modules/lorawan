#pragma once

#include <stdint.h>
#include "../sx1302/libloragw/inc/loragw_hal.h"

extern const int MAX_RX_PKT;

struct lgw_pkt_rx_s* createRxPacketArray(void);
int receive(struct lgw_pkt_rx_s* packet);
int send(struct lgw_pkt_tx_s* packet);
int stopGateway(void);
int setUpGateway(int bus, int region);
void disableBuffering(void);
void redirectToPipe(int fd);
