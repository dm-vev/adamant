package session_test

import (
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// goldenListener is a server.Listener that hands the server one connection.
type goldenListener struct {
	conns     chan session.Conn
	closeOnce sync.Once
	closed    chan struct{}
}

func (l *goldenListener) Accept() (session.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (*goldenListener) Disconnect(session.Conn, string) error { return nil }

func (l *goldenListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

// goldenServer is what a server.Server sends a client before its session
// starts: the game data handed to StartGameContext and what the server
// writes after it.
type goldenServer struct {
	err     error
	dump    string
	written []proto.Packet
}

// joinServer starts a server.Server once per process, in a world as the
// scenarios set theirs, and has goldenName join it through a listener of
// the test. Making the server also registers the creative items and the
// recipes of vanilla, which the sessions of the scenarios send.
var joinServer = sync.OnceValue(func() goldenServer {
	l := &goldenListener{conns: make(chan session.Conn, 1), closed: make(chan struct{})}
	srv := server.Config{
		Log:                     slog.New(slog.DiscardHandler),
		Listeners:               []func(server.Config) (server.Listener, error){func(server.Config) (server.Listener, error) { return l, nil }},
		Name:                    "Golden Server",
		DisableResourceBuilding: true,
		DisableNether:           true,
		DisableEnd:              true,
		MaxChunkRadius:          goldenRadius,
		RandomTickSpeed:         -1,
		OverworldSeed:           goldenSeed,
		WorldProvider:           world.NopProvider{Set: goldenSettings()},
		Generator:               func(world.Dimension, int64) world.Generator { return goldenGenerator() },
	}.New()
	// The world of the server ticks on a clock: its time stood still from
	// the start, but set it again in case a tick got in first.
	srv.World().StopTime()
	srv.World().SetTime(goldenTime)
	if err := goldenRules(srv.World()); err != nil {
		return goldenServer{err: err}
	}

	conn := newGoldenConn()
	l.conns <- conn
	srv.Listen()
	defer func() { _ = srv.Close() }()

	var out goldenServer
	accepted := make(chan struct{})
	go func() {
		for range srv.Accept() {
			break
		}
		close(accepted)
	}()
	select {
	case <-accepted:
	case <-time.After(10 * time.Second):
		out.err = errors.New("the server did not accept the player")
		return out
	}
	conn.mu.Lock()
	data := conn.data
	conn.mu.Unlock()
	if data == nil {
		out.err = errors.New("the server did not start the game")
		return out
	}
	// The game rules of the world and the items come from maps: they are in
	// no order, so they are sorted by name.
	rules := slices.Clone(data.GameRules)
	slices.SortFunc(rules, func(a, b v2193.GameRule) int { return strings.Compare(a.Name, b.Name) })
	data.GameRules = rules
	items := slices.Clone(data.Items)
	slices.SortFunc(items, func(a, b v2193.ItemEntry) int { return strings.Compare(a.Name, b.Name) })
	data.Items = items
	var b strings.Builder
	writeValue(&b, "    ", "", reflect.ValueOf(*data))
	out.dump = b.String()
	// What the server writes after StartGameContext and before the session
	// is made: the items of the server.
	for len(conn.written) > 0 && len(out.written) == 0 {
		out.written = append(out.written, <-conn.written)
	}
	return out
})

// TestGoldenJoin records what a client is sent from StartGame until it
// spawned: the game data the server starts the game with, the items it
// writes after, then what the session sends as it is made and the player
// spawns (biomes, creative content, recipes, trims, the player list,
// health, food, experience, chunk publisher, entities, game type,
// abilities, permissions, attributes, inventories, commands) and in the
// first tick (chunks around the spawn).
func TestGoldenJoin(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "join", scenarioConfig{keepJoin: true})
	srv := joinServer()
	var head strings.Builder
	head.WriteString("== StartGameContext of the server\n")
	head.WriteString(">> GameData\n")
	head.WriteString(srv.dump)
	head.WriteString("== written by the server after StartGameContext\n")
	for _, pk := range srv.written {
		raw := encode(pk)
		head.WriteString(s.describe(decodeAs(t, pk.ID(), raw), raw, ">>"))
	}
	rest := s.out.String()
	s.out.Reset()
	s.out.WriteString(head.String())
	s.out.WriteString(rest)
	s.snapshot()
}
