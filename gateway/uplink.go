package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"time"

	"github.com/robertkrimen/otto"
	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

type uplinkType string

// Define constant strings for uplink types.
const (
	Unconfirmed uplinkType = "unconfirmed"
	Confirmed   uplinkType = "confirmed"
)

var errInvalidLength = errors.New("unexpected payload length")

// Structure of phyPayload:
// | MHDR | DEV ADDR|  FCTL |   FCnt  | FPort   |  FOpts     |  FRM Payload | MIC |
// | 1 B  |   4 B    | 1 B   |  2 B   |   1 B   | variable    |  variable   | 4B  |
// Returns the node name, readings and error.
func (g *gateway) parseDataUplink(ctx context.Context, phyPayload []byte, packetTime time.Time, snr float64, sf int) (
	string, map[string]interface{}, error,
) {
	// payload should be at least 13 bytes
	if len(phyPayload) < 13 {
		return "", map[string]interface{}{}, fmt.Errorf("%w, payload should be at least 13 bytes but got %d", errInvalidLength, len(phyPayload))
	}

	devAddr := phyPayload[1:5]

	// need to reserve the bytes since payload is in LE.
	devAddrBE := reverseByteArray(devAddr)

	device, err := matchDeviceAddr(devAddrBE, g.devices)
	if err != nil {
		g.logger.Debugf("received packet from unknown device, ignoring")
		return "", map[string]interface{}{}, errNoDevice
	}

	uplinkType := Unconfirmed
	if phyPayload[0] == confirmedUplinkMHdr {
		uplinkType = Confirmed
	}
	g.logger.Debugf("received %s uplink from %s", uplinkType, device.NodeName)

	// confirmed data up, send ACK bit in downlink
	sendAck := phyPayload[0] == confirmedUplinkMHdr

	// Frame control byte contains various settings
	// the last 4 bits is the fopts length
	fctrl := phyPayload[5]
	foptsLength := fctrl & 0x0F

	if len(phyPayload) < 8+int(foptsLength) {
		return "", map[string]interface{}{}, fmt.Errorf("%w, got fopts length of %d but don't have enough bytes", errInvalidLength, foptsLength)
	}

	fopts := phyPayload[8 : 8+foptsLength]

	// get the supported requests from fopts.
	requests := g.getFOptsToSend(fopts, device)

	// frame count - should increase by 1 with each packet sent
	frameCnt := binary.LittleEndian.Uint16(phyPayload[6:8])

	// only validate the MIC if we have a NwkSKey set
	if len(device.NwkSKey) != 0 {
		// Validate the MIC
		mic, err := crypto.ComputeLegacyUplinkMIC(
			types.AES128Key(device.NwkSKey), types.DevAddr(devAddrBE), uint32(frameCnt), phyPayload[:len(phyPayload)-4])
		if err != nil {
			return "", map[string]interface{}{}, err
		}

		if !bytes.Equal(phyPayload[len(phyPayload)-4:], mic[:]) {
			return "", map[string]interface{}{}, errInvalidMIC
		}
	}

	// we will send one device downlink from the do command per uplink.
	var downlinkPayload []byte
	if len(device.Downlinks) > 0 {
		downlinkPayload = device.Downlinks[0]
		// remove the downlink we are about to send to the queue
		device.Downlinks = device.Downlinks[1:]
	}

	// set the duty cycle in first downlink if in EU region.
	setDutyCycle := false
	if device.FCntDown == 0 {
		setDutyCycle = true
		minInterval := calculateMinUplinkInterval(sf, len(phyPayload))
		g.logger.Warnf("Duty cycle limit on EU868 band is 1%%, minimum uplink interval is around %.1f seconds", minInterval)
		device.MinIntervalSeconds = minInterval
		device.MinIntervalUpdated.Store(true)
	}

	if downlinkPayload != nil || len(requests) > 0 || sendAck || setDutyCycle {
		if len(device.NwkSKey) == 0 || device.FCntDown == math.MaxUint16 {
			g.logger.Warnf("Sensor %v must be reset to support new features. "+
				"Please physically restart the sensor to enable downlinks", device.NodeName)
		} else {
			payload, err := g.createDownlink(device, downlinkPayload, requests, sendAck, snr, sf)
			if err != nil {
				return "", map[string]interface{}{}, fmt.Errorf("failed to create downlink: %w", err)
			}
			if err = g.sendDownlink(ctx, payload, false, packetTime); err != nil {
				return "", map[string]interface{}{}, fmt.Errorf("failed to send downlink: %w", err)
			}
			g.logger.Debugf("sent a downlink to %s", device.NodeName)
		}
	}

	// frame port specifies application port - 0 is for MAC commands 1-255 for device messages.
	fPort := phyPayload[8+foptsLength]

	// Ensure there is a frame payload in the packet.
	if int(8+foptsLength+1) >= (len(phyPayload) - 4) {
		return "", map[string]interface{}{}, fmt.Errorf("device %s sent packet with no data", device.NodeName)
	}

	// framepayload is the device readings.
	framePayload := phyPayload[8+foptsLength+1 : len(phyPayload)-4]

	dAddr := types.MustDevAddr(devAddrBE)

	// decrypt the frame payload
	decryptedPayload, err := crypto.DecryptUplink(types.AES128Key(device.AppSKey), *dAddr, (uint32)(frameCnt), framePayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("error while decrypting uplink message: %w", err)
	}

	// decode using the codec.
	readings, err := decodePayload(ctx, fPort, device.DecoderPath, decryptedPayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("error decoding payload of device %s: %w", device.NodeName, err)
	}

	// payload was empty or unparsable
	if len(readings) == 0 {
		return "", map[string]interface{}{}, fmt.Errorf("data received by node %s was not parsable", device.NodeName)
	}

	// Ensure all types in map are protobuf compatible.
	readings = convertTo32Bit(readings)

	// add time to the readings map
	// Note that this won't precisely reflect when the uplink was sent, but since lorawan uplinks are sent infrequently
	// (once per minute max),it will be accurate enough.
	unix := int(time.Now().Unix())
	t := time.Unix(int64(unix), 0)
	timestamp := t.Format(time.RFC3339)
	readings["time"] = timestamp

	return device.NodeName, readings, nil
}

