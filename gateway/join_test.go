package gateway

/*
#cgo CFLAGS: -I./sx1302/libloragw/inc -I./sx1302/libtools/inc
#cgo LDFLAGS: -L./sx1302/libloragw -lloragw -L./sx1302/libtools -lbase64 -lparson -ltinymt32  -lm

#include "../sx1302/libloragw/inc/loragw_hal.h"
#include "gateway.h"
#include <stdlib.h>

*/

import (
	"testing"

	"go.viam.com/test"
)

var devEUI = []byte{0, 2, 3, 4, 5, 6, 7, 0}
var joinEUI = []byte{1, 2, 3, 4, 5, 6, 7, 8}
var devNonce = []byte{1, 1}
var MIC = []byte{1, 1, 4, 4}

func createValidJoinRequest() []byte {
	jr := make([]byte, 0)

	jr = append(jr, 0x00)
	jr = append(jr, joinEUI...)
	jr = append(jr, devEUI...)
	jr = append(jr, devNonce...)
	jr = append(jr, MIC...)

	return jr

}

func TestParseJoinRequestPacket(t *testing.T) {
	devices := make(map[string]*Device)

	testDevice := &Device{devEui: devEUI}

	devices["test"] = testDevice

	// | MHDR | JOIN EUI | DEV EUI  |   DEV NONCE  | MIC   |
	// | 1 B  |   8 B    |    8 B   |     2 B      |  4 B  |}

	jrBytes := createValidJoinRequest()
	jr, _, err := parseJoinRequestPacket(jrBytes, devices)
	test.ShouldBeNil(err)
	test.ShouldEqual(t, jr.devEUI, devEUI)
	test.ShouldEqual(t, jr.joinEUI, joinEUI)
	test.ShouldEqual(t, jr.devNonce, devNonce)

}
