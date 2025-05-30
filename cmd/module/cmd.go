// package main is a module for the lorawan gateway
package main

import (
	"github.com/viam-modules/lorawan/draginolht65n"
	"github.com/viam-modules/lorawan/draginowqslb"
	"github.com/viam-modules/lorawan/gateway"
	"github.com/viam-modules/lorawan/milesightct101"
	"github.com/viam-modules/lorawan/milesightem310"
	"github.com/viam-modules/lorawan/node"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: gateway.Model}, // TODO: remove after migration(or keep it as a secret)
		resource.APIModel{API: sensor.API, Model: gateway.ModelGenericHat},
		resource.APIModel{API: sensor.API, Model: gateway.ModelSX1302WaveshareHat},
		resource.APIModel{API: sensor.API, Model: gateway.ModelRak7391},
		resource.APIModel{API: sensor.API, Model: node.Model},
		resource.APIModel{API: sensor.API, Model: draginolht65n.Model},
		resource.APIModel{API: sensor.API, Model: milesightem310.Model},
		resource.APIModel{API: sensor.API, Model: milesightct101.Model},
		resource.APIModel{API: sensor.API, Model: draginowqslb.Model},
	)
}
