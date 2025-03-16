package main

import (
	"log"

	"lug/cmd"
	"lug/util"
)

func main() {
	// util.SetMode("debug")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	defer util.VmPool.Shutdown()
}