// 8 and 16 bit integers are not supported in protobuf.
// If the decoder returns those types, convert to 32 bit integer.
func convertTo32Bit(readings map[string]interface{}) map[string]interface{} {
	// Iterate over the map and convert uint8 values to uint32
	for key, value := range readings {
		if reflect.TypeOf(value).Kind() == reflect.Uint8 {
			readings[key] = uint32(value.(uint8))
		}
		if reflect.TypeOf(value).Kind() == reflect.Uint16 {
			readings[key] = uint32(value.(uint16))
		}
		if reflect.TypeOf(value).Kind() == reflect.Int16 {
			readings[key] = int32(value.(int16))
		}
		if reflect.TypeOf(value).Kind() == reflect.Int8 {
			readings[key] = int32(value.(int8))
		}
	}
	return readings
}

func matchDeviceAddr(devAddr []byte, devices map[string]*node.Node) (*node.Node, error) {
	for _, dev := range devices {
		if bytes.Equal(devAddr, dev.Addr) {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("no match for DeviceAddress %v", devAddr)
}

func decodePayload(ctx context.Context, fPort uint8, path string, data []byte) (map[string]interface{}, error) {
	//nolint: gosec
	decoder, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}, fmt.Errorf("error reading decoder: %w", err)
	}

	// Convert the byte slice to a string
	fileContent := string(decoder)

	readingsMap, err := convertBinaryToMap(ctx, fPort, fileContent, data)
	if err != nil {
		return map[string]interface{}{}, fmt.Errorf("error executing decoder: %w", err)
	}

	return readingsMap, nil
}

