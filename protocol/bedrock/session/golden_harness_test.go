package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"maps"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// The golden tests bring up a player as the server does, on a connection
// that records every packet the session writes, drive it through the game
// API and through packets a client sends, and compare what was written,
// byte for byte, with a snapshot in testdata/golden. They are the net under
// changes that must not change what Bedrock clients receive: any changed
// byte of any packet changes a snapshot. Run with -update-snapshots to
// write the snapshots again, and read the diff before committing it.

// isolated runs the golden test passed in a process of its own, the test
// binary run again for it alone, and reports whether it did: the test then
// returns, its process having done the work. Other tests of the package
// register recipes and the like for the whole process, which the client is
// sent, so a golden test run after them would see what they left.
func isolated(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GOLDEN_CHILD") == t.Name() {
		return false
	}
	readGoldenSnapshots(t)
	args := []string{"-test.run=^" + t.Name() + "$", "-test.count=1", "-test.timeout=5m"}
	if *updateSnapshots {
		args = append(args, "-update-snapshots")
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GOLDEN_CHILD="+t.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s in a process of its own: %v\n%s", t.Name(), err, out)
	}
	return true
}

// readGoldenSnapshots reads every snapshot of testdata/golden in the process
// that runs the go test command. The scenario's own process compares with
// them, and Go caches a test result on the files the test process opened
// only: read here too, a changed snapshot runs the tests again.
func readGoldenSnapshots(t *testing.T) {
	t.Helper()
	dir := filepath.Join("testdata", "golden")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// goldenName, goldenUUID and goldenXUID are who the client of every scenario
// logs in as.
const (
	goldenName = "Golden"
	goldenXUID = "2535400000000042"
)

var goldenUUID = uuid.MustParse("00000000-0000-4000-8000-00000000901d")

// goldenSeed is the seed of the worlds of the scenarios. goldenTime is the
// time they stand still at, and goldenRadius the chunk radius the client
// asks for.
const (
	goldenSeed   = 20261103
	goldenTime   = 6000
	goldenRadius = 1
)

// goldenSkin is the skin the client logs in with: a 64x32 texture of a
// pattern, a skin ID of its own so that the session does not make one up.
var goldenSkin = func() []byte {
	pix := make([]byte, 64*32*4)
	for i := range pix {
		pix[i] = byte(i * 7)
	}
	return pix
}()

// goldenConn is a session.Conn of a client that logs in as goldenName. It
// keeps the packets the session writes, in order, and hands the session
// the packets the test sends as a client does.
type goldenConn struct {
	// written receives every packet the session writes.
	written chan proto.Packet
	// in holds the packets the client sends; ready tells, each time the
	// session reads, that the packet sent before is in its inbox.
	in    chan proto.Packet
	ready chan struct{}

	closeOnce sync.Once
	closed    chan struct{}
	// eof is closed once the session read the end of the connection.
	eofOnce sync.Once
	eof     chan struct{}

	mu   sync.Mutex
	data *netconn.GameData
}

func newGoldenConn() *goldenConn {
	return &goldenConn{
		written: make(chan proto.Packet, 1<<16),
		in:      make(chan proto.Packet),
		ready:   make(chan struct{}, 1),
		closed:  make(chan struct{}),
		eof:     make(chan struct{}),
	}
}

func (c *goldenConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (*goldenConn) IdentityData() login.Identity {
	return login.Identity{Identity: goldenUUID.String(), DisplayName: goldenName, XUID: goldenXUID}
}

func (*goldenConn) ClientData() login.ClientData {
	return login.ClientData{
		LanguageCode:    "en_GB",
		SkinID:          "golden-skin",
		PlayFabID:       "golden-playfab",
		SkinData:        base64.StdEncoding.EncodeToString(goldenSkin),
		SkinImageWidth:  64,
		SkinImageHeight: 32,
	}
}

func (*goldenConn) ClientCacheEnabled() bool { return false }
func (*goldenConn) ChunkRadius() int         { return goldenRadius }
func (*goldenConn) Latency() time.Duration   { return 0 }
func (*goldenConn) Flush() error             { return nil }
func (*goldenConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}
}

// ReadPacket hands the session the next packet the client sends. Each call
// first tells the test that the packet read before is in the inbox of the
// session, which reads the next one only once it put it there.
func (c *goldenConn) ReadPacket() (proto.Packet, error) {
	select {
	case c.ready <- struct{}{}:
	default:
	}
	select {
	case pk := <-c.in:
		return pk, nil
	case <-c.closed:
		c.eofOnce.Do(func() { close(c.eof) })
		return nil, io.EOF
	}
}

func (c *goldenConn) WritePacket(pk proto.Packet) error {
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
	}
	c.written <- pk
	return nil
}

func (c *goldenConn) StartGameContext(_ context.Context, data netconn.GameData) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = &data
	return nil
}

