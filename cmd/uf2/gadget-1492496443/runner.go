// Autogenerated from device-runner-tinygo.tmpl

package main

import (
	"github.com/merliot/hub"
	"github.com/merliot/hub/examples/gadget"
)

func main() {
	hub.Run(gadget.NewModel)
}
