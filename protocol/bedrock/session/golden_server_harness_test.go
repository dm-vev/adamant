package session_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The golden tests of the server join their clients to a server.Server the
// way a client joins one: the session.Listener of a ConnListener of the test
// hands the server the joins of connections, and the server starts their
// game, makes their sessions and players, spawns them through Accept,
// reconnects, disconnects and closes them itself. The scenarios of golden_harness_test.go instead
// make the session and the player as the server does; these go through the
// server, as built from a server.UserConfig the way the program builds it,
// with its operators, whitelist and bans in files of a directory of the test.
// The client is recorded with the normalisations of the harness, in its
// format, plus those below, which the server's clock makes necessary.
//
// The world of a server ticks on a clock of its own, twenty times a second,
// so the steps of these scenarios cannot be ticks. What they record is still
// the same from run to run:
//
//   - The world is flat, its time and weather stand still, and the chunks
//     around the spawn are loaded, by a loader of the test, before anyone
//     joins: a chunk a client is sent is ready as soon as it is asked for,
//     in the order the session asks for them, not in the order the
//     generator happens to finish them.
//   - A step ends with a marker, a message sent through each player in a
//     transaction of the world after what the step waits for: the ticks a
//     step took are serialised with it, and once the client's chunks are
//     all sent a tick of a still world sends a client nothing, save the
//     weather below.
//   - Before its marker a step lets the world tick 21 times: a session sends
//     the enums of its commands that changed, as the names of the players
//     online, every twentieth of its own ticks, and so within the step.
//   - The spawn of a player ends in the transaction of Accept, where the
//     loop body of the test writes a marker to every player: what the spawn
//     sends is recorded apart from what the ticks after it send.
//
// Normalised on top of the harness's normaliser, each because the clock
// decides it rather than the server:
//
//   - Every twenty ticks of its clock the world shows its viewers the
//     weather, a LevelEvent StopRaining and a LevelEvent StopThunderstorm
//     with no data, the same pair every time. How many a step sees depends
//     on how long it took, so in what the ticks send they are left out
//     (any other weather is recorded). The pair a player is sent as it
//     spawns is recorded.
//   - The session sends the abilities of a player again 50ms after a game
//     type, on a timer of its own whose transaction lands between the
//     ticks that send the chunks wherever the clock puts it. In what the
//     ticks after a spawn send, the UpdateAbilities packets are moved out
//     of their place to the end of the step, under a heading of their own.
//   - When the server closes, it disconnects its players in the order of a
//     map, and a client then may or may not be sent the departure of a
//     player disconnected before it before its own connection closes. In
//     what a client is sent as the server closes, a player list removal,
//     an actor removal and a chat message about another player are left
//     out; anything else is recorded.
//   - A connection the server closed may still have been written packets
//     the session had queued before the Disconnect, by flushers racing the
//     close. What a client is sent after its Disconnect packet is not
//     recorded: the client is gone.
//   - What the server saves of a player is recorded without the fields that
//     count ticks (FoodTick, TimeSinceRest) or are drawn at random
//     (EnchantmentSeed): see savedLine.

// serverName is the name of the server of the scenarios.
const serverName = "Golden Server"

// serverChunks is how many chunks a client is sent around the spawn at
// goldenRadius.
const serverChunks = 9

// serverIdentity is who a client of a server scenario logs in as.
type serverIdentity struct {
	name string
	xuid string
	id   uuid.UUID
	addr net.Addr
}

var (
	goldenIdentity  = serverIdentity{name: goldenName, xuid: goldenXUID, id: goldenUUID, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}}
	visitorIdentity = serverIdentity{name: "Visitor", xuid: "2535400000000043", id: uuid.MustParse("00000000-0000-4000-8000-000000005157"), addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 19132}}
)

// identityConn is a goldenConn of a client that logs in as an identity of
// its own. If startErr is set, the game of the client does not start:
// StartGameContext returns it, as it does for a client that went away.
type identityConn struct {
	*goldenConn
	who      serverIdentity
	startErr error
}

