package gateway

import (
	"testing"

	"go.viam.com/test"
)

func TestConvertTo32Bit(t *testing.T) {
	// Create test input with various integer types
	input := map[string]interface{}{
		"uint8_val":  uint8(255),
		"uint16_val": uint16(65535),
		"int8_val":   int8(-128),
		"int16_val":  int16(-32768),
		"other_val":  "string", // Should remain unchanged
	}

	// Convert the values
	result := convertTo32Bit(input)

	// Verify uint8 was converted to uint32
	uint8Conv, ok := result["uint8_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint8Conv, test.ShouldEqual, uint32(255))

	// Verify uint16 was converted to uint32
	uint16Conv, ok := result["uint16_val"].(uint32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, uint16Conv, test.ShouldEqual, uint32(65535))

	// Verify int8 was converted to int32
	int8Conv, ok := result["int8_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int8Conv, test.ShouldEqual, int32(-128))

	// Verify int16 was converted to int32
	int16Conv, ok := result["int16_val"].(int32)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, int16Conv, test.ShouldEqual, int32(-32768))

	// Verify non-integer values remain unchanged
	otherVal, ok := result["other_val"].(string)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, otherVal, test.ShouldEqual, "string")
}
