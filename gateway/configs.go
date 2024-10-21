package gateway

/*
#include "gateway.h"
*/
import "C"
import "errors"

func setIfChainConfigs() error {
	var ifConfig ifChainConfig
	ifConfig.enable = true
	//TODO: make actual types for these/confgurable
	ifConfig.datarate = 7     // SF 7
	ifConfig.bandwidth = 0x04 // 125k

	for i := 0; i < 8; i++ {
		ifConfig.rfChain = RFChains[i]
		ifConfig.freqHz = IfFrequncies[i]
		cConfig := ifConfigtoC(ifConfig)
		s := int(C.setIFConfig(C.uint8_t(i), cConfig))
		if s != 0 {
			return errors.New("error setting rxif config")
		}
	}
	return nil
}

func setGatewayConfig() error {
	gatewayConf := gatewayConfig{
		publicLora: true,
		clkSrc:     0,
		fullDuplex: false,
		SPIPath:    "/dev/spidev0.0",
	}

	cConfig := gatewayConfigtoC(gatewayConf)

	err := int(C.setBoardConfig(cConfig))
	if err != 0 {
		return errors.New("error setting the gateway config")
	}
	return nil
}

func setRfChainConfig(rfchain int, freqHz int) error {
	rfConf := rfChainConfig{
		enable:     true,
		freqHz:     freqHz,
		rssiOffset: -215.4,
		enableTX:   true,
		radioType:  RadioTypeSX1250,
	}

	cConfig := rfChainConfigtoC(rfConf)

	err := int(C.setRFConfig(C.uint8_t(rfchain), cConfig))
	if err != 0 {
		return errors.New("error setting the rf chain config")
	}
	return nil
}
