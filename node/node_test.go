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

	"github.com/viam-modules/gateway/testutils"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

const (
	// Common test values.
	testDecoderPath = "/path/to/decoder.js"

	// OTAA test values.
	testDevEUI = "0123456789ABCDEF"
	testAppKey = "0123456789ABCDEF0123456789ABAAAA"

	// ABP test values.
	testDevAddr = "01234567"
	testAppSKey = "0123456789ABCDEF0123456789ABCDEE"
	testNwkSKey = "0123456789ABCDEF0123456789ABCDEF"

	// Gateway dependency.
	testGatewayName = "gateway"
	testNodeName    = "test-node"
)

var (
	testInterval     = 5.0
	testNodeReadings = map[string]interface{}{"reading": 1}
	testDecoderURL   = "https://raw.githubusercontent.com/Milesight-IoT/SensorDecoders/40e844fedbcf9a8c3b279142672fab1c89bee2e0/" +
		"CT_Series/CT101/CT101_Decoder.js"
	nodeNames    = []string{testNodeName}
	gatewayNames = []string{testGatewayName}
)

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

	deps, tmpDir := testutils.NewNodeTestEnv(t, gatewayNames, nodeNames, testDecoderPath)
	// copy the path to the tmpDir
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, testDecoderPath)

	// Test OTAA config
	validConf := resource.Config{
		Name: testNodeName,
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

	testNode := n.(*Node)
	test.That(t, testNode.NodeName, test.ShouldEqual, testNodeName)
	test.That(t, testNode.JoinType, test.ShouldEqual, JoinTypeOTAA)
	test.That(t, testNode.DecoderPath, test.ShouldEqual, testDecoderPath)

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

	testNode = n.(*Node)
	test.That(t, testNode.NodeName, test.ShouldEqual, "test-node-abp")
	test.That(t, testNode.JoinType, test.ShouldEqual, JoinTypeABP)
	test.That(t, testNode.DecoderPath, test.ShouldEqual, testDecoderPath)

	// Verify ABP byte arrays
	expectedDevAddr, err := hex.DecodeString(testDevAddr)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, testNode.Addr, test.ShouldResemble, expectedDevAddr)

	expectedAppSKey, err := hex.DecodeString(testAppSKey)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, testNode.AppSKey, test.ShouldResemble, expectedAppSKey)
	n.Close(ctx)

	// Decoder can be URL
	validConf = resource.Config{
		Name: testNodeName,
		ConvertedAttributes: &Config{
			Decoder:  testDecoderURL,
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	n, err = newNode(ctx, deps, validConf, logger)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldNotBeNil)

	testNode = n.(*Node)
	test.That(t, testNode.NodeName, test.ShouldEqual, testNodeName)
	test.That(t, testNode.JoinType, test.ShouldEqual, JoinTypeOTAA)
	expectedPath := filepath.Join(tmpDir, "CT101_Decoder.js")
	test.That(t, testNode.DecoderPath, test.ShouldEqual, expectedPath)
	n.Close(ctx)

	// Invalid decoder file should error
	invalidDecoderConf := resource.Config{
		Name: testNodeName,
		ConvertedAttributes: &Config{
			Decoder:  "/worong/path",
			Interval: &testInterval,
			JoinType: JoinTypeOTAA,
			DevEUI:   testDevEUI,
			AppKey:   testAppKey,
		},
	}

	_, err = newNode(ctx, deps, invalidDecoderConf, logger)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "provided decoder file path is not valid")
}

func TestReadings(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, tmpDir := testutils.NewNodeTestEnv(t, gatewayNames, nodeNames, "decoder.js")
	// copy the path to the tmpDir
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, "decoder.js")

	validConf := resource.Config{
		Name: testNodeName,
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
	test.That(t, readings, test.ShouldResemble, testNodeReadings)

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
	test.That(t, readings, test.ShouldResemble, NoReadings)

	// If lastReadings is empty and the call is from data manager, return ErrNoCaptureToStore
	_, err = n.Readings(ctx, map[string]interface{}{data.FromDMString: true})
	test.That(t, err, test.ShouldBeError, data.ErrNoCaptureToStore)

	// If data.FromDmString is false, return no error
	_, err = n.Readings(context.Background(), map[string]interface{}{data.FromDMString: false})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, readings, test.ShouldResemble, NoReadings)
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

func TestIsValidURL(t *testing.T) {
	// Test valid URLs
	validURLs := []string{
		"http://example.com",
		"https://example.com",
		"https://example.com/path/to/resource",
		"https://example.com/path/to/resource?param=value",
		"https://sub.example.com:8080/path",
		"ftp://ftp.example.com",
	}

	for _, url := range validURLs {
		test.That(t, isValidURL(url), test.ShouldBeTrue)
	}

	// Test invalid URLs
	invalidURLs := []string{
		"",
		"example.com",
		"http://",
		"https://",
		"/path/to/resource",
		"file.js",
		"./file.js",
		"../file.js",
		"C:/path/to/file.js",
		"/usr/local/file.js",
	}

	for _, url := range invalidURLs {
		test.That(t, isValidURL(url), test.ShouldBeFalse)
	}
}

