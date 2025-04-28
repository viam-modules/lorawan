package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/regions"
	v1 "go.viam.com/api/common/v1"
	pb "go.viam.com/api/component/sensor/v1"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/utils"
	"go.viam.com/utils/protoutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/structpb"
)

type sensorService struct {
	pb.UnimplementedSensorServiceServer
}

func main() {
	utils.ContextualMain(mainWithArgs, module.NewLoggerFromArgs("cgo"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {

	config := parseAndValidateArguments()

	err := lorahw.SetupGateway2(config.comType, config.path, config.region)
	if err != nil {
		return err
	}

	fmt.Println("Attempting to bind to TCP port")

	// OS will assign a free port
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	sensorServ := sensorService{}
	pb.RegisterSensorServiceServer(s, sensorServ)
	reflection.Register(s)

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	port := lis.Addr().(*net.TCPAddr).Port
	fmt.Println("Server successfully started:", port)

	// capture c log output
	startCLogging(ctx, logger)

	go func() {
		// Graceful shutdown
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
		<-c
		log.Println("Received shutdown signal")
		s.GracefulStop()
	}()

	<-ctx.Done()
	return nil
}

type concentratorConfig struct {
	comType int
	path    string
	region  regions.Region
}

func parseAndValidateArguments() concentratorConfig {
	var config concentratorConfig
	comType := flag.Int("comType", 1, "comtype 0 for spi 1 for usb")
	path := flag.String("path", "/dev/ttyACM0", "Path concentrator is connected to")
	region := flag.Int("region", 1, "region of concentrator, 1 for us915 2 for eu868")
	flag.Parse()

	config.path = *path
	config.region = regions.Region(*region)
	config.comType = *comType

	return config
}

func (s sensorService) DoCommand(ctx context.Context, req *v1.DoCommandRequest) (*v1.DoCommandResponse, error) {
	cmd := req.Command.AsMap()
	if _, ok := cmd["get_packets"]; ok {
		packets, err := lorahw.ReceivePackets()
		if err != nil {
			return nil, err
		}

		if len(packets) != 0 {
			fmt.Println("do command got a packet")
		}

		resp := map[string]interface{}{"packets": packets}
		pbRes, err := protoutils.StructToStructPb(resp)
		if err != nil {
			return nil, err
		}
		return &v1.DoCommandResponse{Result: pbRes}, nil
	}
	if packet, ok := cmd["send_packet"]; ok {
		fmt.Println("got send packet request")
		fmt.Println(packet)
		pkt, err := convertToTxPacket(packet.(map[string]interface{}))
		if err != nil {
			return nil, err
		}
		err = lorahw.SendPacket(ctx, pkt)
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func convertToTxPacket(pktMap map[string]interface{}) (*lorahw.TxPacket, error) {
	var pkt lorahw.TxPacket
	// Convert map to JSON
	jsonBytes, err := json.Marshal(pktMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map: %w", err)
	}

	// Convert JSON to struct
	if err := json.Unmarshal(jsonBytes, &pkt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to RxPacket: %w", err)
	}
	return &pkt, nil

}

func (s sensorService) Close(ctx context.Context, extra map[string]interface{}) map[string]interface{} {
	return nil
}

func (s sensorService) GetGeometries(context.Context, *v1.GetGeometriesRequest) (*v1.GetGeometriesResponse, error) {
	return nil, nil
}
func (s sensorService) GetReadings(context.Context, *v1.GetReadingsRequest) (*v1.GetReadingsResponse, error) {
	return &v1.GetReadingsResponse{Readings: map[string]*structpb.Value{"here": structpb.NewStringValue("hi")}}, nil
}

// startCLogging starts the goroutine to capture C logs into the logger.
// If loggingRoutineStarted indicates routine has already started, it does nothing.
func startCLogging(ctx context.Context, logger logging.Logger) {
	logger.Debug("Starting c logger background routine")
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		logger.Errorf("unable to create pipe for C logs")
		return
	}
	// g.logReader = stdoutR
	// g.logWriter = stdoutW

	// Redirect C's stdout to the write end of the pipe
	lorahw.RedirectLogsToPipe(stdoutW.Fd())
	scanner := bufio.NewScanner(stdoutR)

	//g.loggingWorker = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) { g.captureCOutputToLogs(ctx, scanner) })

	captureCOutputToLogs(ctx, scanner, logger)
}

// captureOutput is a background routine to capture C's stdout and log to the module's logger.
// This is necessary because the sx1302 library only uses printf to report errors.
func captureCOutputToLogs(ctx context.Context, scanner *bufio.Scanner, logger logging.Logger) {
	// Need to disable buffering on stdout so C logs can be displayed in real time.
	lorahw.DisableBuffering()

	// loop to read lines from the scanner and log them
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if scanner.Scan() {
				line := scanner.Text()
				switch {
				case strings.Contains(line, "STANDBY_RC mode"):
					// The error setting standby_rc mode indicates a hardware initiaization failure
					// add extra log to instruct user what to do.
					logger.Error(line)
					logger.Error("gateway hardware is misconfigured: unplug the board and wait a few minutes before trying again")
				case strings.Contains(line, "ERROR"):
					logger.Error(line)
				case strings.Contains(line, "WARNING"):
					logger.Warn(line)
				default:
					logger.Debug(line)
				}
			}
		}
	}
}
