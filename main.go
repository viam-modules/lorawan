// package main is a module for the lorawan gateway
package main

import (
	"context"
	"gateway/gateway"
	"gateway/node"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/utils"
)

func main() {
	utils.ContextualMain(mainWithArgs, module.NewLoggerFromArgs("lorawan-gateway"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	module, err := module.NewModuleFromArgs(ctx, logger)
	if err != nil {
		return err
	}

	if err = module.AddModelFromRegistry(ctx, sensor.API, gateway.Model); err != nil {
		return err
	}

	if err = module.AddModelFromRegistry(ctx, sensor.API, node.Model); err != nil {
		return err
	}

	err = module.Start(ctx)
	defer module.Close(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}
