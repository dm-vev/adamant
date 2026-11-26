package app

import (
	"context"

	"github.com/dm-vev/adamant/net/metrics"
	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/server/cmd/builtin"
	"github.com/dm-vev/adamant/server/console"
	"github.com/dm-vev/adamant/server/logging"
	"github.com/dm-vev/adamant/server/monitor"
	"github.com/dm-vev/adamant/server/player/chat"
)

// Run reads config.toml from the working directory, starts the server with its
// monitoring, remote admin, and optional interactive console services, and
// blocks until the server shuts down.
func Run(headless bool) {
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

	if !headless {
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
	mon := proc.monitor.Service(monitor.Options{Log: proc.logs.Logger(logging.Server)})
	mon.Serve()
	conf.Services = append(conf.Services, mon, remoteAdminService(proc))

	srv := conf.New()
	builtin.Register(srv)
	srv.Listen()
	return srv
}