func (c *identityConn) StartGameContext(ctx context.Context, data netconn.GameData) error {
	if c.startErr != nil {
		return c.startErr
	}
	return c.goldenConn.StartGameContext(ctx, data)
}

func (c *identityConn) IdentityData() login.Identity {
	return login.Identity{Identity: c.who.id.String(), DisplayName: c.who.name, XUID: c.who.xuid}
}

func (c *identityConn) RemoteAddr() net.Addr { return c.who.addr }

// serverListener is the session.ConnListener of the scenarios: it hands the
// server the connections the test passes it, and records what the server
// disconnects through it.
type serverListener struct {
	goldenListener
	mu           sync.Mutex
	disconnected []string
}

func (l *serverListener) Disconnect(c session.Conn, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.disconnected = append(l.disconnected, fmt.Sprintf("%s: %q", c.IdentityData().DisplayName, reason))
	return c.Close()
}

// goldenPlayers is the player.Provider of a server scenario: it keeps what
// the server saves of a player in memory, gives it back when the player
// joins again, and logs both.
type goldenPlayers struct {
	mu     sync.Mutex
	saved  map[uuid.UUID]player.Config
	log    []string
	closed bool
	// saves receives the name of each player saved.
	saves chan string
}

func newGoldenPlayers() *goldenPlayers {
	return &goldenPlayers{saved: map[uuid.UUID]player.Config{}, saves: make(chan string, 16)}
}

func (p *goldenPlayers) Save(id uuid.UUID, data player.Config, w *world.World) error {
	p.mu.Lock()
	p.saved[id] = data
	p.log = append(p.log, "saved "+savedLine(data, w))
	p.mu.Unlock()
	p.saves <- data.Name
	return nil
}

func (p *goldenPlayers) Load(id uuid.UUID, dim func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.saved[id]
	if !ok {
		p.log = append(p.log, fmt.Sprintf("loaded %s: none saved", id))
		return player.Config{}, nil, errors.New("no data saved")
	}
	p.log = append(p.log, fmt.Sprintf("loaded %s: %s", id, data.Name))
	return data, dim(world.Overworld), nil
}

func (p *goldenPlayers) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.log = append(p.log, "provider closed")
	return nil
}

// take returns what the provider logged since it was last taken.
func (p *goldenPlayers) take() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.log
	p.log = nil
	return out
}

// savedLine describes what the server saved of a player: what a player
// joining again gets back, without the fields that count ticks or are drawn
// at random.
func savedLine(d player.Config, w *world.World) string {
	mode, _ := world.GameModeID(d.GameMode)
	return fmt.Sprintf("%s (%s, %s): in %v at %v rotation %v, game mode %d, health %v/%v, food %v, saturation %v, experience %v, held slot %v",
		d.Name, d.XUID, d.UUID, w.Dimension(), d.Position, d.Rotation, mode, d.Health, d.MaxHealth, d.Food, d.Saturation, d.Experience, d.HeldSlot)
}

// serverScenario is a server.Server the clients of a scenario join, and the
// record of what they were sent and what the server did.
type serverScenario struct {
	t       *testing.T
	name    string
	dir     string
	srv     *server.Server
	l       *serverListener
	players *goldenPlayers
	loader  *world.Loader
	brief   bool

	out  strings.Builder
	norm *normaliser

	// mu guards the clients and their markers, which the loop body of
	// Accept reads on the goroutine of the world.
	mu sync.Mutex
	// clients are the clients that connected, in the order they did.
	clients []*serverClient
	markers int
	// yielded are the names of the players Accept yielded, in order.
	yielded []string

	// spawned receives each player Accept yields, once the loop body wrote
	// its markers; acceptDone is closed once Accept ends.
	spawned    chan *world.EntityHandle
	acceptDone chan struct{}
	closed     bool
}

// serverClient is a client of a server scenario: its connection and what
// was recorded of it.
type serverClient struct {
	s    *serverScenario
	who  serverIdentity
	conn *identityConn
	// rec records the packets the client was sent, as the harness does.
	rec *scenario
	// h is the handle of the player of the client once it spawned, and
	// marker the marker the client's stream is read up to; both are
	// guarded by the mutex of the scenario.
	h      *world.EntityHandle
	marker string
	// pending are the packets read but not yet recorded in a step.
	pending []proto.Packet
	// gone is set once the client was sent a Disconnect: what it is sent
	// after is not recorded. It is guarded by the mutex of the scenario.
	gone bool
}