// goldenLog returns the logger of the sessions of the scenarios: none, or
// at the debug level on standard error if GOLDEN_DEBUG is set, which shows
// why the session refused what a client sent.
func goldenLog() *slog.Logger {
	if os.Getenv("GOLDEN_DEBUG") == "" {
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// goldenSettings returns the settings of a world of the scenarios: the time
// and weather stand still, so that nothing a client is sent depends on how
// long a test took.
func goldenSettings() *world.Settings {
	return &world.Settings{
		Name:               "Golden",
		Seed:               goldenSeed,
		SeedSet:            true,
		Spawn:              cube.Pos{0, -60, 0},
		Time:               goldenTime,
		TimeCycle:          false,
		WeatherCycle:       false,
		DefaultGameMode:    world.GameModeSurvival,
		Difficulty:         world.DifficultyNormal,
		TickRange:          6,
		FallDamage:         true,
		RequiredSleepTicks: 100,
	}
}

// goldenProvider is the provider of a world of the scenarios: it saves
// nothing but maps, which it keeps in memory, numbered from 1.
type goldenProvider struct {
	world.NopProvider
	mu   sync.Mutex
	maps map[int64]world.MapData
}

func (p *goldenProvider) LoadMap(id int64) (world.MapData, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.maps[id]
	if !ok {
		return world.MapData{}, fmt.Errorf("no map %d", id)
	}
	return data, nil
}

func (p *goldenProvider) StoreMap(data world.MapData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.maps == nil {
		p.maps = map[int64]world.MapData{}
	}
	p.maps[data.ID] = data
	return nil
}

func (p *goldenProvider) AllocateMapID() (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return int64(len(p.maps) + 1), nil
}

func (*goldenProvider) JavaMapID(nativeID int64) (int32, error) { return int32(nativeID), nil }
func (*goldenProvider) NativeMapID(javaID int32) (int64, error) { return int64(javaID), nil }

// goldenRules sets the game rules of a world of the scenarios: players
// spawn right at the spawn, not at random around it.
func goldenRules(w *world.World) error {
	return w.SetGameRule("respawn_radius", int64(0))
}

// goldenGenerator returns the flat generator of the worlds of the
// scenarios: grass on two layers of dirt on bedrock, at the bottom of the
// world.
func goldenGenerator() world.Generator {
	return generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
}

// scenario is a player of a session on a goldenConn, in a world of its own
// that ticks only when the scenario says, and the record of what its
// client was sent.
type scenario struct {
	t    *testing.T
	name string
	w    *world.World
	conn *goldenConn
	sess *session.Session
	h    *world.EntityHandle

	out     strings.Builder
	norm    *normaliser
	markers int
	// stacks are the stack network IDs of the inventory of the client as it
	// was last sent them, by window and slot, as the server named them.
	stacks map[stackSlot]int32
	// chunks are the positions of the chunks the client was sent, in
	// order, which it then asks the sub-chunks of.
	chunks []v2193.ChunkPos
	// responses are the statuses of the item stack requests the client
	// was answered, in order.
	responses []uint8
	// nextRequest is the ID of the next item stack request of the client:
	// -1, -3 and on, as the client numbers them.
	nextRequest int32
	// window and windowType are of the container last opened.
	window     uint32
	windowType uint8
	// crafting are the recipes the client was sent, which its requests
	// name by their network IDs, and enchantOptions the options of the
	// enchanting table it was last sent.
	crafting       *v2193.CraftingDataPacket
	enchantOptions []v2193.EnchantmentOption
	// normalise, if set, normalises a packet the client was sent before it
	// is recorded, and reports whether it changed any of it, or whether to
	// leave it out of the record. Only the scenarios of
	// TestMenuRequestScenarios set it, for what they draw at random.
	normalise func(pk proto.Packet) (changed, omit bool)
	// stopped is closed once the session stopped.
	stopped chan struct{}
	// ownsWorld is set if the scenario made its world, which it then closes.
	ownsWorld bool
}

// stackSlot is a slot of a window of the client.
type stackSlot struct {
	window uint32
	slot   int32
}

// scenarioConfig changes how a scenario starts.
type scenarioConfig struct {
	// player changes the player.Config the player is made of.
	player func(conf *player.Config)
	// keepJoin records what the client is sent while it joins and in the
	// first tick after.
	keepJoin bool
	// chunks leaves the chunks to the scenario: otherwise the world ticks
	// until the client was sent every chunk around it, unrecorded, before
	// the scenario starts.
	chunks bool
	// world is the world of another scenario for the player to join, which
	// that scenario closes; otherwise the scenario makes a world of its own.
	world *world.World
	// who is who the client logs in as if not goldenIdentity.
	who *serverIdentity
	// session changes the session.Config the session is made of.
	session func(conf *session.Config)
	// joinMessage, unless zero, is written to the chat as the player
	// spawns, as the server writes its join message.
	joinMessage chat.Translation
	// quitMessage, unless zero, is written to the chat as the player
	// leaves, as the server's QuitMessage.
	quitMessage chat.Translation
	// norm is the normaliser of another scenario to name IDs with, so that
	// the record of either names an ID alike.
	norm *normaliser
}

// warmupTicks are the ticks that send every chunk in goldenRadius around
// the spawn, four a tick.
const warmupTicks = 4

// newScenario joins a player to a world of its own as the server does: the
// session is made as a Join of the session's Listener makes it, the player
// as Server.join and createPlayer fill its config, and it is spawned as
// Accept spawns it. The
// client then gets its chunks around the spawn, which are recorded only if
// the config says so.
func newScenario(t *testing.T, name string, conf scenarioConfig) *scenario {
	t.Helper()
	if err := joinServer().err; err != nil {
		t.Fatal(err)
	}
	w := conf.world
	if w == nil {
		w = goldenWorld(t)
	}
	s := &scenario{t: t, name: name, w: w, conn: newGoldenConn(), norm: newNormaliser(), stacks: map[stackSlot]int32{}, nextRequest: -1, stopped: make(chan struct{}), ownsWorld: conf.world == nil}
	if conf.norm != nil {
		s.norm = conf.norm
	}
	var conn session.Conn = s.conn
	id := goldenUUID
	if conf.who != nil {
		conn, id = &identityConn{goldenConn: s.conn, who: *conf.who}, conf.who.id
	}
	var stopOnce sync.Once

	// As the Start of a Join of the session's Listener.
	sconf := session.Config{
		Log:            goldenLog(),
		MaxChunkRadius: goldenRadius,
		BlockRegistry:  w.BlockRegistry(),
		IsOperator:     func(uuid.UUID) bool { return false },
	}
	if conf.session != nil {
		conf.session(&sconf)
	}
	s.sess = sconf.New(conn)

	// As Server.join for a player the provider does not know, then
	// Server.createPlayer.
	var pconf player.Config
	<-w.Exec(func(tx *world.Tx) { pconf.Position = tx.PlayerSpawnPosition(w.Spawn()) })
	pconf.GameMode = w.DefaultGameMode()
	pconf.Name = conn.IdentityData().DisplayName
	pconf.XUID = conn.IdentityData().XUID
	pconf.UUID = id
	pconf.Locale, _ = language.Parse(strings.Replace(conn.ClientData().LanguageCode, "_", "-", 1))
	pconf.Skin = session.ClientSkin(conn.ClientData())
	// The seed of the enchanting table is otherwise random.
	pconf.EnchantmentSeed = 7
	// As the Left hook and the quit message of Server.createPlayer.
	pconf.Left = func(*world.Tx, *player.Player) { stopOnce.Do(func() { close(s.stopped) }) }
	pconf.QuitMessage = conf.quitMessage
	if conf.player != nil {
		conf.player(&pconf)
	}
	pconf.Client = s.sess
	s.h = world.EntitySpawnOpts{Position: pconf.Position, ID: id}.New(player.Type, pconf)
	s.sess.Bind(s.h, pconf.Skin)

	t.Cleanup(s.close)

	// As Server.Accept. The spawn sends the abilities with the permissions,
	// and, as Bedrock ignores abilities sent right after a game type, again
	// a moment later on a timer of the session, which ends the join.
	s.begin("join: the session is made and the player spawned", conf.keepJoin)
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(s.h).(*player.Player)
		s.sess.Spawn(p, tx)
		// As Server.welcome.
		chat.Global.Subscribe(p)
		if !conf.joinMessage.Zero() {
			chat.Global.Writet(conf.joinMessage, p.Name())
		}
	})
	s.sync()
	s.awaitPacket("UpdateAbilities", conf.keepJoin)
	s.awaitPacket("UpdateAbilities", conf.keepJoin)
	s.drain(conf.keepJoin)
	// The session reads what the client sends from now on.
	s.awaitReader()
	if conf.chunks {
		return s
	}
	// The first tick sends the chunks around the spawn.
	s.begin("first tick", conf.keepJoin)
	s.advance()
	s.drain(conf.keepJoin)
	for range warmupTicks - 1 {
		s.advance()
		s.drain(false)
	}
	return s
}

