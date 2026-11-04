package main

import (
	"context"
	"flag"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/console"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/dm-vev/adamant/net/metrics"
)

func main() {
	headless := flag.Bool("headless", false, "run without the interactive console")
	flag.Parse()
	chat.Global.Subscribe(chat.StdoutSubscriber{})
	drops := newLogDrops()
	drops.register(metrics.Default)
	// readConfig sets up the logs as config.toml says, writing to stderr.
	conf, proc, err := readConfig(logging.Options{Dropped: drops.add})
	if err != nil {
		panic(err)
	}
	srv := start(conf, proc)
	srv.CloseOnProgramEnd()

	if !*headless {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go console.New(srv, conf.Log).Run(ctx)
	}

	for p := range srv.Accept() {
		_ = p
	}
}

// start makes the server of conf, with the monitor and the remote admin
// API of proc as its services, registers the commands and starts it. The
// monitor serves before the worlds are opened, so that /livez answers
// while they load. Closing the server closes both, freeing their ports.
func start(conf server.Config, proc process) *server.Server {
	mon := newMonitor(proc.monitor, proc.logs.Logger(logging.Server))
	mon.serve()
	conf.Services = append(conf.Services, mon, remoteAdminService(proc))

	srv := conf.New()
	builtin.Register(srv)
	srv.Listen()
	return srv
}
