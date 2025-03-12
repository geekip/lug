package main

import (
	"log"

	"lug/cmd"
	"lug/util"
)

func main() {
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	defer util.VmPool.Shutdown()
}