// goldenWorld returns a new world of the scenarios.
func goldenWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{
		Log:             slog.New(slog.DiscardHandler),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       goldenGenerator(),
		Provider:        &goldenProvider{NopProvider: world.NopProvider{Set: goldenSettings()}},
		Seed:            goldenSeed,
	}.New()
	if err := goldenRules(w); err != nil {
		t.Fatal(err)
	}
	return w
}

// close disconnects the player and closes the world of a scenario. Once the
// player quit, the session is out of the list of sessions, which the next
// scenario then starts without.
func (s *scenario) close() {
	s.sync()
	if _, err := world.Call(context.Background(), s.w, func(tx *world.Tx) (struct{}, error) {
		if e, ok := s.h.Entity(tx); ok {
			e.(*player.Player).Disconnect("golden end")
		}
		return struct{}{}, nil
	}); err != nil {
		s.t.Errorf("disconnect: %v", err)
	}
	select {
	case <-s.stopped:
	case <-time.After(goldenDeadline):
		s.t.Errorf("%s: the session did not stop", s.name)
	}
	if s.ownsWorld {
		_ = s.w.Close()
	}
}

// player returns the player of the scenario in a transaction.
func (s *scenario) player(tx *world.Tx) *player.Player {
	s.t.Helper()
	e, ok := s.h.Entity(tx)
	if !ok {
		s.t.Fatalf("%s: the player is not in the world", s.name)
	}
	return e.(*player.Player)
}

// step runs f with the player in a transaction of its world and records what
// the client was sent under the heading.
func (s *scenario) step(heading string, f func(tx *world.Tx, p *player.Player)) {
	s.t.Helper()
	s.begin(heading, true)
	s.run(f)
	s.drain(true)
}

// gameModeStep is step for an f that changes the game mode or flight, after
// which the session sends the abilities again on a timer of its own, 50ms
// later: as many UpdateAbilities as the timers send, resends, are waited
// for before the step ends. f itself must send no UpdateAbilities. A timer of the session runs a transaction of
// its own, so the world is left alone until then.
func (s *scenario) gameModeStep(heading string, resends int, f func(tx *world.Tx, p *player.Player)) {
	s.t.Helper()
	s.begin(heading, true)
	s.run(f)
	for range resends {
		s.awaitPacket("UpdateAbilities", true)
	}
	s.drain(true)
}

// run runs f with the player in a transaction of its world.
func (s *scenario) run(f func(tx *world.Tx, p *player.Player)) {
	s.t.Helper()
	var panicked any
	var stack []byte
	s.sync()
	defer s.sync()
	<-s.w.Exec(func(tx *world.Tx) {
		defer func() {
			if panicked = recover(); panicked != nil {
				stack = debug.Stack()
			}
		}()
		f(tx, s.player(tx))
	})
	if panicked != nil {
		s.t.Fatalf("%s: %v\n%s", s.name, panicked, stack)
	}
}

// advance advances the world of the scenario a tick.
func (s *scenario) advance() {
	s.sync()
	s.w.AdvanceTick()
	s.sync()
}

// sync orders what the test does in the world with the transactions of the
// timers of the session, which run on goroutines of their own: both lock
// the handle of the player, which World does too. The timer that sends the
// abilities again starts its transaction 50ms after it was set, by when the
// transaction that set it ended and synced, and the test waits for what the
// timer sends, and a moment more, before it syncs again and goes on.
func (s *scenario) sync() {
	_ = s.h.World()
}

// tick advances the world of the scenario n ticks, recording what the
// client was sent after each under the heading and the number of the tick.
func (s *scenario) tick(heading string, n int) {
	s.t.Helper()
	for i := 1; i <= n; i++ {
		h := heading
		if n > 1 {
			h = fmt.Sprintf("%s, tick %d", heading, i)
		}
		s.begin(h, true)
		s.advance()
		s.drain(true)
	}
}

