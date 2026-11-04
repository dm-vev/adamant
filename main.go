package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"

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
	// readConfig sets up the logs as config.toml says, writing to stderr.
	drops := newLogDrops()
	drops.register(metrics.Default)
	conf, _, err := readConfig(logging.Options{Dropped: drops.add})
	if err != nil {
		panic(err)
	}
	go func() {
		// Profiles and metrics show what the process holds: serve them to
		// this machine alone.
		http.Handle("/metrics", metrics.Default.Handler())
		log.Println("pprof and metrics on localhost:6060")
		_ = http.ListenAndServe("localhost:6060", nil)
	}()

	srv := conf.New()
	builtin.Register(srv)
	srv.CloseOnProgramEnd()

	if !*headless {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go console.New(srv, conf.Log).Run(ctx)
	}

	srv.Listen()
	for p := range srv.Accept() {
		_ = p
	}
}