// serverConfig changes how the server of a scenario is made.
type serverConfig struct {
	// user changes the user configuration the server is made of, and
	// bedrock the part of the configuration of the Bedrock adapter.
	user    func(uc *server.UserConfig)
	bedrock func(uc *bedrockadapter.UserConfig)
	// network keeps the listener the program makes, a NetherNet listener,
	// beside the listener of the test.
	network bool
	// brief records the registries a client is sent as it joins, the
	// blocks and items of the game data and every packet but
	// AvailableCommands whose encoding is written as its SHA-256, by that
	// SHA-256 alone, without their fields: the scenario of the join shows
	// them, and any changed byte still changes the snapshot.
	brief bool
}

// newServerScenario makes a server as the program does, from a
// server.UserConfig, with its files in a directory of the test, its world
// as the scenarios of the harness set theirs, a player provider that keeps
// what it saves in memory, and the listener of the test in place of the
// network. It loads the chunks around the spawn, registers the commands of
// the server and starts listening and accepting.
func newServerScenario(t *testing.T, name string, conf serverConfig) *serverScenario {
	t.Helper()
	s := &serverScenario{t: t, name: name, dir: t.TempDir(), norm: newNormaliser(), players: newGoldenPlayers(), brief: conf.brief,
		spawned: make(chan *world.EntityHandle, 4), acceptDone: make(chan struct{})}
	s.l = &serverListener{goldenListener: goldenListener{conns: make(chan session.Conn, 4), closed: make(chan struct{})}}

	uc, bu := server.DefaultConfig(), bedrockadapter.DefaultConfig()
	uc.Server.Name = serverName
	bu.Server.AuthEnabled = false
	uc.World.SaveData = false
	uc.World.Seed = goldenSeed
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Players.SaveData = false
	uc.Players.MaximumChunkRadius = goldenRadius
	bu.Resources.AutoBuildPack = false
	bu.Resources.Folder = filepath.Join(s.dir, "resources")
	uc.Whitelist.File = filepath.Join(s.dir, "white-list.txt")
	bu.Network.IdentityFile = filepath.Join(s.dir, "server-key.pem")
	if conf.user != nil {
		conf.user(&uc)
	}
	if conf.bedrock != nil {
		conf.bedrock(&bu)
	}
	// As readConfig composes them: the part of the adapter first, and its
	// listener first.
	bc, err := bu.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	c, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	c.Log = goldenLog()
	test := func(c server.Config, _ server.Host) (server.Listener, error) {
		return session.Listen(s.l, session.ListenConfig{Log: c.Log, EmoteChatMuted: bc.MuteEmoteChat, Blocks: c.Blocks}), nil
	}
	if conf.network {
		c.Listeners = append(c.Listeners, bc.Listener, test)
	} else {
		c.Listeners = []func(server.Config, server.Host) (server.Listener, error){test}
	}
	c.PlayerProvider = s.players
	c.RandomTickSpeed = -1
	c.WorldProvider = world.NopProvider{Set: goldenSettings()}
	c.Generator = func(world.Dimension, int64) world.Generator { return goldenGenerator() }
	s.srv = c.New()
	w := s.srv.World()
	w.StopTime()
	w.SetTime(goldenTime)
	if err := goldenRules(w); err != nil {
		t.Fatal(err)
	}
	s.preload()
	t.Cleanup(s.close)

	// As the program does: the commands of the server, which ask the
	// operators list who may run them.
	builtin.Register(s.srv)
	s.srv.Listen()
	go s.accept()
	return s
}

