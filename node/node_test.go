package node

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.viam.com/rdk/components/encoder"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/test"
)

const (

	// Common test values.
	testDecoderPath = "/path/to/decoder"

	// OTAA test values.
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// ABP test values.
	testDevAddr = "01234567"
	testAppSKey = "0123456789ABCDEF0123456789ABCDEE"
	testNwkSKey = "0123456789ABCDEF0123456789ABCDEF"

	// Gateway dependency.
	testGatewayName = "gateway"
)

var (
	testInterval     = 5.0
	testNodeReadings = map[string]interface{}{"reading": 1}
	testDecoderURL   = "https://raw.githubusercontent.com/Milesight-IoT/SensorDecoders/40e844fedbcf9a8c3b279142672fab1c89bee2e0/" +
		"CT_Series/CT101/CT101_Decoder.js"
)

func createMockGateway() *inject.Sensor {
	mockGateway := &inject.Sensor{}
	mockGateway.DoFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		if _, ok := cmd["validate"]; ok {
			return map[string]interface{}{"validate": 1.0}, nil
		}
		return map[string]interface{}{}, nil
	}
	mockGateway.ReadingsFunc = func(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
		readings := make(map[string]interface{})
		readings["test-node"] = testNodeReadings
		return readings, nil
	}
	return mockGateway
}

func TestConfigValidate(t *testing.T) {
	// valid config
	conf := &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		DevEUI:   testDevEUI,
		AppKey:   testAppKey,
		Gateways: []string{testGatewayName},
	}
	deps, err := conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(deps), test.ShouldEqual, 1)
	test.That(t, deps[0], test.ShouldEqual, testGatewayName)

	// Test missing decoder path
	conf = &Config{
		Interval: &testInterval,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrDecoderPathRequired))

	// Test missing interval
	conf = &Config{
		Decoder: testDecoderPath,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrIntervalRequired))

	zeroInterval := 0.0
	// Test zero interval
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &zeroInterval,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrIntervalZero))

	// Test invalid join type
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: "INVALID",
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrInvalidJoinType))
}

func TestValidateOTAAAttributes(t *testing.T) {
	// Test missing DevEUI
	conf := &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		AppKey:   testAppKey,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrDevEUIRequired))

	// Test invalid DevEUI length
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		DevEUI:   "0123456", // Not 8 bytes
		AppKey:   testAppKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrDevEUILength))

	// Test missing AppKey
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		DevEUI:   testDevEUI,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrAppKeyRequired))

	// Test invalid AppKey length
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		DevEUI:   testDevEUI,
		AppKey:   "0123456", // Not 16 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrAppKeyLength))

	// Test valid OTAA config
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeOTAA,
		DevEUI:   testDevEUI,
		AppKey:   testAppKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
}

func TestValidateABPAttributes(t *testing.T) {
	// Test missing AppSKey
	conf := &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		NwkSKey:  testNwkSKey,
		DevAddr:  testDevAddr,
	}
	_, err := conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrAppSKeyRequired))

	// Test invalid AppSKey length
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  "0123456", // Not 16 bytes
		NwkSKey:  testNwkSKey,
		DevAddr:  testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrAppSKeyLength))

	// Test missing NwkSKey
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  testAppSKey,
		DevAddr:  testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrNwkSKeyRequired))

	// Test invalid NwkSKey length
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  testAppSKey,
		NwkSKey:  "0123456", // Not 16 bytes
		DevAddr:  testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrNwkSKeyLength))

	// Test missing DevAddr
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  testAppSKey,
		NwkSKey:  testNwkSKey,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrDevAddrRequired))

	// Test invalid DevAddr length
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  testAppSKey,
		NwkSKey:  testNwkSKey,
		DevAddr:  "0123", // Not 4 bytes
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeError, resource.NewConfigValidationError("", ErrDevAddrLength))

	// Test valid ABP config
	conf = &Config{
		Decoder:  testDecoderPath,
		Interval: &testInterval,
		JoinType: JoinTypeABP,
		AppSKey:  testAppSKey,
		NwkSKey:  testNwkSKey,
		DevAddr:  testDevAddr,
	}
	_, err = conf.Validate("")
	test.That(t, err, test.ShouldBeNil)
}