// client sends packets as the client does, which the session handles in the
// next tick of the world of the player, and records them and what the
// client was sent in that tick.
func (s *scenario) client(heading string, pks ...proto.Packet) {
	s.t.Helper()
	s.begin(heading, true)
	for _, pk := range pks {
		raw := encode(pk)
		s.out.WriteString(s.describe(decodeAs(s.t, pk.ID(), raw), raw, "<<"))
		s.send(pk)
	}
	s.advance()
	s.drain(true)
}

// send hands the session a packet as the client sends it: encoded and
// decoded again, as the server reads it, and put in the inbox of the
// session.
func (s *scenario) send(pk proto.Packet) {
	s.t.Helper()
	back := decodeAs(s.t, pk.ID(), encode(pk))
	select {
	case s.conn.in <- back:
	case <-time.After(goldenDeadline):
		s.t.Fatalf("%s: the session does not read", s.name)
	}
	s.awaitReader()
}

// awaitReader waits until the session reads again from its connection.
func (s *scenario) awaitReader() {
	s.t.Helper()
	select {
	case <-s.conn.ready:
	case <-time.After(goldenDeadline):
		s.t.Fatalf("%s: the session does not read", s.name)
	}
}

// awaitPacket waits until the client is sent a packet of the name passed
// by a timer of the session, recording what it is sent until then if keep
// is set. The world is left alone while waiting: such a timer runs a
// transaction of its own, which may still write packets after this one, so
// once it was sent the timer is given a moment more to end it.
func (s *scenario) awaitPacket(name string, keep bool) {
	s.t.Helper()
	timer := time.NewTimer(goldenDeadline)
	defer timer.Stop()
	for {
		select {
		case pk := <-s.conn.written:
			if s.record(pk, keep) == name {
				time.Sleep(timerSettle)
				return
			}
		case <-timer.C:
			s.t.Fatalf("%s: no %s was sent", s.name, name)
		}
	}
}

// goldenDeadline is how long the harnesses wait for what a step needs
// before they fail it: the session reading, the server accepting, spawning
// or saving a player, a packet of a timer. Only a failing step waits it out,
// so it is long enough for a runner under load or the race detector, where
// 5 or 10 seconds were not.
const goldenDeadline = 30 * time.Second

// timerSettle is how long a transaction of a timer of the session is given
// to end once it sent what was waited for. It orders nothing recorded:
// what the transaction writes after is recorded in the same step anyway.
const timerSettle = 20 * time.Millisecond

// begin starts a step of the record under a heading if keep is set.
func (s *scenario) begin(heading string, keep bool) {
	if keep {
		fmt.Fprintf(&s.out, "== %s\n", heading)
	}
}

// drain records what the client was sent since the last step if keep is
// set. A message of its own, sent last through the player, marks the end.
func (s *scenario) drain(keep bool) {
	s.t.Helper()
	s.markers++
	marker := fmt.Sprintf("golden marker %d", s.markers)
	s.run(func(_ *world.Tx, p *player.Player) { p.Message(marker) })
	timer := time.NewTimer(goldenDeadline)
	defer timer.Stop()
	for {
		select {
		case pk := <-s.conn.written:
			if text, ok := pk.(*v2193.TextPacket); ok && text.Message == marker {
				return
			}
			s.record(pk, keep)
		case <-timer.C:
			s.t.Fatalf("%s: the session did not send its marker", s.name)
		}
	}
}

// record keeps the stack network IDs of the packet the client was sent and
// writes it to the record if keep is set. It returns the name of the
// packet.
func (s *scenario) record(pk proto.Packet, keep bool) string {
	s.t.Helper()
	raw := encode(pk)
	back := decodeAs(s.t, pk.ID(), raw)
	s.trackStacks(back)
	if keep && normaliseOrbs && orbMotion(back) {
		keep = false
	}
	if keep {
		s.out.WriteString(s.describe(back, raw, ">>"))
	} else {
		// IDs seen while not recording are still named in order, so that
		// the names of the recorded part do not depend on what is left out.
		s.norm.packet(reflect.ValueOf(back).Elem())
	}
	return packetName(back)
}

