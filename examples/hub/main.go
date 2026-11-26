package main

import (
	"log/slog"
	"os"

	bedrockadapter "github.com/dm-vev/adamant/protocol/bedrock"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/server/cmd/builtin"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	bc, err := bedrockadapter.DefaultConfig().Config(log)
	if err != nil {
		log.Error("bedrock config", "err", err)
		os.Exit(1)
	}
	sc := server.DefaultConfig()
	conf, err := sc.Config(log)
	if err != nil {
		log.Error("server config", "err", err)
		os.Exit(1)
	}
	conf.Listeners = append(conf.Listeners, bc.Listener)

	srv := conf.New()
	builtin.Register(srv)
	srv.Listen()
	srv.CloseOnProgramEnd()

	hub := newHubService(nil)
	app := plugin.New(srv, plugin.WithDataDir("plugins"), plugin.WithLogger(log))
	if err := app.UseOrdered(hub.plugin(), newPermPlugin()); err != nil {
		log.Error("enable plugins", "err", err)
		os.Exit(1)
	}
	defer app.Disable()

	for range srv.Accept() {
	}
}