// preload loads the chunks around the spawn and keeps them loaded with a
// loader of the test, which sees nothing.
func (s *serverScenario) preload() {
	s.t.Helper()
	w := s.srv.World()
	var spawn mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		spawn = tx.PlayerSpawnPosition(w.Spawn())
		s.loader = world.NewLoader(goldenRadius+1, w, world.NopViewer{})
		s.loader.Move(tx, spawn)
	})
	centre := world.ChunkPos{int32(spawn[0]) >> 4, int32(spawn[2]) >> 4}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := true
		<-w.Exec(func(tx *world.Tx) {
			s.loader.Load(tx, 64)
			// The loader loads a circle a chunk wider than the square the
			// clients are sent.
			for x := centre[0] - goldenRadius; x <= centre[0]+goldenRadius; x++ {
				for z := centre[1] - goldenRadius; z <= centre[1]+goldenRadius; z++ {
					if col, ok := s.loader.Chunk(world.ChunkPos{x, z}); !ok || !col.Ready() {
						ready = false
					}
				}
			}
		})
		if ready {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatal("the chunks around the spawn did not load")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// accept accepts the players of the server as the program does. The loop
// body runs in the transaction that spawned the player: it ends what the
// spawn sent every player in the world with a marker, the player spawned
// last of all.
func (s *serverScenario) accept() {
	defer close(s.acceptDone)
	for p := range s.srv.Accept() {
		s.mu.Lock()
		// A client that went before its player spawned joins no more.
		var joining *serverClient
		for _, c := range s.clients {
			if c.h == nil && !c.gone && c.who.id == p.UUID() {
				joining = c
				break
			}
		}
		for _, c := range s.clients {
			if c.h == nil {
				continue
			}
			if e, ok := c.h.Entity(p.Tx()); ok {
				e.(*player.Player).Message(c.nextMarkerLocked())
			}
		}
		if joining != nil {
			joining.h = p.H()
			p.Message(joining.nextMarkerLocked())
		}
		s.yielded = append(s.yielded, p.Name())
		s.mu.Unlock()
		s.spawned <- p.H()
	}
}

// online returns the clients whose players spawned and who were not sent a
// Disconnect.
func (s *serverScenario) online() []*serverClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*serverClient
	for _, c := range s.clients {
		if c.h != nil && !c.gone {
			out = append(out, c)
		}
	}
	return out
}

// nextMarkerLocked returns the text of the next marker of the client. The
// mutex of the scenario is held.
func (c *serverClient) nextMarkerLocked() string {
	c.s.markers++
	c.marker = fmt.Sprintf("golden marker %d", c.s.markers)
	return c.marker
}

// handle returns the handle of the player of the client.
func (c *serverClient) handle() *world.EntityHandle {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return c.h
}

// currentMarker returns the marker the client's stream is read up to.
func (c *serverClient) currentMarker() string {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return c.marker
}

// connect hands the server a connection of a client logging in as who,
// without waiting for anything.
func (s *serverScenario) connect(who serverIdentity) *serverClient {
	s.t.Helper()
	return s.connectConn(&identityConn{goldenConn: newGoldenConn(), who: who})
}

// connectConn hands the server the connection passed, without waiting for
// anything.
func (s *serverScenario) connectConn(conn *identityConn) *serverClient {
	s.t.Helper()
	who := conn.who
	c := &serverClient{s: s, who: who, conn: conn}
	c.rec = &scenario{t: s.t, name: s.name, norm: s.norm, stacks: map[stackSlot]int32{}, nextRequest: -1}
	s.mu.Lock()
	s.clients = append(s.clients, c)
	s.mu.Unlock()
	s.l.conns <- c.conn
	return c
}

// join has a client log in as who and records what it is sent until it
// spawned and its chunks are sent, and what the other clients are sent
// meanwhile. It returns once the session reads from the connection.
func (s *serverScenario) join(heading string, who serverIdentity) *serverClient {
	s.t.Helper()
	fmt.Fprintf(&s.out, "== %s\n", heading)
	others := s.online()
	c := s.connect(who)
	if h := s.awaitSpawn(heading); h != c.handle() {
		s.t.Fatalf("%s: %s: the server spawned another player than %s", s.name, heading, who.name)
	}
	s.joined(heading, c, others)
	return c
}

// lastYielded returns the name of the player Accept yielded last.
func (s *serverScenario) lastYielded() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.yielded) == 0 {
		return ""
	}
	return s.yielded[len(s.yielded)-1]
}