// describe returns a packet as the snapshot shows it: a line of the
// direction, its name and ID and the size of its encoding, the fields of
// the packet decoded from raw, its encoding, and then raw itself in hex.
// Values that differ from run to run are normalised first, see normaliser:
// the hex of a packet that holds any is that of the normalised packet
// encoded again, which the line marks "normalised".
func (s *scenario) describe(pk proto.Packet, raw []byte, dir string) string {
	v := reflect.ValueOf(pk).Elem()
	s.norm.touched = false
	var omit bool
	if s.normalise != nil {
		var changed bool
		if changed, omit = s.normalise(pk); changed {
			s.norm.touched = true
		}
	}
	s.norm.packet(v)
	if omit {
		return ""
	}
	mark := ""
	if s.norm.touched {
		raw, mark = encode(pk), " normalised"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s id=%d len=%d%s\n", dir, packetName(pk), pk.ID(), len(raw), mark)
	writeValue(&b, "    ", "", v)
	if len(raw) > maxHexBytes {
		sum := sha256.Sum256(raw)
		fmt.Fprintf(&b, "    sha256 %x\n", sum)
	} else {
		for off := 0; off < len(raw); off += 32 {
			fmt.Fprintf(&b, "    | %x\n", raw[off:min(off+32, len(raw))])
		}
	}
	return b.String()
}

// maxHexBytes is the size of the largest encoding written in hex. Larger
// ones, the registries of a join and full chunks, are written as their
// SHA-256 next to the rendering of each of their fields: any changed byte
// still changes the snapshot, and the fields show where.
const maxHexBytes = 4096

// trackStacks keeps the stack network IDs the client was sent, which the
// requests of the client name.
func (s *scenario) trackStacks(pk proto.Packet) {
	switch pk := pk.(type) {
	case *v2193.InventoryContentPacket:
		for i, it := range pk.Content {
			s.stacks[stackSlot{pk.WindowID, int32(i)}] = it.StackNetworkID
		}
	case *v2193.InventorySlotPacket:
		s.stacks[stackSlot{pk.WindowID, int32(pk.Slot)}] = pk.NewItem.StackNetworkID
	case *v2193.LevelChunkPacket:
		s.chunks = append(s.chunks, pk.Position)
	case *v2193.ItemStackResponsePacket:
		for _, r := range pk.Responses {
			s.responses = append(s.responses, r.Status)
			for _, c := range r.ContainerInfo {
				for _, slot := range c.SlotInfo {
					s.stacks[s.stackKey(c.Container.ContainerID, int(slot.Slot))] = slot.StackNetworkID
				}
			}
		}
	case *v2193.ContainerOpenPacket:
		s.window, s.windowType = uint32(pk.WindowID), pk.ContainerType
	case *v2193.CraftingDataPacket:
		s.crafting = pk
	case *v2193.PlayerEnchantOptionsPacket:
		s.enchantOptions = pk.Options
	}
}

// windowOf returns the window of the client that holds a container of
// item stack requests: the inventory, the off-hand, the armour, the
// container opened, or else the UI.
func (s *scenario) windowOf(container uint8) uint32 {
	switch container {
	case v2193.ContainerHotBar, v2193.ContainerInventory, v2193.ContainerCombinedHotBarAndInventory:
		return v2193.WindowIDInventory
	case v2193.ContainerOffhand:
		return v2193.WindowIDOffHand
	case v2193.ContainerArmor:
		return v2193.WindowIDArmour
	case v2193.ContainerLevelEntity, v2193.ContainerShulkerBox, v2193.ContainerBarrel, v2193.ContainerCrafterLevelEntity,
		v2193.ContainerFurnaceIngredient, v2193.ContainerFurnaceFuel, v2193.ContainerFurnaceResult:
		return s.window
	}
	return v2193.WindowIDUI
}

// again returns a slot of a container that an earlier action of the item
// stack request being made changed: the client names it by the ID of the
// request, as the stack it holds has an ID the client was not yet sent.
func (s *scenario) again(container uint8, slot int) v2193.StackRequestSlotInfo {
	return v2193.StackRequestSlotInfo{
		Container:      v2193.FullContainerName{ContainerID: container},
		Slot:           uint8(slot),
		StackNetworkID: s.nextRequest,
	}
}

// slot returns a slot of a container of item stack requests with the stack
// network ID the client was last sent for it.
func (s *scenario) slot(container uint8, slot int) v2193.StackRequestSlotInfo {
	return v2193.StackRequestSlotInfo{
		Container:      v2193.FullContainerName{ContainerID: container},
		Slot:           uint8(slot),
		StackNetworkID: s.stacks[s.stackKey(container, slot)],
	}
}

// stackKey returns the window and slot of the client that a slot of a
// container of item stack requests is. The off hand is a window of one
// slot, which requests name as slot 1.
func (s *scenario) stackKey(container uint8, slot int) stackSlot {
	if container == v2193.ContainerOffhand {
		slot = 0
	}
	return stackSlot{s.windowOf(container), int32(slot)}
}

// snapshot compares the record of a scenario with its snapshot, or writes
// the snapshot with -update-snapshots.
func (s *scenario) snapshot() {
	s.t.Helper()
	path := filepath.Join("testdata", "golden", s.name+".snapshot")
	got := s.out.String()
	if *updateSnapshots {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			s.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("%v: run with -update-snapshots to write it", err)
	}
	if got != string(want) {
		s.t.Errorf("what the client is sent differs from %s (run with -update-snapshots if the change is meant):\n%s", path, snapshotDiff(string(want), got))
	}
}

// snapshotDiff returns the first lines that differ between two snapshots,
// each under the step and packet it is part of.
func snapshotDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	var step, packet string
	for i := 0; i < max(len(w), len(g)) && shown < 8; i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		switch {
		case strings.HasPrefix(wl, "== "):
			step = wl
		case strings.HasPrefix(wl, ">> "), strings.HasPrefix(wl, "<< "):
			packet = wl
		}
		if wl == gl {
			continue
		}
		shown++
		fmt.Fprintf(&b, "line %d, in %q, %q:\n  want: %s\n   got: %s\n", i+1, step, packet, clip(wl), clip(gl))
		if strings.HasPrefix(wl, "== ") != strings.HasPrefix(gl, "== ") {
			b.WriteString("  (the steps no longer line up; later lines are left out)\n")
			break
		}
	}
	if len(w) != len(g) {
		fmt.Fprintf(&b, "the snapshot has %d lines, the run %d\n", len(w), len(g))
	}
	return b.String()
}

// clip shortens a line of a diff.
func clip(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// encode returns the encoding of a packet. A packet shared by sessions is in
// a wrapper of the session, which encodes as the packet.
func encode(pk proto.Packet) []byte {
	var w wire.Writer
	pk.Encode(&w)
	return w.B
}

// decodeAs decodes a packet of the ID passed as a client reads it. Decoding
// and encoding again does not give back every byte: NBT, for one, decodes
// an empty list of compounds as an empty list of nothing.
func decodeAs(t *testing.T, id uint32, raw []byte) proto.Packet {
	t.Helper()
	back := v2193.New(id)
	r := wire.NewReader(raw)
	back.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("packet %d does not decode: %v, %d bytes left", id, r.Err(), r.Len())
	}
	return back
}

// packetName returns the name of a packet without the Packet suffix.
func packetName(pk proto.Packet) string {
	return strings.TrimSuffix(reflect.TypeOf(pk).Elem().Name(), "Packet")
}

