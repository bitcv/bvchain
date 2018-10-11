package main

import (
	"bvchain/cfg"
	"bvchain/node"
	"bvchain/util/log"
	"bvchain/util"

	"flag"
	"fmt"
	"os"
)

func main() {
	logger := log.New(os.Stderr)

	cfgfile := flag.String("config", "config.toml", "configurations")
	flag.Parse()

	config, err := cfg.LoadConfig(*cfgfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERR: %#v\n", err)
		os.Exit(1)
	}

	node, err := node.NewNode(config, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERR: %#v\n", err)
		os.Exit(1)
	}
	node.Start()
	util.TrapSignalTerm(func(sig os.Signal){
		fmt.Printf("captured %v, exiting...\n", sig)
		node.Stop()
	})
	node.WaitForStop()
}