// awaitSpawn waits until Accept yields a player.
func (s *serverScenario) awaitSpawn(heading string) *world.EntityHandle {
	s.t.Helper()
	select {
	case h := <-s.spawned:
		return h
	case <-time.After(10 * time.Second):
		s.t.Fatalf("%s: %s: the server did not spawn the player", s.name, heading)
		return nil
	}
}

// joined records the join of c, whose player just spawned: the game the
// server started for it and what it was written before its session, and
// then what spawnSent records.
func (s *serverScenario) joined(heading string, c *serverClient, others []*serverClient) {
	s.t.Helper()
	c.conn.mu.Lock()
	data := c.conn.data
	c.conn.mu.Unlock()
	if data == nil {
		s.t.Fatalf("%s: the server did not start the game of %s", s.name, c.who.name)
	}
	dump := gameDataDump(*data)
	if s.brief {
		dump = briefGameData(*data)
	}
	fmt.Fprintf(&s.out, "-- StartGameContext of %s\n>> GameData\n%s", c.who.name, dump)
	s.spawnSent(c, others)
	s.awaitReader(c)
}

// spawnSent records what the spawn of c, whose player just spawned, sent
// each client, and what the ticks after sent until the client's chunks and
// the abilities sent again are in.
func (s *serverScenario) spawnSent(c *serverClient, others []*serverClient) {
	s.t.Helper()
	// The spawn ended with a marker written in the transaction of Accept.
	for _, o := range append(others, c) {
		fmt.Fprintf(&s.out, "-- %s, the session made and the player spawned\n", o.who.name)
		o.readMarker()
		// The others were sent what ticks sent since the last step too.
		o.write(o.take(), o != c)
	}
	// The ticks after: the chunks and the abilities sent again.
	chunks, resent := 0, 0
	c.readUntil(func(pk proto.Packet) bool {
		switch pk.(type) {
		case *v2193.LevelChunkPacket:
			chunks++
		case *v2193.UpdateAbilitiesPacket:
			resent++
		}
		return chunks >= serverChunks && resent >= 1
	})
	s.endStep(append(others, c), true)
	s.providerLog()
}

// awaitReader waits until the session of c reads from its connection.
func (s *serverScenario) awaitReader(c *serverClient) {
	s.t.Helper()
	select {
	case <-c.conn.ready:
	case <-time.After(10 * time.Second):
		s.t.Fatalf("%s: the session of %s does not read", s.name, c.who.name)
	}
}