func TestNewNode(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	// Test OTAA config
	validConf := resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			Decoder:  testDecoderPath,
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node := n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node")
	test.That(t, node.JoinType, test.ShouldEqual, JoinTypeOTAA)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Test with valid ABP config
	validABPConf := resource.Config{
		Name: "test-node-abp",
		ConvertedAttributes: &Config{
			Decoder:  testDecoderPath,
			Interval: &testInterval,
			JoinType: JoinTypeABP,
			AppSKey:  testAppSKey,
			NwkSKey:  testNwkSKey,
			DevAddr:  testDevAddr,
		},
	}

	n, err = newNode(ctx, deps, validABPConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node = n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node-abp")
	test.That(t, node.JoinType, test.ShouldEqual, JoinTypeABP)
	test.That(t, node.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Verify ABP byte arrays
	expectedDevAddr, err := hex.DecodeString(testDevAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.Addr, test.ShouldResemble, expectedDevAddr)

	expectedAppSKey, err := hex.DecodeString(testAppSKey)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, node.AppSKey, test.ShouldResemble, expectedAppSKey)
	n.Close(ctx)

	// Decoder can be URL
	validConf = resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			Decoder:  testDecoderURL,
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	tmpDir := t.TempDir()
	t.Setenv("VIAM_MODULE_DATA", tmpDir)

	n, err = newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	node = n.(*Node)
	test.That(t, node.NodeName, test.ShouldEqual, "test-node")
	test.That(t, node.JoinType, test.ShouldEqual, JoinTypeOTAA)
	expectedPath := filepath.Join(tmpDir, "CT101_Decoder.js")
	test.That(t, node.DecoderPath, test.ShouldEqual, expectedPath)
	n.Close(ctx)
}

func TestReadings(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	mockGateway := createMockGateway()
	deps := make(resource.Dependencies)
	deps[encoder.Named(testGatewayName)] = mockGateway

	validConf := resource.Config{
		Name: "test-node",
		ConvertedAttributes: &Config{
			Decoder:  testDecoderPath,
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err := newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	readings, err := n.Readings(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldEqual, testNodeReadings)

	// node for empty readings.
	validConf = resource.Config{
		Name: "other-node",
		ConvertedAttributes: &Config{
			Decoder:  testDecoderPath,
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err = newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	// If lastReadings is empty and the call is not from data manager, return no error.
	readings, err = n.Readings(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, noReadings)

	// If lastReadings is empty and the call is from data manager, return ErrNoCaptureToStore
	_, err = n.Readings(ctx, map[string]interface{}{data.FromDMString: true})
	test.That(t, err, test.ShouldBeError, data.ErrNoCaptureToStore)

	// If data.FromDmString is false, return no error
	_, err = n.Readings(context.Background(), map[string]interface{}{data.FromDMString: false})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, noReadings)
}

type ctrl struct {
	statusCode int
	response   string
}

func (c *ctrl) mockHandler(w http.ResponseWriter, r *http.Request) {
	resp := []byte(c.response)

	w.WriteHeader(c.statusCode)
	w.Write(resp)
}

// HTTPMock creates a mock HTTP server.
func HTTPMock(pattern string, statusCode int, response string) *httptest.Server {
	c := &ctrl{statusCode, response}

	handler := http.NewServeMux()
	handler.HandleFunc(pattern, c.mockHandler)

	return httptest.NewServer(handler)
}

func TestWriteDecoder(t *testing.T) {
	logger := logging.NewTestLogger(t)
	// Prep first run directory
	dataDirectory1 := t.TempDir()

	t.Setenv("VIAM_MODULE_DATA", dataDirectory1)

	t.Run("Test successful request of decoder", func(t *testing.T) {
		resp := "good test"
		srv := HTTPMock("/myurl", http.StatusOK, resp)
		sClient := &http.Client{
			Timeout: time.Second * 180,
		}
		decoderName1 := "decoder1.js"

		fileName1, err := WriteDecoderFileFromURL(context.Background(), decoderName1, srv.URL+"/myurl", sClient, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, fileName1, test.ShouldContainSubstring, decoderName1)
		file1, err := os.ReadFile(fileName1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, string(file1), test.ShouldEqual, resp)
		// second decoder

		resp2 := "good test 2"
		srv2 := HTTPMock("/myurl", http.StatusOK, resp2)
		sClient2 := &http.Client{
			Timeout: time.Second * 180,
		}
		decoderName2 := "decoder2.js"

		fileName2, err := WriteDecoderFileFromURL(context.Background(), decoderName2, srv2.URL+"/myurl", sClient2, logger)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, fileName2, test.ShouldContainSubstring, decoderName2)
		file2, err := os.ReadFile(fileName2)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, string(file2), test.ShouldEqual, resp2)
		dirEntries, err := os.ReadDir(dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dirEntries), test.ShouldEqual, 2)

		// repeat decoder
		_, err = WriteDecoderFileFromURL(context.Background(), decoderName1, srv.URL+"/myurl", sClient, logger)
		test.That(t, err, test.ShouldBeNil)
		dirEntries, err = os.ReadDir(dataDirectory1)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, len(dirEntries), test.ShouldEqual, 2)
	})

	t.Run("Test failed request of decoder", func(t *testing.T) {
		resp := "bad test"
		srv := HTTPMock("/myurl", http.StatusNotFound, resp)
		sClient := &http.Client{
			Timeout: time.Second * 180,
		}
		decoderBad := "decoder3.js"

		_, err := WriteDecoderFileFromURL(context.Background(), decoderBad, srv.URL+"/myurl", sClient, logger)
		test.That(t, err, test.ShouldBeError, fmt.Errorf(ErrBadDecoderURL, 404))
	})
}