func convertBinaryToMap(ctx context.Context, fPort uint8, decodeScript string, b []byte) (map[string]interface{}, error) {
	decodeScript += "\n\nDecode(fPort, bytes);\n"

	vars := make(map[string]interface{})

	vars["fPort"] = fPort
	vars["bytes"] = b

	v, err := executeDecoder(ctx, decodeScript, vars)
	if err != nil {
		return nil, err
	}

	switch v.(type) {
	case map[string]interface{}:
	default:
		return map[string]interface{}{}, fmt.Errorf("decoder returned unexpected data type: %v", reflect.TypeOf(v))
	}

	readings := v.(map[string]interface{})

	return readings, nil
}

// struct to hold both value and error to send through channel.
type result struct {
	val otto.Value
	err error
}

func executeDecoder(ctx context.Context, script string, vars map[string]interface{}) (out interface{}, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			err = fmt.Errorf("%s", caught)
		}
	}()

	vm := otto.New()
	vm.Interrupt = make(chan func(), 1)
	vm.SetStackDepthLimit(32)

	for k, v := range vars {
		if err := vm.Set(k, v); err != nil {
			return nil, err
		}
	}

	resultChan := make(chan result)
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	go func() {
		var res result
		res.val, res.err = vm.Run(script)
		resultChan <- res
	}()

	select {
	// after 10 ms, interrupt the decoder.
	case <-timeoutCtx.Done():
		vm.Interrupt <- func() {
		}
		return nil, ctx.Err()
	case res := <-resultChan:
		// the decoder completed
		if res.err != nil {
			return nil, res.err
		}
		return res.val.Export()
	}
}

// returns a list of the fopt mac commands the module supports sending a downlink for.
func (g *gateway) getFOptsToSend(fopts []byte, device *node.Node) []byte {
	requests := make([]byte, 0)
	for _, b := range fopts {
		switch b {
		case deviceTimeCID:
			requests = append(requests, b)
		case linkCheckCID:
			requests = append(requests, b)
		case dutyCycleCID:
			// do nothing, response
		default:
			// unsupported mac command
			g.logger.Debugf("got unsupported mac command %x from %s", b, device.NodeName)
		}
	}
	return requests
}

// Calculate an estimated minimum uplink interval based on the sf and size of the uplink.
// Formula for TOA can be found in semtech's sx1262 datasheet 6.1.4:
// https://semtech.my.salesforce.com/sfc/p/#E0000000JelG/a/2R000000Un7F/yT.fKdAr9ZAo3cJLc4F2cBdUsMftpT2vsOICP7NmvMo
func calculateMinUplinkInterval(sf, size int) float64 {
	bw := 125000.0
	dutyCycle := 0.0078125      // 0.78% duty cycle
	preambleSymbols := 8        // symbols in lora packet preamble
	overheadSymbols := 4.25     // other symbols in lora packet
	syncWordSymbols := 8        // overhead for sync word
	explicitHeaderSymbols := 20 // lora explicit header
	codeRate := 1               // lora packets using 4/5 coderate so use 1
	crcNumBits := 16            // num bits in crc

	payloadBits := 8 * size // get the number of bits of the payload
	payloadBits += crcNumBits
	payloadBits -= 4 * (sf) // Unclear what the -4 does, not explained in datasheet.
	payloadBits += 8        // Mystery number from datasheet formula
	payloadBits += explicitHeaderSymbols

	bitsPerSymbol := float64(sf)

	payloadSymbols := math.Ceil(float64(max(payloadBits, 0))/(4*bitsPerSymbol)) * float64(codeRate+4)
	totalSymbols := payloadSymbols + float64(preambleSymbols) + overheadSymbols + float64(syncWordSymbols)

	// calculate time on air
	timePerSymbol := math.Pow(2, float64(sf)) / bw
	toa := totalSymbols * timePerSymbol

	// total time available for uplinks in an hour
	timeav := dutyCycle * 3600
	uplinksPerHour := timeav / toa
	minIntervalSeconds := 3600 / uplinksPerHour

	return minIntervalSeconds
}
