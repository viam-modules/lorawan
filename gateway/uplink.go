package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/robertkrimen/otto"
	"github.com/viam-modules/gateway/node"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

// Structure of phyPayload:
// | MHDR | DEV ADDR|  FCTL |   FCnt  | FPort   |  FOpts     |  FRM Payload | MIC |
// | 1 B  |   4 B    | 1 B   |  2 B   |   1 B   | variable    |  variable   | 4B  |.
// Returns the node name, readings and error.
func (g *gateway) parseDataUplink(ctx context.Context, phyPayload []byte, packetTime time.Time) (string, map[string]interface{}, error) {
	devAddr := phyPayload[1:5]

	// need to reserve the bytes since payload is in LE.
	devAddrBE := reverseByteArray(devAddr)

	device, err := matchDeviceAddr(devAddrBE, g.devices)
	if err != nil {
		g.logger.Debugf("received packet from unknown device, ignoring")
		return "", map[string]interface{}{}, errNoDevice
	}

	g.logger.Debugf("received uplink from %s", device.NodeName)

	sendAck := false
	// confirmed data up, send confirmation bit in downlink
	if phyPayload[0] == 0x80 {
		sendAck = true
	}

	var downlinkPayload []byte
	if len(device.Downlinks) > 0 {
		// we will send one device downlink from the do command per uplink.
		var err error
		downlinkPayload, err = g.createDownlink(device, device.Downlinks[0], sendAck)
		if err != nil {
			return "", map[string]interface{}{}, fmt.Errorf("failed to create downlink: %w", err)
		}

		// remove the downlink we just sent from the queue
		device.Downlinks = device.Downlinks[1:]
		// no frame payloads to send but we still need to send the ack.
	} else if sendAck {
		downlinkPayload, err = g.createDownlink(device, nil, sendAck)
		if err != nil {
			return "", map[string]interface{}{}, fmt.Errorf("failed to create ack downlink: %w", err)
		}
	}

	if downlinkPayload != nil {
		err = g.sendDownlink(ctx, downlinkPayload, false, packetTime)
		if err != nil {
			// don't return error if the downlink fails, we still want to parse the uplink.
			g.logger.Errorf("failed to send downlink: %w", err)
		}

		g.logger.Debugf("sent downlink packet to %s", device.NodeName)
	}

	// Frame control byte contains various settings
	// the last 4 bits is the fopts length
	fctrl := phyPayload[5]
	foptsLength := fctrl & 0x0F

	// frame count - should increase by 1 with each packet sent
	frameCnt := binary.LittleEndian.Uint16(phyPayload[6:8])

	// fopts not supported in this module yet.
	if foptsLength != 0 {
		_ = phyPayload[8 : 8+foptsLength]
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
		return map[string]interface{}{}, fmt.Errorf("decoder returned unexpected data type: %s", reflect.TypeOf(v))
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
