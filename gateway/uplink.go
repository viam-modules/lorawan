package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"gateway/node"
	"os"
	"time"

	"github.com/robertkrimen/otto"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

// Structure of phyPayload:
// | MHDR | DEV ADDR|  FCTL |   FCnt  | FPort   |  FOpts     |  FRM Payload | MIC |
// | 1 B  |   4 B    | 1 B   |  2 B   |   1 B   | variable    |  variable   | 4B  |
func (g *Gateway) parseDataUplink(ctx context.Context, phyPayload []byte) (string, map[string]interface{}, error) {

	devAddr := phyPayload[1:5]

	// need to reserve the bytes since payload is in LE.
	devAddrBE := reverseByteArray(devAddr)

	device, err := matchDeviceAddr(devAddrBE, g.devices)
	if err != nil {
		g.logger.Infof("received packet from unknown device, ignoring")
		return "", map[string]interface{}{}, errNoDevice
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

	// device data in the message.
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
		return "", map[string]interface{}{}, fmt.Errorf("Error decoding payload: %w", err)
	}

	// update the last reading timestamp
	device.LastReadingTime = int(time.Now().Unix())

	unix := int(time.Now().Unix())
	t := time.Unix(int64(unix), 0)

	// Format the Time object into a timestamp string
	timestamp := t.Format(time.RFC3339)
	readings["time"] = timestamp

	return device.NodeName, readings, nil
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
	decoder, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}, err
	}

	// Convert the byte slice to a string
	fileContent := string(decoder)

	readingsMap, err := convertBinaryToMap(ctx, fPort, fileContent, data)

	return readingsMap, nil
}

func convertBinaryToMap(ctx context.Context, fPort uint8, decodeScript string, b []byte) (map[string]interface{}, error) {

	decodeScript = decodeScript + "\n\nDecode(fPort, bytes);\n"

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
		return map[string]interface{}{}, errors.New("decoder returned unexpected data type")
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
	timeoutCtx, _ := context.WithTimeout(ctx, 10*time.Millisecond)

	go func() {
		var res result
		res.val, res.err = vm.Run(script)
		resultChan <- res
	}()

	select {
	case <-timeoutCtx.Done():
		vm.Interrupt <- func() {
			errors.New("ctx canceled")
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
