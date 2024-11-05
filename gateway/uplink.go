package gateway

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/robertkrimen/otto"
	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

// Structure of phyPayload:
// | MHDR | DEV ADDR|  FCTL |   FCnt  | FPort   |  FOpts     |  FRM Payload | MIC |
// | 1 B  |   4 B    | 1 B   |  2 B   |   1 B   | variable    |  variable   | 4B  |
func (g *Gateway) parseDataUplink(phyPayload []byte) (string, map[string]interface{}, error) {

	/// devAddr is bytes one to five - little endian
	devAddr := phyPayload[1:5]

	// need to reserve the bytes to match since devices in BE.
	devAddrBE := reverseByteArray(devAddr)

	device, err := matchDevice(devAddrBE, g.devices)
	if err != nil {
		g.logger.Infof("received packet from unknown device, ignoring")
		return "", map[string]interface{}{}, nil
	}

	// Frame control byte contains settings
	// the last 4 bits is the fopts length
	fctrl := phyPayload[5]
	foptsLength := fctrl & 0x0F

	// frame count - should increase by 1 with each packet sent
	frameCnt := binary.LittleEndian.Uint16(phyPayload[6:8])

	fmt.Println(frameCnt)
	fmt.Println(uint32(frameCnt))

	// fopts not supported in this module yet.
	if foptsLength != 0 {
		_ = phyPayload[8 : 8+foptsLength]
	}

	// frame port specifies application port - 0 is for MAC commands 1-255 for device messages.
	fPort := phyPayload[8+foptsLength]

	framePayload := phyPayload[8+foptsLength+1 : len(phyPayload)-4]

	dAddr := types.MustDevAddr(devAddrBE)

	//TODO: validate the MIC - last 4 bytes
	// mic, err := crypto.ComputeLegacyUplinkMIC(key, *dAddr, (uint32)(frameCnt), phyPayload[:len(phyPayload)-4])

	// decrypt the frame payload
	decryptedPayload, err := crypto.DecryptUplink(device.appSKey, *dAddr, (uint32)(frameCnt), framePayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("error while decrypting uplink message: %w", err)
	}

	// decode using codec
	readings, err := decodePayload(fPort, device.decoderPath, decryptedPayload)
	if err != nil {
		return "", map[string]interface{}{}, fmt.Errorf("Error decoding payload: %w", err)
	}

	return device.name, readings, nil
}

func matchDevice(devAddr []byte, devices map[string]*Device) (*Device, error) {
	for _, dev := range devices {
		if bytes.Equal(devAddr, dev.addr) {
			return dev, nil
		}
	}
	return nil, errors.New("no match")
}

func decodePayload(fPort uint8, path string, data []byte) (map[string]interface{}, error) {
	decoder, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}, err
	}

	// Convert the byte slice to a string
	fileContent := string(decoder)

	readingsMap, err := binaryToMap(fPort, fileContent, data)

	return readingsMap, nil
}

// chirpstack-application-server internal/codec support other type
func binaryToMap(fPort uint8, decodeScript string, b []byte) (map[string]interface{}, error) {

	decodeScript = decodeScript + "\n\nDecode(fPort, bytes);\n"

	vars := make(map[string]interface{})

	vars["fPort"] = fPort
	vars["bytes"] = b

	v, err := executeJS(decodeScript, vars)
	if err != nil {
		return nil, err
	}

	switch v.(type) {
	case map[string]interface{}:
	default:
		return map[string]interface{}{}, errors.New("codec returned unexpected data type")
	}

	readings := v.(map[string]interface{})

	return readings, nil
}

func executeJS(script string, vars map[string]interface{}) (out interface{}, err error) {
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

	go func() {
		time.Sleep(10 * time.Millisecond)
		vm.Interrupt <- func() {
			panic(errors.New("execution timeout"))
		}
	}()

	var val otto.Value
	val, err = vm.Run(script)
	if err != nil {
		return nil, err
	}

	return val.Export()
}
