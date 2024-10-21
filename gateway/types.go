package gateway

type gatewayConfig struct {
	publicLora bool
	clkSrc     int // which rf chain supplies the clock
	fullDuplex bool
	SPIPath    string
}

type radioType int

const (
	RadioTypeNone   radioType = iota // 0
	RadioTypeSX1255                  // 1
	RadioTypeSX1257                  // 2
	RadioTypeSX1272                  // 3
	RadioTypeSX1276                  // 4
	RadioTypeSX1250                  // 5
)

// Defines the config for a radio frequency chain.
type rfChainConfig struct {
	enable     bool
	freqHz     int
	rssiOffset float32
	enableTX   bool
	radioType  radioType
}

// Defines the config for the intermediate frequency chain.
// This defines the 8 channels and what frequencies they are able to recieve relative to the main frequency.
type ifChainConfig struct {
	enable    bool
	rfChain   int
	freqHz    int // This is the frequency relative to the main freq ie -200 will rx at main freq-200hz
	bandwidth int
	datarate  int // datarate is really spreading factor in the sx1302 HAL.
}

// This defines what RF chain to use for each of the 9 if chains
var RFChains = [9]int{0, 0, 0, 0, 1, 1, 1, 0}

// the If chain frequencies allow the gateway to read from devices with
// subtracting main frequenecy - intermediate frequency
var IfFrequncies = []int{
	-400000,
	-200000,
	0,
	200000,
	400000,
	-400000,
	-200000,
	0,
	300000, /* lora service */
}
