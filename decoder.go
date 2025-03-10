package main

import (
	"crypto/aes"
	"encoding/hex"
	"fmt"

	"go.thethings.network/lorawan-stack/v3/pkg/crypto"
	"go.thethings.network/lorawan-stack/v3/pkg/types"
)

// A simplified version of types.AES128Key from the TTN LoRaWAN stack
type AES128Key [16]byte

// Reverse a byte array (used for handling endianness)
func reverseBytes(b []byte) []byte {
	result := make([]byte, len(b))
	for i, j := 0, len(b)-1; i < len(b); i, j = i+1, j-1 {
		result[i] = b[j]
	}
	return result
}

// Decrypt a join-accept message using AES Encrypt (with ECB mode)
// Handles any size payload by processing in 16-byte blocks and handling remainder
func DecryptJoinAccept(key AES128Key, encrypted []byte) ([]byte, error) {
	cipher, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	// Create a buffer for the decrypted payload
	payload := make([]byte, len(encrypted))

	// Process complete 16-byte blocks
	fullBlocks := len(encrypted) / 16
	for i := 0; i < fullBlocks; i++ {
		cipher.Encrypt(payload[i*16:(i+1)*16], encrypted[i*16:(i+1)*16])
	}

	// Handle any remaining bytes (if not a multiple of 16)
	remaining := len(encrypted) % 16
	if remaining > 0 {
		// For remaining bytes, we need to create a temporary block
		startPos := fullBlocks * 16
		tempBlock := make([]byte, 16)
		copy(tempBlock, encrypted[startPos:])

		// Decrypt the temp block
		tempDecrypted := make([]byte, 16)
		cipher.Encrypt(tempDecrypted, tempBlock)

		// Copy just the needed bytes to the result
		copy(payload[startPos:], tempDecrypted[:remaining])
	}

	return payload, nil
}

func main() {
	// // The join accept message (with MHDR)
	joinAcceptHex := "588be9b8b2e78b421f1708ae5f4e90a0fdc6b24dc18f268b8c0bd2b3"

	// // The App Key
	appKeyHex := "39002A9830cc5771fb982203553228c1"

	// // Parse the join accept message
	joinAcceptBytes, err := hex.DecodeString(joinAcceptHex)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Parse the app key
	appKeyBytes, err := hex.DecodeString(appKeyHex)
	if err != nil {
		fmt.Println(err)
		return
	}

	mic := []byte{52, 28, 227, 227}
	//mic := []byte{152, 132, 147, 246}

	joinAcceptBytes = append(joinAcceptBytes, mic...)
	// Decrypt the payload
	decryptedPayload, err := crypto.DecryptJoinAccept(*types.MustAES128Key(appKeyBytes), joinAcceptBytes)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(decryptedPayload)


	

	fmt.Printf("app nonce: %v\n", decryptedPayload[0:3])
	fmt.Printf("net id: %v\n", decryptedPayload[3:6])
	fmt.Printf("dev addr: %v\n", decryptedPayload[6:10])
	fmt.Printf("dl settings: %v\n", decryptedPayload[10:11])
	fmt.Printf("rxdelay: %v\n", decryptedPayload[11])
	fmt.Printf("cflist: %v\n", decryptedPayload[12:28])
	fmt.Printf("%d", len(decryptedPayload))

	// 	nwkSKey, _ := hex.DecodeString("8cfaca253b3ed22727a6a299212f6386")

	// 	appSKey, _ := hex.DecodeString("968196e9e19a76438a26e9a1f37c6a78")
	// 	devAddrBE := []byte{0x01, 0x19, 0xff, 0xd2}
	// 	devAddrLE := reverseByteArray(devAddrBE)

	// 	nwkSKey, _ := hex.DecodeString("31667dc9088091860d03d00eb04fdf11")
	// 	appSKey, _ := hex.DecodeString("49f8bc5df75aea815af374bf6775a63f")
	// 	var fCntDown uint16 = 4

	// 	devAddrBE := []byte{0x01, 0xe8, 0xf9, 0x02}
	// 	devAddrLE := reverseByteArray(devAddrBE)
	// 	// payload := []byte{96, 157, 235, 5, 0, 128, 2, 0, 57, 255, 0, 1, 1, 61, 239, 231, 4, 162, 91, 218, 82}
	// 	// mic, err := crypto.ComputeLegacyDownlinkMIC(*types.MustAES128Key(nwkSKey), *types.MustDevAddr(devAddrBE), uint32(fCntDown), payload)
	// 	// if err != nil {
	// 	// 	fmt.Println(err)
	// 	// }

	// 	payload := make([]byte, 0)

	// 	// Mhdr unconfirmed data down
	// 	payload = append(payload, 0x60)

	// 	payload = append(payload, devAddrLE...)

	// 	// 3. FCtrl: ADR (1), RFU (0), ACK (0), FPending (0), FOptsLen (5)
	// 	payload = append(payload, 0x80)

	// 	fCntBytes := make([]byte, 2)
	// 	binary.LittleEndian.PutUint16(fCntBytes, fCntDown)
	// 	payload = append(payload, fCntBytes...)

	// 	// fopts := []byte{
	// 	// 	0b00111001, // data rate and tx power
	// 	// 	0xFF,
	// 	// 	0x00,
	// 	// 	0x01,
	// 	// }

	// 	// fopts := []byte{0x3A, 0xFF, 0x00, 0x01}

	// 	// payload = append(payload, fopts...)

	// 	// // Fport
	// 	// Change to 0x00 for MAC-only downlink
	// 	payload = append(payload, 0x01) // 0x85 for tilt

	// 	// 1 min
	// 	framePayload := []byte{0x01, 0x00, 0x02, 0x58} //  dragino
	// 	//framePayload := []byte{0xff, 0x10, 0xff} //tilt reset

	// 	encrypted, err := crypto.EncryptDownlink(*types.MustAES128Key(appSKey), *types.MustDevAddr(devAddrBE), uint32(fCntDown), framePayload)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 	}

	// 	//payload = payload[:len(payload)-len(framePayload)]
	// 	payload = append(payload, encrypted...)

	// 	mic, err := crypto.ComputeLegacyDownlinkMIC(*types.MustAES128Key(nwkSKey), *types.MustDevAddr(devAddrBE), uint32(fCntDown), payload)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 	}

	// 	fmt.Println(mic)
	// 	fmt.Printf("%x\n", encrypted)

	// 	validMIC := []byte{42, 70, 129, 125}

	// 	if bytes.Equal(mic[:], validMIC) {
	// 		fmt.Println("correct")
	// 	} else {
	// 		fmt.Println("incorrect")
	// 	}

	// }

	// // reverseByteArray creates a new array reversed of the input.
	// // Used to convert little endian fields to big endian and vice versa.
	// func reverseByteArray(arr []byte) []byte {
	// 	reversed := make([]byte, len(arr))

	//		for i, j := 0, len(arr)-1; i < len(arr); i, j = i+1, j-1 {
	//			reversed[i] = arr[j]
	//		}
	//		return reversed
	//	}
}