// normaliser sets the values of a packet that differ from run to run, and
// only those, to values that do not. Each is named in the order it is first
// seen in a scenario, so that the same ID keeps the same name throughout:
//
//   - Runtime and unique IDs of entities other than the player itself,
//     which come from a counter of the process: fields whose name holds
//     UniqueID or RuntimeID or ends in EntityID, but not those of blocks or
//     the RuntimeID of an item entry. Unique IDs of entities other than
//     players are made of their random UUID. They become 1000 and up. 0
//     and -1 (none) and 1 (the player) stay, as do the IDs of boss bars that the
//     session numbers from -2^62.
//   - Stack network IDs of items, from another counter of the process:
//     fields named StackNetworkID. Positive ones become 1 and up; 0 (no
//     item) and the negative IDs of requests of the client stay.
//   - UUIDs other than that of the player, made at random: those of recipes
//     without a Java ID and of skins and capes, as uuid.UUID fields and as
//     strings. They are numbered for each kind of packet, so that more
//     recipes do not renumber the skins: the first eight hex digits are the
//     CRC-32 of the name of the packet they are first seen in, the last
//     twelve their number.
//   - The Name of an option of the enchanting table, which is drawn from
//     the global random source rather than the seed of the player: it
//     becomes "name".
//   - The Elements of SetHud, which the session collects in a map: they
//     are sorted.
//   - The SerialisedEventData of LevelEventGeneric and the
//     SerialisedOffers of UpdateTrade, NBT the session encodes from maps,
//     whose entries the encoder writes in no order: the entries of each
//     compound are sorted by name.
//   - The containers of an ItemStackResponse and the slots of each, which
//     the session collects in maps: they are sorted by container ID and
//     slot, before any stack network ID in them is named.
//   - The visible effects of entity metadata (EntityDataKeyVisibleMobEffects),
//     seven bits for each effect packed in the order of a map of the effects
//     of the entity: the effects are sorted.
//
// The GameRules and Items of the game data, which come from maps, are
// sorted by name where the game data is written, see gameDataDump.
type normaliser struct {
	entities map[uint64]uint64
	stacks   map[int64]int64
	uuids    map[uuid.UUID]uuid.UUID
	// kind is the name of the packet being normalised, and counts the
	// UUIDs named for each.
	kind  string
	kinds map[string]int
	// touched is set once a value of the packet is one of those
	// normalised, whether or not normalising changed it.
	touched bool
}

func newNormaliser() *normaliser {
	return &normaliser{entities: map[uint64]uint64{}, stacks: map[int64]int64{}, uuids: map[uuid.UUID]uuid.UUID{}, kinds: map[string]int{}}
}

var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

var uuidType = reflect.TypeFor[uuid.UUID]()

// packet normalises the fields of a packet.
func (n *normaliser) packet(v reflect.Value) {
	n.kind = strings.TrimSuffix(v.Type().Name(), "Packet")
	if normaliseOrbs {
		n.orb(v)
	}
	if hud, ok := v.Addr().Interface().(*v2193.SetHudPacket); ok && len(hud.Elements) > 1 {
		slices.Sort(hud.Elements)
		n.touched = true
	}
	if pk, ok := v.Addr().Interface().(*v2193.LevelEventGenericPacket); ok {
		pk.SerialisedEventData = sortedCompound(pk.SerialisedEventData)
		n.touched = true
	}
	if pk, ok := v.Addr().Interface().(*v2193.UpdateTradePacket); ok {
		pk.SerialisedOffers = sortedNBT(pk.SerialisedOffers)
		n.touched = true
	}
	if pk, ok := v.Addr().Interface().(*v2193.ItemStackResponsePacket); ok {
		for _, r := range pk.Responses {
			if len(r.ContainerInfo) > 1 {
				n.touched = true
			}
			slices.SortFunc(r.ContainerInfo, func(a, b v2193.StackResponseContainerInfo) int {
				return int(a.Container.ContainerID) - int(b.Container.ContainerID)
			})
			for _, c := range r.ContainerInfo {
				if len(c.SlotInfo) > 1 {
					n.touched = true
				}
				slices.SortFunc(c.SlotInfo, func(a, b v2193.StackResponseSlotInfo) int { return int(a.Slot) - int(b.Slot) })
			}
		}
	}
	if v.Type() == reflect.TypeFor[v2193.PlayerEnchantOptionsPacket]() {
		opts := v.FieldByName("Options")
		for i := range opts.Len() {
			opts.Index(i).FieldByName("Name").SetString("name")
			n.touched = true
		}
	}
	n.value(v, "")
}

