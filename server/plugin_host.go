package server

import (
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/plugin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"iter"
	"log/slog"
)

type pluginHost struct {
	srv *Server
}

func newPluginHost(srv *Server) plugin.Host[*Server, Config] {
	return pluginHost{srv: srv}
}

func (h pluginHost) Instance() *Server {
	return h.srv
}

func (h pluginHost) Config() Config {
	return h.srv.conf
}

func (h pluginHost) Logger() *slog.Logger {
	return h.srv.conf.Log
}

func (h pluginHost) StartTime() time.Time {
	return h.srv.StartTime()
}

func (h pluginHost) Listen() {
	h.srv.Listen()
}

func (h pluginHost) Accept() iter.Seq[*player.Player] {
	return h.srv.Accept()
}

func (h pluginHost) World() *world.World {
	return h.srv.World()
}

func (h pluginHost) Nether() *world.World {
	return h.srv.Nether()
}

func (h pluginHost) End() *world.World {
	return h.srv.End()
}

func (h pluginHost) MaxPlayerCount() int {
	return h.srv.MaxPlayerCount()
}

func (h pluginHost) PlayerCount() int {
	return h.srv.PlayerCount()
}

func (h pluginHost) Players(tx *world.Tx) iter.Seq[*player.Player] {
	return h.srv.Players(tx)
}

func (h pluginHost) Player(id uuid.UUID) (*world.EntityHandle, bool) {
	return h.srv.Player(id)
}

func (h pluginHost) PlayerByName(name string) (*world.EntityHandle, bool) {
	return h.srv.PlayerByName(name)
}

func (h pluginHost) PlayerByXUID(xuid string) (*world.EntityHandle, bool) {
	return h.srv.PlayerByXUID(xuid)
}

func (h pluginHost) ExecuteCommand(source cmd.Source, commandLine string) {
	<-h.srv.world.Exec(func(tx *world.Tx) {
		cmd.ExecuteLine(source, commandLine, tx, nil)
	})
}

func (h pluginHost) PlayerSummaries() []plugin.PlayerSummary {
	h.srv.pmu.RLock()
	players := make([]*onlinePlayer, 0, len(h.srv.p))
	for _, p := range h.srv.p {
		players = append(players, p)
	}
	h.srv.pmu.RUnlock()

	summaries := make([]plugin.PlayerSummary, 0, len(players))
	for _, op := range players {
		summary := plugin.PlayerSummary{
			UUID: op.handle.UUID(),
			Name: op.name,
			XUID: op.xuid,
		}
		ok := op.handle.ExecWorld(func(tx *world.Tx, e world.Entity) {
			if pl, ok := e.(*player.Player); ok {
				summary.Dimension = tx.World().Dimension()
				summary.Position = pl.Position()
				summary.GameMode = pl.GameMode()
				summary.Latency = pl.Latency()
			}
		})
		summary.Connected = ok
		summaries = append(summaries, summary)
	}
	return summaries
}

func (h pluginHost) CloseOnProgramEnd() {
	h.srv.CloseOnProgramEnd()
}

func (h pluginHost) Close() error {
	return h.srv.Close()
}

func (h pluginHost) LoadPlugins() {
	h.srv.LoadPlugins()
}

func (h pluginHost) PluginsEnabled() bool {
	return h.srv.PluginsEnabled()
}

var _ plugin.Host[*Server, Config] = pluginHost{}
