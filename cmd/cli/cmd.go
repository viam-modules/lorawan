package main

import (
	"context"
	"time"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"

	"gateway/gateway"
)

func main() {
	err := realMain()
	if err != nil {
		panic(err)
	}
}

func realMain() error {
	ctx := context.Background()
	logger := logging.NewLogger("cli")

	pp := 18
	rp := 23

	cfg := gateway.Config{
		PowerPin: &pp,
		ResetPin: &rp,
	}

	g, err := gateway.NewGateway(ctx, sensor.Named("foo").AsNamed(), &cfg, logger)
	if err != nil {
		return err
	}
	defer g.Close(ctx)

	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		r, err := g.Readings(ctx, nil)
		if err != nil {
			return err
		}
		logger.Info(r)
	}

	return nil
}