func (n *normaliser) value(v reflect.Value, name string) {
	if v.Type() == uuidType {
		if v.CanSet() {
			v.Set(reflect.ValueOf(n.uuid(v.Interface().(uuid.UUID))))
		}
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			field := v.Field(i)
			fname := f.Name
			// Optional values are a struct of Val and whether it is set:
			// Val goes by the name of the field.
			if fname == "Val" {
				fname = name
			}
			n.value(field, fname)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			n.value(v.Index(i), name)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			n.value(v.Elem(), name)
		}
	case reflect.Map:
		// Maps of the packets the scenarios send hold no IDs: metadata and
		// NBT. Strings in them that are UUIDs are left as they are.
		if name == "EntityMetadata" && v.Type().Key().Kind() == reflect.Uint32 {
			key := reflect.ValueOf(uint32(v2193.EntityDataKeyVisibleMobEffects))
			if packed := v.MapIndex(key); packed.IsValid() && packed.Elem().Kind() == reflect.Int64 && packed.Elem().Int() >= 1<<7 {
				v.SetMapIndex(key, reflect.ValueOf(sortedEffects(packed.Elem().Int())))
				n.touched = true
			}
		}
	case reflect.String:
		if v.CanSet() && uuidPattern.MatchString(v.String()) {
			v.SetString(uuidPattern.ReplaceAllStringFunc(v.String(), func(s string) string {
				id, err := uuid.Parse(s)
				if err != nil {
					return s
				}
				return n.uuid(id).String()
			}))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !v.CanSet() {
			return
		}
		switch {
		case name == "StackNetworkID":
			if id := v.Int(); id > 0 {
				v.SetInt(n.stack(id))
			}
		case name == "BossEntityUniqueID" && v.Int() < bossBarIDs:
			// The session numbers boss bars of their own ID itself.
		case entityIDField(name):
			if id := v.Int(); id != 0 && id != 1 && id != -1 {
				v.SetInt(int64(n.entity(uint64(id))))
			}
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.CanSet() && entityIDField(name) {
			if id := v.Uint(); id > 1 && id != math.MaxUint64 {
				v.SetUint(n.entity(id))
			}
		}
	}
}

// sortedCompound returns the entries of an NBT compound of the network
// encoding, without its header and end, sorted by name at every depth.
func sortedCompound(entries []byte) []byte {
	whole := sortedNBT(append(append([]byte{10, 0}, entries...), 0))
	return whole[2 : len(whole)-1]
}

// sortedNBT returns an NBT compound of the network encoding with the
// entries of every compound in it sorted by name. The encoder writes the
// entries of a map in the order Go ranges over it, but those of a struct
// in the order of its fields: each compound becomes a struct of a field for
// each entry, in the order of their names.
func sortedNBT(raw []byte) []byte {
	var m map[string]any
	if err := nbt.UnmarshalEncoding(raw, &m, nbt.NetworkLittleEndian); err != nil {
		return raw
	}
	out, err := nbt.MarshalEncoding(orderedNBT(m).Interface(), nbt.NetworkLittleEndian)
	if err != nil {
		return raw
	}
	return out
}

// orderedNBT returns an NBT value with each compound in it as a struct of
// its entries in the order of their names.
func orderedNBT(v any) reflect.Value {
	switch v := v.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(v))
		fields := make([]reflect.StructField, len(keys))
		values := make([]reflect.Value, len(keys))
		for i, k := range keys {
			values[i] = orderedNBT(v[k])
			fields[i] = reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: values[i].Type(), Tag: reflect.StructTag(fmt.Sprintf("nbt:%q", k))}
		}
		out := reflect.New(reflect.StructOf(fields)).Elem()
		for i := range values {
			out.Field(i).Set(values[i])
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = orderedNBT(e).Interface()
		}
		return reflect.ValueOf(out)
	}
	return reflect.ValueOf(v)
}

// sortedEffects returns the visible effects of entity metadata, seven bits
// each, sorted.
func sortedEffects(packed int64) int64 {
	var effects []int64
	for ; packed != 0; packed >>= 7 {
		effects = append(effects, packed&0x7f)
	}
	slices.Sort(effects)
	var out int64
	for _, e := range effects {
		out = out<<7 | e
	}
	return out
}

// bossBarIDs is above the IDs a session gives boss bars of an ID of their
// own, which start at -2^62.
const bossBarIDs = -(1 << 62) + (1 << 32)

// entityIDField reports whether a field of the name passed holds the ID of
// an entity.
func entityIDField(name string) bool {
	if strings.Contains(name, "Block") || name == "RuntimeID" {
		// Blocks and items go by runtime IDs too.
		return false
	}
	return strings.Contains(name, "UniqueID") || strings.Contains(name, "RuntimeID") || strings.HasSuffix(name, "EntityID")
}

func (n *normaliser) entity(id uint64) uint64 {
	n.touched = true
	if to, ok := n.entities[id]; ok {
		return to
	}
	to := uint64(1000 + len(n.entities))
	n.entities[id] = to
	return to
}

func (n *normaliser) stack(id int64) int64 {
	n.touched = true
	if to, ok := n.stacks[id]; ok {
		return to
	}
	to := int64(1 + len(n.stacks))
	n.stacks[id] = to
	return to
}

func (n *normaliser) uuid(id uuid.UUID) uuid.UUID {
	if id == goldenUUID || id == uuid.Nil {
		return id
	}
	n.touched = true
	if to, ok := n.uuids[id]; ok {
		return to
	}
	n.kinds[n.kind]++
	var to uuid.UUID
	binary.BigEndian.PutUint32(to[:4], crc32.ChecksumIEEE([]byte(n.kind)))
	binary.BigEndian.PutUint32(to[12:], uint32(n.kinds[n.kind]))
	n.uuids[id] = to
	return to
}

// maxInline is the width of the longest value written on one line.
const maxInline = 160

// writeValue writes a value under a label: on one line if it is short,
// otherwise one line for each of its fields or elements.
func writeValue(b *strings.Builder, indent, label string, v reflect.Value) {
	if val, set, ok := optional(v); ok {
		if !set {
			fmt.Fprintf(b, "%s%snone\n", indent, label)
			return
		}
		v = val
	}
	if line := inline(v); len(line) <= maxInline || !expandable(v) {
		if len(line) > maxUnclipped {
			// Long lists of numbers, as the pixels of a map.
			writeClipped(b, indent, label, v)
			return
		}
		fmt.Fprintf(b, "%s%s%s\n", indent, label, line)
		return
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	fmt.Fprintf(b, "%s%s%s\n", indent, label, typeName(v))
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.IsExported() {
				writeValue(b, indent+"  ", f.Name+": ", v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			label := fmt.Sprintf("[%d] ", i)
			if v.Len() > maxExpanded {
				writeClipped(b, indent+"  ", label, v.Index(i))
				continue
			}
			writeValue(b, indent+"  ", label, v.Index(i))
		}
	case reflect.Map:
		for _, k := range sortedKeys(v) {
			label := inline(k) + ": "
			if v.Len() > maxExpanded {
				writeClipped(b, indent+"  ", label, v.MapIndex(k))
				continue
			}
			writeValue(b, indent+"  ", label, v.MapIndex(k))
		}
	}
}

// maxExpanded is the length of the longest list or map whose elements are
// written over as many lines as they take. The elements of longer ones,
// the registries a client is sent as it joins, are written on one line
// each, see writeClipped.
const maxExpanded = 128

// maxUnclipped is the width of the longest value with no parts to write on
// lines of their own that is written as it is.
const maxUnclipped = 2048

// maxClipped is the width of the longest element of a long list written as
// it is.
const maxClipped = 240

// writeClipped writes an element of a long list on one line: as it is if
// short, else its start and the start of the SHA-256 of all of it, so that
// a change of any part of it still shows on its line.
func writeClipped(b *strings.Builder, indent, label string, v reflect.Value) {
	line := inline(v)
	if len(line) > maxClipped {
		sum := sha256.Sum256([]byte(line))
		line = fmt.Sprintf("%s… <%d chars sha256:%x>", line[:maxClipped], len(line), sum[:8])
	}
	fmt.Fprintf(b, "%s%s%s\n", indent, label, line)
}

// optional returns the value of an optional value of the protocol, a
// struct of Set and Val, and whether it is set. ok is false for any other
// value. Val of an optional not set is not encoded, so it is not written.
func optional(v reflect.Value) (val reflect.Value, set, ok bool) {
	if v.Kind() != reflect.Struct || v.NumField() != 2 || v.Type().Field(0).Name != "Set" || v.Type().Field(1).Name != "Val" || v.Field(0).Kind() != reflect.Bool {
		return reflect.Value{}, false, false
	}
	return v.Field(1), v.Field(0).Bool(), true
}

// expandable reports whether a value has parts to write on lines of their
// own.
func expandable(v reflect.Value) bool {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Map:
		return v.Type() != uuidType
	case reflect.Slice, reflect.Array:
		return !numeric(v.Type().Elem())
	}
	return false
}

