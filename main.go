// package main is a module for the lorawan gateway
package main

import (
	"gateway/gateway"
	"gateway/node"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: gateway.ModelHAT},
		resource.APIModel{API: sensor.API, Model: gateway.ModelUSB},
		resource.APIModel{API: sensor.API, Model: node.Model},
	)
}
