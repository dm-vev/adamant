package main

import (
	"flag"

	"github.com/df-mc/dragonfly/internal/app"
)

func main() {
	headless := flag.Bool("headless", false, "run without the interactive console")
	flag.Parse()
	app.Run(*headless)
}