// numeric reports whether a type is a number or lists of numbers, such as
// height maps, which are written on one line however long.
func numeric(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return numeric(t.Elem())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// typeName returns the name of the type of a value to head its parts.
func typeName(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return fmt.Sprintf("%s (%d)", v.Type().String(), v.Len())
	case reflect.Map:
		return fmt.Sprintf("map (%d)", v.Len())
	}
	return v.Type().Name()
}

// inline returns a value on one line. Pointers are followed, maps are in
// the order of their keys, floats as exact as Go writes them, and byte
// slices in hex, or as their size and the start of their SHA-256 when long.
func inline(v reflect.Value) string {
	var b strings.Builder
	writeInline(&b, v)
	return b.String()
}

func writeInline(b *strings.Builder, v reflect.Value) {
	if !v.IsValid() {
		b.WriteString("<nil>")
		return
	}
	if v.Type() == uuidType {
		b.WriteString(v.Interface().(uuid.UUID).String())
		return
	}
	if val, set, ok := optional(v); ok {
		if !set {
			b.WriteString("none")
			return
		}
		v = val
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			b.WriteString("<nil>")
			return
		}
		if v.Kind() == reflect.Pointer {
			b.WriteByte('&')
		}
		writeInline(b, v.Elem())
	case reflect.Struct:
		b.WriteByte('{')
		first := true
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			if !first {
				b.WriteByte(' ')
			}
			first = false
			b.WriteString(f.Name)
			b.WriteByte(':')
			writeInline(b, v.Field(i))
		}
		b.WriteByte('}')
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			raw := make([]byte, v.Len())
			reflect.Copy(reflect.ValueOf(raw), v)
			if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') && json.Valid(raw) {
				// JSON, as of forms, as the text it is.
				b.WriteString("json" + strconv.Quote(string(raw)))
				return
			}
			if len(raw) > 3 && raw[0] == 10 {
				// NBT, as of trade offers, decoded, if it is NBT the
				// scenarios write as it is.
				var m map[string]any
				if err := nbt.UnmarshalEncoding(raw, &m, nbt.NetworkLittleEndian); err == nil && string(sortedNBT(raw)) == string(raw) {
					b.WriteString("nbt")
					writeInline(b, reflect.ValueOf(m))
					return
				}
			}
			if len(raw) <= 48 {
				b.WriteString("0x" + hex.EncodeToString(raw))
			} else {
				sum := sha256.Sum256(raw)
				fmt.Fprintf(b, "<%d bytes sha256:%x>", len(raw), sum[:8])
			}
			return
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i := range v.Len() {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeInline(b, v.Index(i))
		}
		b.WriteByte(']')
	case reflect.Map:
		b.WriteString("map[")
		for i, k := range sortedKeys(v) {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeInline(b, k)
			b.WriteByte(':')
			writeInline(b, v.MapIndex(k))
		}
		b.WriteByte(']')
	case reflect.String:
		b.WriteString(strconv.Quote(v.String()))
	case reflect.Float32:
		writeFloat(b, v.Float(), 32)
	case reflect.Float64:
		writeFloat(b, v.Float(), 64)
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		b.WriteString(strconv.FormatUint(v.Uint(), 10))
	default:
		fmt.Fprintf(b, "%v", v.Interface())
	}
}

func writeFloat(b *strings.Builder, f float64, bits int) {
	if math.IsNaN(f) {
		b.WriteString("NaN")
		return
	}
	b.WriteString(strconv.FormatFloat(f, 'g', -1, bits))
}

// sortedKeys returns the keys of a map in the order of how they are
// written.
func sortedKeys(v reflect.Value) []reflect.Value {
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(inline(a), inline(b)) })
	return keys
}

// gameDataDump returns the game data a server starts a game with as the
// snapshots show it. The game rules of the world and the items come from
// maps: they are in no order, so they are sorted by name.
func gameDataDump(data netconn.GameData) string {
	rules := slices.Clone(data.GameRules)
	slices.SortFunc(rules, func(a, b v2193.GameRule) int { return strings.Compare(a.Name, b.Name) })
	data.GameRules = rules
	items := slices.Clone(data.Items)
	slices.SortFunc(items, func(a, b v2193.ItemEntry) int { return strings.Compare(a.Name, b.Name) })
	data.Items = items
	var b strings.Builder
	writeValue(&b, "    ", "", reflect.ValueOf(data))
	return b.String()
}