// settle lets the world tick a few times, so that whatever the ticks
// still had to send is sent before the step ends.
func (s *serverScenario) settle() {
	s.t.Helper()
	w := s.srv.World()
	start := w.CurrentTick()
	deadline := time.Now().Add(10 * time.Second)
	for w.CurrentTick() < start+settleTicks {
		if time.Now().After(deadline) {
			s.t.Fatalf("%s: the world does not tick", s.name)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settleTicks is how many ticks settle waits for: more than the twenty
// after which a session sends the enums of its commands that changed, as
// the names of the players online.
const settleTicks = 21

// step runs f in a transaction of the world of the server, with the player
// of each client, and records what the clients are sent under the heading.
func (s *serverScenario) step(heading string, f func(tx *world.Tx)) {
	s.t.Helper()
	fmt.Fprintf(&s.out, "== %s\n", heading)
	<-s.srv.World().Exec(f)
	s.endStep(s.online(), false)
	s.providerLog()
}

// endStep ends a step of the clients passed with a marker written through
// each in one transaction, and records what each was sent until it.
// resend is set for a step of the ticks after a spawn, whose UpdateAbilities
// packets are moved to the end of the step.
func (s *serverScenario) endStep(clients []*serverClient, resend bool) {
	s.t.Helper()
	s.settle()
	missing := ""
	<-s.srv.World().Exec(func(tx *world.Tx) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, c := range clients {
			e, ok := c.h.Entity(tx)
			if !ok {
				missing = c.who.name
				continue
			}
			e.(*player.Player).Message(c.nextMarkerLocked())
		}
	})
	if missing != "" {
		s.t.Fatalf("%s: %s is not in the world", s.name, missing)
	}
	for _, c := range clients {
		c.readMarker()
		pks := c.take()
		var resent []proto.Packet
		if resend {
			pks = slices.DeleteFunc(pks, func(pk proto.Packet) bool {
				if _, ok := pk.(*v2193.UpdateAbilitiesPacket); ok {
					resent = append(resent, pk)
					return true
				}
				return false
			})
		}
		fmt.Fprintf(&s.out, "-- %s\n", c.who.name)
		c.write(pks, true)
		if len(resent) > 0 {
			fmt.Fprintf(&s.out, "-- %s, the abilities sent again by the timer of the session\n", c.who.name)
			c.write(resent, true)
		}
	}
}

// providerLog records what the player provider did since it was last
// recorded, if anything.
func (s *serverScenario) providerLog() {
	if log := s.players.take(); len(log) > 0 {
		s.out.WriteString("-- player provider\n")
		for _, line := range log {
			fmt.Fprintf(&s.out, "    %s\n", line)
		}
	}
}

// server records what the server tells of its players.
func (s *serverScenario) server() {
	s.t.Helper()
	s.out.WriteString("-- server\n")
	var names []string
	for p := range s.srv.Players(nil) {
		names = append(names, p.Name())
	}
	slices.Sort(names)
	fmt.Fprintf(&s.out, "    players online: %d %q\n", s.srv.PlayerCount(), names)
	s.mu.Lock()
	var whos []serverIdentity
	for _, c := range s.clients {
		if !slices.Contains(whos, c.who) {
			whos = append(whos, c.who)
		}
	}
	s.mu.Unlock()
	for _, who := range whos {
		_, byID := s.srv.Player(who.id)
		_, byName := s.srv.PlayerByName(who.name)
		_, byXUID := s.srv.PlayerByXUID(who.xuid)
		fmt.Fprintf(&s.out, "    %s: by UUID %v, by name %v, by XUID %v\n", who.name, byID, byName, byXUID)
	}
	s.l.mu.Lock()
	for _, d := range s.l.disconnected {
		fmt.Fprintf(&s.out, "    disconnected by the listener: %s\n", d)
	}
	s.l.disconnected = nil
	s.l.mu.Unlock()
}

// awaitSave waits until the player provider saved the player of the name.
func (s *serverScenario) awaitSave(name string) {
	s.t.Helper()
	select {
	case got := <-s.players.saves:
		if got != name {
			s.t.Fatalf("%s: the server saved %s, not %s", s.name, got, name)
		}
	case <-time.After(10 * time.Second):
		s.t.Fatalf("%s: the server did not save %s", s.name, name)
	}
}

// readMarker reads what the client was sent until its marker, keeping it
// to be taken.
func (c *serverClient) readMarker() {
	c.s.t.Helper()
	marker := c.currentMarker()
	c.readUntil(func(pk proto.Packet) bool {
		text, ok := pk.(*v2193.TextPacket)
		if ok && text.Message == marker {
			c.pending = c.pending[:len(c.pending)-1]
			return true
		}
		return false
	})
}

// readUntil reads what the client was sent, keeping it to be taken, until
// done reports true for a packet read.
func (c *serverClient) readUntil(done func(pk proto.Packet) bool) {
	c.s.t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case pk := <-c.conn.written:
			c.pending = append(c.pending, pk)
			if done(pk) {
				return
			}
		case <-timer.C:
			names := make([]string, len(c.pending))
			for i, pk := range c.pending {
				names[i] = packetName(pk)
			}
			c.s.t.Fatalf("%s: %s was not sent what the step waits for, only %v", c.s.name, c.who.name, names)
		}
	}
}

// readClosed reads what the client was sent until the server closed its
// connection and nothing more can be written to it, keeping it to be
// taken.
func (c *serverClient) readClosed() {
	c.s.t.Helper()
	select {
	case <-c.conn.closed:
	case <-time.After(10 * time.Second):
		c.s.t.Fatalf("%s: the server did not close the connection of %s", c.s.name, c.who.name)
	}
	// A flusher that saw the connection open may still be writing to it.
	time.Sleep(timerSettle)
	for {
		select {
		case pk := <-c.conn.written:
			c.pending = append(c.pending, pk)
		default:
			return
		}
	}
}