func TestIsValidFilePath(t *testing.T) {
	// Create a temporary directory for testing files
	tempDir := t.TempDir()

	// Test valid JS file
	validJSPath := filepath.Join(tempDir, "valid.js")
	err := os.WriteFile(validJSPath, []byte("// test file"), 0o666)
	test.That(t, err, test.ShouldBeNil)
	err = isValidFilePath(validJSPath)
	test.That(t, err, test.ShouldBeNil)

	// Test nested valid JS file
	dirPath := filepath.Join(tempDir, "test_dir")
	err = os.Mkdir(dirPath, 0o777)
	test.That(t, err, test.ShouldBeNil)
	nestedJSPath := filepath.Join(dirPath, "nested.js")
	err = os.WriteFile(nestedJSPath, []byte("// nested test file"), 0o666)
	test.That(t, err, test.ShouldBeNil)
	err = isValidFilePath(nestedJSPath)
	test.That(t, err, test.ShouldBeNil)

	// Test non-existent file
	nonExistentPath := filepath.Join(tempDir, "nonexistent.js")
	err = isValidFilePath(nonExistentPath)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "error checking file")

	// Test directory instead of file
	err = isValidFilePath(dirPath)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldEqual, "path is a directory, not a file")

	// Test file with wrong extension
	invalidExtPath := filepath.Join(tempDir, "invalid.txt")
	err = os.WriteFile(invalidExtPath, []byte("test file"), 0o666)
	test.That(t, err, test.ShouldBeNil)
	err = isValidFilePath(invalidExtPath)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldEqual, "decoder must be a .js file")
}

func TestDoCommand(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, tmpDir := testutils.NewNodeTestEnv(t, gatewayNames, nodeNames, "decoder.js")
	// copy the path to the tmpDir
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, "decoder.js")

	validConf := resource.Config{
		Name: testNodeName,
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

	t.Run("Test successful downlink DoCommand that sends to the gateway", func(t *testing.T) {
		req := map[string]interface{}{DownlinkKey: "bytes"}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should receive a success from the gateway
		gatewayResp, gatewayOk := resp[GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeTrue)
		test.That(t, gatewayResp, test.ShouldEqual, "downlink added")

		// we should not receive a node success message
		nodeResp, nodeOk := resp[DownlinkKey].(map[string]interface{})
		test.That(t, nodeOk, test.ShouldBeFalse)
		test.That(t, nodeResp, test.ShouldBeNil)
	})
	t.Run("Test successful downlink DoCommand that returns the node response", func(t *testing.T) {
		// testKey controls whether we send bytes to the gateway. used for debugging.
		req := map[string]interface{}{TestKey: "", DownlinkKey: "bytes"}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldNotBeNil)
		test.That(t, err, test.ShouldBeNil)

		// we should receive a success from the gateway
		gatewayResp, gatewayOk := resp[GatewaySendDownlinkKey].(string)
		test.That(t, gatewayOk, test.ShouldBeFalse)
		test.That(t, gatewayResp, test.ShouldEqual, "")

		// we should not receive a node success message
		nodeResp, nodeOk := resp[DownlinkKey].(map[string]interface{})
		test.That(t, nodeOk, test.ShouldBeTrue)
		test.That(t, nodeResp, test.ShouldNotBeNil)
		testNodeBytes, ok := nodeResp[n.Name().ShortName()]
		test.That(t, ok, test.ShouldBeTrue)
		test.That(t, testNodeBytes, test.ShouldEqual, req[DownlinkKey])
	})

	t.Run("Test nil DoCommand returns empty", func(t *testing.T) {
		resp, err := n.DoCommand(ctx, nil)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err, test.ShouldBeNil)
	})

	t.Run("Test failed downlink DoCommand due to wrong type", func(t *testing.T) {
		req := map[string]interface{}{DownlinkKey: false}
		resp, err := n.DoCommand(ctx, req)
		test.That(t, resp, test.ShouldBeEmpty)
		test.That(t, err.Error(), test.ShouldContainSubstring, "error parsing payload, expected string")
	})
}

