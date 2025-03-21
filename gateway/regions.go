package gateway

import (
	"strings"
)

// struct to hold region specific gateway info.
type regionInfo struct {
	cfList       []byte
	dlSettings   byte
	rx2Freq      int
	rx2Bandwidth int
}

// regionInfoUS defines the region specific parameters for the US915 band.
var regionInfoUS = regionInfo{
	// Use data rate 8 for rx2 downlinks
	// DR8 = SF12 BW 500K
	// See lorawan1.0.3 regional specs doc for a table of data rates to SF/BW for each region.
	dlSettings: 0x08,
	// CFList for US915 using Channel Mask
	// This tells the device to only transmit on channels 0-7
	cfList: []byte{
		0xFF, // Enable channels 0-7
		0x00, // Disable channels 8-15
		0x00, // Disable channels 16-23
		0x00, // Disable channels 24-31
		0x00, // Disable channels 32-39
		0x00, // Disable channels 40-47
		0x00, // Disable channels 48-55
		0x00, // Disable channels 56-63
		0x01, // Enable channel 64, disable 65-71
		0x00, // Disable channels 72-79
		0x00, // RFU (reserved for future use)
		0x00, // RFU
		0x00, // RFU
		0x00, // RFU
		0x00, // RFU
		0x01, // CFList Type = 1 (Channel Mask)
	},
	rx2Bandwidth: rx2BandwidthUS,
	rx2Freq:      rx2FrequencyUS,
}

// regionInfoEU defines the region specific parameters for the EU868 band.
var regionInfoEU = regionInfo{
	// Use data rate 0 for rx2 downlinks
	// DR0 = SF12 BW 125K
	// See lorawan1.0.3 regional specs doc for a table of data rates to SF/BW for each region.
	dlSettings: 0x00,
	cfList: []byte{
		0xC4, 0xD2, 0x33, // 867.1 MHz
		0xD4, 0xD2, 0x33, // 867.3 MHz
		0xE4, 0xD2, 0x33, // 867.5 MHz
		0xF4, 0xD2, 0x33, // 867.7 MHz
		0x04, 0xD3, 0x33, // 867.9 MHz
		0x00, // CFList type (0x00 for frequency list)
	},
	rx2Bandwidth: rx2BandwidthEU,
	rx2Freq:      rx2FrequencyEU,
}

func getRegion(region string) region {
	region = strings.ToUpper(region)
	switch region {
	case "US", "US915", "915":
		return US
	case "EU", "EU868", "868":
		return EU
	default:
		return unspecified
	}
}
