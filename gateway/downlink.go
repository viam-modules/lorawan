package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	lorahw "github.com/viam-modules/gateway/hal"
	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
	"go.viam.com/utils"
)

const (
	joinDelay      = 6         // rx2 delay in seconds for sending join accept message.
	downlinkDelay  = 2         // rx2 delay in seconds for downlink messages.
	rx2FrequencyUS = 923300000 // rx2 default freq for US
	rx2FrequencyEU = 869525000 // rx2 default freq for EU region
	rx2SF          = 12        // spreading factor for rx2 window, used for both US and EU
	rx2BandwidthUS = 0x06      // 500k bandwidth for US downlinks
	rx2BandwidthEU = 0x04      // 125k bandwidth for EU downlinks
	// command identifiers of supported mac commands.
	deviceTimeCID = 0x0D
	linkCheckCID  = 0x02
)

func (g *gateway) sendDownlink(ctx context.Context, payload []byte, isJoinAccept bool, packetTime time.Time) error {
	if len(payload) > 255 {
		return fmt.Errorf("error sending downlink, payload size is %d bytes, max size is 255 bytes", len(payload))
	}

	txPkt := &lorahw.TxPacket{
		Freq:      uint32(g.regionInfo.rx2Freq),
		Power:     26,
		DataRate:  rx2SF,
		Bandwidth: uint8(g.regionInfo.rx2Bandwidth),
		Size:      uint(len(payload)),
		Payload:   payload,
	}

	// 47709/32*time.Microsecond is the internal delay of sending a packet
	waitDuration := (downlinkDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	if isJoinAccept {
		waitDuration = (joinDelay * time.Second) - (time.Since(packetTime)) - 47709/32*time.Microsecond
	}

	if !accurateSleep(ctx, waitDuration) {
		return fmt.Errorf("error sending downlink: %w", ctx.Err())
	}

	err := lorahw.SendPacket(ctx, txPkt)
	if err != nil {
		return err
	}

	return nil
}

// According to lorawan docs, downlinks have a +/- 20 us error window, so regular sleep would not be accurate enough.
func accurateSleep(ctx context.Context, duration time.Duration) bool {
	// If we use utils.SelectContextOrWait(), we will wake up sometime after when we're supposed
	// to, which can be hundreds of microseconds later (because the process scheduler in the OS only
	// schedules things every millisecond or two). For use cases like a web server responding to a
	// query, that's fine. but when outputting a PWM signal, hundreds of microseconds can be a big
	// deal. To avoid this, we sleep for less time than we're supposed to, and then busy-wait until
	// the right time. Inspiration for this approach was taken from
	// https://blog.bearcats.nl/accurate-sleep-function/
	// On a raspberry pi 4, naively calling utils.SelectContextOrWait tended to have an error of
	// about 140-300 microseconds, while this version had an error of 0.3-0.6 microseconds.
	startTime := time.Now()
	maxBusyWaitTime := 1500 * time.Microsecond
	if duration > maxBusyWaitTime {
		shorterDuration := duration - maxBusyWaitTime
		if !utils.SelectContextOrWait(ctx, shorterDuration) {
			return false
		}
	}

	for time.Since(startTime) < duration {
		if err := ctx.Err(); err != nil {
			return false
		}
		// Otherwise, busy-wait some more
	}
	return true
}

// Downlink payload structure
// | MHDR | DEV ADDR | FCTRL | FCNTDOWN |  FOPTS (optional)  |  FPORT | encrypted frame payload  |  MIC |
// | 1 B  |   4 B    |  1 B  |    2 B   |       variable     |   1 B  |      variable            | 4 B  |
func (g *gateway) createDownlink(device *node.Node, framePayload, uplinkFopts []byte, sendAck bool, snr float64, sf int) (
	[]byte, error,
) {
	payload := make([]byte, 0)

	// Mhdr unconfirmed data down
	payload = append(payload, unconfirmedDownLinkMHdr)

	devAddrLE := reverseByteArray(device.Addr)

	payload = append(payload, devAddrLE...)

	fopts := make([]byte, 0)

	// Handle any mac commands sent in the uplink fopts.
	if len(uplinkFopts) != 0 {
		for _, b := range uplinkFopts {
			switch b {
			case deviceTimeCID:
				g.logger.Debugf("got device time request from %s", device.NodeName)
				deviceTimeAns, err := createDeviceTimeAns()
				if err != nil {
					g.logger.Errorf("failed to create device time answer: %w", err)
				}
				fopts = append(fopts, deviceTimeAns...)
			case linkCheckCID:
				g.logger.Debugf("got link check request from %s", device.NodeName)
				linkCheckAns := createLinkCheckAns(snr, sf)
				fopts = append(fopts, linkCheckAns...)
			default:
				// unknown mac command - this shouldn't happen since we remove these in parseDataUplink.
			}
		}
	}

	//  FCtrl: ADR (1), RFU (0), ACK(0/1), FPending (0), FOptsLen (0000)
	fctrl := byte(0x80)
	if sendAck {
		fctrl = 0xA0
	}

	// append 4 bit foptsLength to first 4 bits of fctrl.
	fOptsLength := len(fopts) & 0x0F
	fctrl |= byte(fOptsLength)
	payload = append(payload, fctrl)

	fCntBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(fCntBytes, uint16(device.FCntDown)+1)
	payload = append(payload, fCntBytes...)

	payload = append(payload, fopts...)

	// If there is a framePayload to send, add it to the downlink.
	if framePayload != nil {
		if device.FPort == 0 {
			return nil, errors.New("invalid downlink fport, ensure fport attribute is correctly set in the node config")
		}
		payload = append(payload, device.FPort)
		encrypted, err := crypto.EncryptDownlink(
			types.AES128Key(device.AppSKey), *types.MustDevAddr(device.Addr), device.FCntDown+1, framePayload)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encrypted...)
	}

	mic, err := crypto.ComputeLegacyDownlinkMIC(
		types.AES128Key(device.NwkSKey), *types.MustDevAddr(device.Addr), device.FCntDown+1, payload)
	if err != nil {
		return nil, err
	}

	payload = append(payload, mic[:]...)

	// increment fCntDown
	device.FCntDown++

	// create new deviceInfo to update the fcntDown in the file.
	deviceInfo := deviceInfo{
		DevEUI:   fmt.Sprintf("%X", device.DevEui),
		DevAddr:  fmt.Sprintf("%X", device.Addr),
		AppSKey:  fmt.Sprintf("%X", device.AppSKey),
		NwkSKey:  fmt.Sprintf("%X", device.NwkSKey),
		FCntDown: &device.FCntDown,
	}

	if err = g.searchAndRemove(g.dataFile, device.DevEui); err != nil {
		return nil, fmt.Errorf("failed to remove device info from file: %w", err)
	}
	if err = g.addDeviceInfoToFile(g.dataFile, deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to add device info to file: %w", err)
	}

	return payload, nil
}