func TestIntervalDownlink(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)

	deps, tmpDir := testutils.NewNodeTestEnv(t, gatewayNames, nodeNames, "decoder.js")
	// copy the path to the tmpDir
	testDecoderPath := fmt.Sprintf("%s/%s", tmpDir, "decoder.js")

	validConf := resource.Config{
		Name: testNodeName,
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
	testNode := n.(*Node)

	tests := []struct {
		name              string
		interval          float64
		payloadUnits      Units
		numBytes          int
		useLittleEndian   bool
		header            string
		expectedPayload   string
		testGatewayReturn bool
		expectedErr       string
	}{
		{
			name:              "valid interval in seconds with a size of 2",
			interval:          1,
			payloadUnits:      Seconds,
			numBytes:          2,
			useLittleEndian:   false,
			header:            "01",
			expectedPayload:   "01003C",
			testGatewayReturn: false,
		},
		{
			name:              "valid interval in seconds with a size of 2 that sends to the gateway",
			interval:          1,
			payloadUnits:      Seconds,
			numBytes:          2,
			useLittleEndian:   false,
			header:            "01",
			expectedPayload:   "downlink added",
			testGatewayReturn: true,
		},
		{
			name:              "valid interval in seconds with a size of 4",
			interval:          1,
			payloadUnits:      Seconds,
			numBytes:          4,
			useLittleEndian:   false,
			header:            "01",
			expectedPayload:   "010000003C",
			testGatewayReturn: false,
		},
		{
			name:              "valid interval in seconds with a size of 3 with little endianness",
			interval:          1,
			payloadUnits:      Seconds,
			numBytes:          3,
			useLittleEndian:   true,
			header:            "01",
			expectedPayload:   "013C0000",
			testGatewayReturn: false,
		},
		{
			name:              "valid interval in minutes with a size of 4",
			interval:          1,
			payloadUnits:      Minutes,
			numBytes:          4,
			useLittleEndian:   false,
			header:            "02",
			expectedPayload:   "0200000001",
			testGatewayReturn: false,
		},
		{
			name:              "valid interval with the payload in uppercase",
			interval:          0.5,
			payloadUnits:      Seconds,
			numBytes:          4,
			useLittleEndian:   false,
			header:            "ff8e",
			expectedPayload:   "FF8E0000001E",
			testGatewayReturn: false,
		},
		{
			name:              "fail due to unspecified units",
			interval:          1,
			numBytes:          4,
			useLittleEndian:   false,
			header:            "02",
			expectedPayload:   "",
			testGatewayReturn: false,
			expectedErr:       "cannot send interval downlink, units unspecified",
		},
		{
			name:              "fail due to invalid units",
			interval:          1,
			payloadUnits:      3,
			numBytes:          4,
			useLittleEndian:   false,
			header:            "02",
			expectedPayload:   "",
			testGatewayReturn: false,
			expectedErr:       "cannot send interval downlink, unit 3 unsupported",
		},
		{
			name:              "fail due to unsupported interval because too many bytes",
			interval:          1,
			payloadUnits:      Minutes,
			numBytes:          9,
			useLittleEndian:   false,
			header:            "02",
			expectedPayload:   "",
			testGatewayReturn: false,
			expectedErr:       "cannot send interval downlink, request 9 bytes exceeds allowed number of bytes(8)",
		},
		{
			name:              "fail due to interval is too large for number of bytes",
			interval:          256,
			payloadUnits:      Minutes,
			numBytes:          1,
			useLittleEndian:   false,
			header:            "02",
			expectedPayload:   "0200000001",
			testGatewayReturn: false,
			expectedErr:       "cannot send interval downlink, interval of 256 minutes exceeds maximum number of bytes 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := IntervalRequest{
				IntervalMin: tt.interval, PayloadUnits: tt.payloadUnits, NumBytes: tt.numBytes,
				UseLittleEndian: tt.useLittleEndian, Header: tt.header, TestOnly: !tt.testGatewayReturn,
			}
			resp, err := testNode.SendIntervalDownlink(ctx, req)
			if tt.expectedErr != "" {
				test.That(t, resp, test.ShouldBeNil)
				test.That(t, err.Error(), test.ShouldContainSubstring, tt.expectedErr)
			} else {
				test.That(t, err, test.ShouldBeNil)

				// receive a response from the gateway
				if tt.testGatewayReturn {
					// we should receive a success from the gateway
					gatewayResp, gatewayOk := resp[GatewaySendDownlinkKey].(string)
					test.That(t, gatewayOk, test.ShouldBeTrue)
					test.That(t, gatewayResp, test.ShouldEqual, tt.expectedPayload)

					// we should not receive a interval success message
					nodeResp, nodeOk := resp[IntervalKey].(string)
					test.That(t, nodeOk, test.ShouldBeFalse)
					test.That(t, nodeResp, test.ShouldEqual, "")
				} else {
					// we should not receive a success from the gateway
					gatewayResp, gatewayOk := resp[GatewaySendDownlinkKey].(string)
					test.That(t, gatewayOk, test.ShouldBeFalse)
					test.That(t, gatewayResp, test.ShouldEqual, "")

					// we should receive a interval payload message
					nodeResp, nodeOk := resp[IntervalKey].(string)
					test.That(t, nodeOk, test.ShouldBeTrue)
					test.That(t, nodeResp, test.ShouldEqual, tt.expectedPayload)
				}
			}
		})
	}
}