// left marks the client gone, as one whose connection closed: it is in no
// step after.
func (c *serverClient) left() {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.gone = true
}

// player returns the player of the client in a transaction.
func (c *serverClient) player(tx *world.Tx) *player.Player {
	c.s.t.Helper()
	e, ok := c.handle().Entity(tx)
	if !ok {
		c.s.t.Fatalf("%s: %s is not in the world", c.s.name, c.who.name)
	}
	return e.(*player.Player)
}

// take returns the packets read and not yet recorded.
func (c *serverClient) take() []proto.Packet {
	out := c.pending
	c.pending = nil
	return out
}

// write records packets the client was sent. Those of what ticks sent have
// the weather the world shows every twenty ticks left out; what follows a
// Disconnect is left out.
func (c *serverClient) write(pks []proto.Packet, ticked bool) {
	c.s.t.Helper()
	for _, pk := range pks {
		if c.gone {
			c.rec.norm.packet(reflect.ValueOf(decodeAs(c.s.t, pk.ID(), encode(pk))).Elem())
			continue
		}
		if ticked && standingWeather(pk) {
			continue
		}
		raw := encode(pk)
		back := decodeAs(c.s.t, pk.ID(), raw)
		c.rec.trackStacks(back)
		desc := c.rec.describe(back, raw, ">>")
		if _, commands := pk.(*v2193.AvailableCommandsPacket); c.s.brief && len(raw) > maxHexBytes && !commands {
			// The line of the packet and that of the SHA-256 of its
			// encoding.
			lines := strings.Split(strings.TrimSuffix(desc, "\n"), "\n")
			desc = lines[0] + "\n" + lines[len(lines)-1] + "\n"
		}
		c.s.out.WriteString(desc)
		if _, ok := pk.(*v2193.DisconnectPacket); ok {
			c.s.mu.Lock()
			c.gone = true
			c.s.mu.Unlock()
		}
	}
}

// standingWeather reports whether a packet is half of the weather the world
// shows its viewers every twenty ticks while it neither rains nor thunders.
func standingWeather(pk proto.Packet) bool {
	ev, ok := pk.(*v2193.LevelEventPacket)
	if !ok || ev.EventData != 0 || ev.Position != ([3]float32{}) {
		return false
	}
	return ev.EventType == v2193.LevelEventStopRaining || ev.EventType == v2193.LevelEventStopThunderstorm
}

// close closes the server if the scenario did not.
func (s *serverScenario) close() {
	if !s.closed {
		s.closed = true
		_ = s.srv.Close()
	}
}

// snapshot compares the record of the scenario with its snapshot, or writes
// it with -update-snapshots.
func (s *serverScenario) snapshot() {
	s.t.Helper()
	rec := &scenario{t: s.t, name: s.name}
	rec.out.WriteString(s.out.String())
	rec.snapshot()
}

// briefGameData returns the game data as gameDataDump does, but for its
// blocks and items, of which it gives the number and the SHA-256 of their
// rendering.
func briefGameData(data netconn.GameData) string {
	items := slices.Clone(data.Items)
	slices.SortFunc(items, func(a, b v2193.ItemEntry) int { return strings.Compare(a.Name, b.Name) })
	summary := func(label string, v any, n int) string {
		var b strings.Builder
		writeValue(&b, "", "", reflect.ValueOf(v))
		return fmt.Sprintf("      %s: (%d) rendered sha256 %x\n", label, n, sha256Of(b.String()))
	}
	blocks := summary("Blocks", data.Blocks, len(data.Blocks))
	itemLine := summary("Items", items, len(items))
	data.Blocks, data.Items = nil, nil
	out := gameDataDump(data)
	out = strings.Replace(out, "      Blocks: []\n", blocks, 1)
	return strings.Replace(out, "      Items: []\n", itemLine, 1)
}

// sha256Of returns the SHA-256 of a rendering.
func sha256Of(rendering string) [32]byte { return sha256.Sum256([]byte(rendering)) }
