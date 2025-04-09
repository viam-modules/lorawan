// package main is a module for the lorawan gateway
package main

import (
	"github.com/viam-modules/gateway/draginolht65n"
	"github.com/viam-modules/gateway/gateway"
	"github.com/viam-modules/gateway/gatewaypoc"
	"github.com/viam-modules/gateway/milesightct101"
	"github.com/viam-modules/gateway/milesightem310"
	networkserver "github.com/viam-modules/gateway/network-server"
	"github.com/viam-modules/gateway/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: gateway.Model}, // TODO: remove after migration(or keep it as a secret)
		resource.APIModel{API: sensor.API, Model: gateway.ModelGenericHat},
		resource.APIModel{API: sensor.API, Model: gateway.ModelSX1302WaveshareHat},
		resource.APIModel{API: sensor.API, Model: node.Model},
		resource.APIModel{API: sensor.API, Model: draginolht65n.Model},
		resource.APIModel{API: sensor.API, Model: milesightem310.Model},
		resource.APIModel{API: sensor.API, Model: milesightct101.Model},
		resource.APIModel{API: sensor.API, Model: gatewaypoc.Model},
		resource.APIModel{API: sensor.API, Model: networkserver.Model},
	)
}
