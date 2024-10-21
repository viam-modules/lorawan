package parser

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/robertkrimen/otto"
)

const (
	joinRequestLen = 23
	joinRequestMHR = 0x0
)

// JoinRequest represents the structure of a Join Request
type JoinRequest struct {
	JoinEUI  []byte
	DevEUI   []byte
	DevNonce []byte
	MIC      uint32
}

type DataUpLink struct {
	DevAddr []byte //4 bytes
	FCnt    uint16
	Fctrl   byte
	fopts   []byte
	FPort   byte
	payload []byte
	FCS     uint16
}

func DecodePayload(fPort uint8, path string, data []byte) (map[string]interface{}, error) {
	decoder, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}, err
	}

	// Convert the byte slice to a string
	fileContent := string(decoder)

	readingsMap, err := binaryToMap(fPort, fileContent, data)

	return readingsMap, nil
}

// Reverse a slice of bytes
func ReverseBytes(input []byte) []byte {
	result := make([]byte, len(input))
	for i, b := range input {
		result[len(input)-1-i] = b
	}
	return result
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