// Helper function to calculate the downlink freq to be used for RX1.
// Not currently being used
func findDownLinkFreq(uplinkFreq int) int {
	// channel number between 0-64
	upLinkFreqNum := (uplinkFreq - 902300000) / 200000
	downLinkChan := upLinkFreqNum % 8
	downLinkFreq := downLinkChan*600000 + 923300000
	return downLinkFreq
}

func createDeviceTimeAns() ([]byte, error) {
	// Create buffer for the complete PHYPayload
	payload := make([]byte, 0)

	// add command identifier
	payload = append(payload, deviceTimeCID)

	// Create frame payload
	// Time is represented as seconds since GPS epoch
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	secondsSinceGPSEpoch := uint32(now.Sub(gpsEpoch).Seconds())

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, secondsSinceGPSEpoch); err != nil {
		return nil, err
	}

	payload = append(payload, buf.Bytes()...)

	// using zero for fractional seconds to match chirpstack's behavior.
	payload = append(payload, 0)

	return payload, nil
}

func createLinkCheckAns(snr float64, sf int) []byte {
	payload := make([]byte, 0)
	payload = append(payload, linkCheckCID)

	// calculate margin value
	minSNR := sfToSNRMin[sf]

	// margin represents the margin above which the last uplink from the demodulation floor.
	margin := snr - minSNR
	gwCnt := 1

	payload = append(payload, byte(margin))
	payload = append(payload, byte(gwCnt))

	return payload
}
