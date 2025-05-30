// package main provides the grpc server for a concentrator
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/lorahw"
	"github.com/viam-modules/gateway/regions"
	v1 "go.viam.com/api/common/v1"
	pb "go.viam.com/api/component/sensor/v1"
	"go.viam.com/rdk/logging"
	"go.viam.com/utils"
	"go.viam.com/utils/protoutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/structpb"
)

type sensorService struct {
	pb.UnimplementedSensorServiceServer
}

var (
	comType     = flag.Int("comType", 1, "comtype 0 for spi 1 for usb")
	path        = flag.String("path", "/dev/ttyACM0", "Path concentrator is connected to")
	region      = flag.Int("region", 1, "region of concentrator, 1 for us915 2 for eu868")
	baseChannel = flag.Int("baseChannel", 0, "base channel to receive packets")
)

func main() {
	utils.ContextualMain(mainWithArgs, logging.NewDebugLogger("cgo"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	config := parseAndValidateArguments()

	// Need to disable buffering on stdout so C logs can be displayed in real time.
	lorahw.DisableBuffering()

	// OS will assign a free port
	//nolint:gosec
	lis, err := net.Listen("tcp", ":0")
	logger.Info("Attempting to bind to TCP port")
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
	//nolint:forbidigo
	fmt.Println("Server successfully started:", port)

	err = lorahw.SetupGateway(config.comType, config.path, config.region, config.baseChannel)
	if err != nil {
		return err
	}
	logger.Info("done setting up gateway")

	go func() {
		// Graceful shutdown
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
		<-c
		logger.Infof("received shutdown signal")
		s.GracefulStop()
	}()

	<-ctx.Done()
	return nil
}

type concentratorConfig struct {
	comType     int
	path        string
	region      regions.Region
	baseChannel int
}

func parseAndValidateArguments() concentratorConfig {
	flag.Parse()

	return concentratorConfig{
		comType:     *comType,
		path:        *path,
		region:      regions.Region(*region),
		baseChannel: *baseChannel,
	}
}

func (s sensorService) DoCommand(ctx context.Context, req *v1.DoCommandRequest) (*v1.DoCommandResponse, error) {
	cmd := req.GetCommand().AsMap()
	if _, ok := cmd[gateway.GetPacketsKey]; ok {
		packets, err := lorahw.ReceivePackets()
		if err != nil {
			return nil, err
		}

		resp := map[string]interface{}{"packets": packets}
		pbRes, err := protoutils.StructToStructPb(resp)
		if err != nil {
			return nil, err
		}
		return &v1.DoCommandResponse{Result: pbRes}, nil
	}
	if packet, ok := cmd[gateway.SendPacketKey]; ok {
		pkt, err := convertToTxPacket(packet.(map[string]interface{}))
		if err != nil {
			return nil, err
		}
		err = lorahw.SendPacket(ctx, pkt)
		if err != nil {
			return nil, err
		}
	}
	if _, ok := cmd[gateway.StopKey]; ok {
		err := lorahw.StopGateway()
		if err != nil {
			return nil, err
		}
	}
	return &v1.DoCommandResponse{}, nil
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
	return &v1.GetGeometriesResponse{}, nil
}

func (s sensorService) GetReadings(context.Context, *v1.GetReadingsRequest) (*v1.GetReadingsResponse, error) {
	return &v1.GetReadingsResponse{Readings: map[string]*structpb.Value{"here": structpb.NewStringValue("hi")}}, nil
}
