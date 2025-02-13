// package main is a module for the lorawan gateway
package main

import (
	"gateway/draginolht65n"
	"gateway/gateway"
	"gateway/node"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: gateway.Model},
		resource.APIModel{API: sensor.API, Model: node.Model},
		resource.APIModel{API: sensor.API, Model: draginolht65n.Model},
	)
}
