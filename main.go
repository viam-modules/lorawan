// package main is a module for the lorawan gateway
package main

import (
	"gateway/gateway"
	"gateway/node"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

const moduleName = "lorawan"

func main() {
	module.ModularMain(
		moduleName,
		resource.APIModel{API: sensor.API, Model: gateway.Model},
		resource.APIModel{API: sensor.API, Model: node.Model},
	)
}
