package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	runtimedebug "runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Session handles incoming packets from connections and sends outgoing packets by providing a thin layer
// of abstraction over direct packets. A Session basically 'controls' an entity.
type Session struct {
	conf            Config
	teardownStarted atomic.Bool
	connOnce        sync.Once

	ent  *world.EntityHandle
	conn Conn
	// connWriteMu serializes connection writes to avoid concurrent WritePacket/Flush calls.
	connWriteMu   sync.Mutex
	handlers      map[uint32]packetHandler
	bossBarMu     sync.Mutex
	bossBarIDs    map[string]int64
	nextBossBarID int64
	// packetsMu protects the packets queued for the connection. While the
	// world of the session ticks, packets are held in heldPackets and handed
	// to the flushers at once afterwards; others go to pendingPackets, which
	// a flusher takes while flushing is set. flushSpare is the array of the
	// batch flushed last, used again by the flusher alone.
	packetsMu      sync.Mutex
	pendingPackets []proto.Packet
	holdPackets    int
	heldPackets    []proto.Packet
	flushing       bool
	flushSpare     []proto.Packet
	// tickEnded is when the batch of the tick of the world of the session
	// that is waiting for a flusher ended, zero for none.
	tickEnded int64
	// abilityResend is the delayed ability update queued after a game mode change.
	abilityResendMu    sync.Mutex
	abilityResend      *world.Task
	abilityResendGen   uint64
	abilityResendDelay time.Duration
	closing            atomic.Bool

	// commandOrigin holds the last command origin so output can match request metadata.
	commandOrigin atomic.Pointer[v2193.CommandOrigin]
	commandsDirty atomic.Bool
	// abilities is an immutable snapshot for clients joining the player list.
	abilities atomic.Pointer[v2193.AbilityData]

	currentScoreboard atomic.Pointer[string]
	currentLines      atomic.Pointer[[]string]

	chunkLoader *world.Loader
	// chunkRadius is updated by packet handlers while the background tick reads it.
	chunkRadius    atomic.Int32
	maxChunkRadius int32

	emoteChatMuted bool

	teleportPos atomic.Pointer[mgl64.Vec3]

	// congested is set while the connection holds more for the client than
	// it takes: see congestedAbove. stale holds the entities whose moves
	// were kept back meanwhile, under staleMu.
	congested atomic.Bool
	staleMu   sync.Mutex
	stale     map[*world.EntityHandle]struct{}

	entityMutex sync.RWMutex
	// entityRuntimeIDs holds the runtime IDs of entities shown to the session.
	entityRuntimeIDs map[*world.EntityHandle]uint64
	entities         map[uint64]*world.EntityHandle
	hiddenEntities   map[uuid.UUID]struct{}
	// hidesEntities is set while hiddenEntities holds any entity, so that
	// showing entities does not take entityMutex to find none hidden.
	hidesEntities atomic.Bool
	// deathAnimations records ordinary mobs whose death event lets the client
	// finish the animation and remove the actor itself. Protected by entityMutex.
	deathAnimations map[uint64]struct{}
	// justShown holds the entities shown whose equipment was not shown yet:
	// equipment they lack then goes unsent, as BDS sends none. Protected by
	// entityMutex.
	justShown map[*world.EntityHandle]struct{}

	// heldSlot points to the current hotbar slot index. It is stored in an atomic pointer so inventory callbacks can
	// safely access the pointer while the session is still wiring up inventory state.
	heldSlot                     atomic.Pointer[uint32]
	inv, offHand, enderChest, ui *inventory.Inventory
	armour                       *inventory.Armour

	// listEntry is the PlayerList packet adding the player of the session
	// with its current skin, encoded once for every session it goes to, as
	// the skin goes nowhere else when another player sees the player.
	listEntry atomic.Pointer[sharedPacket]

	breakingPos cube.Pos

	inTransaction, containerOpened atomic.Bool
	openedWindowID                 atomic.Uint32
	openedContainerID              atomic.Uint32
	openedWindow                   atomic.Pointer[inventory.Inventory]
	openedPos                      atomic.Pointer[cube.Pos]
	openedEntity                   atomic.Pointer[world.EntityHandle]
	swingingArm                    atomic.Bool
	changingSlot                   atomic.Bool
	changingDimension              atomic.Bool
	// moving is set while applying client-driven movement to avoid echoing it back to the same client.
	moving atomic.Bool

	lastChunkPos world.ChunkPos

	recipes       map[uint32]recipe.Recipe
	javaRecipeIDs map[string][]string

	blobMu                sync.Mutex
	blobs                 map[uint64][]byte
	openChunkTransactions []map[uint64]struct{}
	invOpened             bool

	hudMu      sync.RWMutex
	hudUpdates map[hud.Element]bool
	hiddenHud  map[hud.Element]struct{}

	debugShapesMu     sync.RWMutex
	debugShapes       map[int]debug.Shape
	debugShapeUpdates []debugShapeUpdate

	viewLayer *world.ViewLayer

	// inbox holds what the client sent that the world of the player did not
	// handle yet, in order; inboxClosed is set once the connection closed.
	// inboxSpare is the array of the inbox handled last, used again.
	inboxMu     sync.Mutex
	inbox       []proto.Packet
	inboxClosed bool
	inboxSpare  []proto.Packet
	// commandState is what the command updates Control sends start from.
	commandState commandUpdates

	lastActivity atomic.Int64

	inputLocksMu sync.RWMutex
	inputLocks   uint32

	br world.BlockRegistry
}

// debugShapeUpdate represents a pending debug shape mutation. A nil shape removes the matching ID.
type debugShapeUpdate struct {
	id    int
	shape debug.Shape
}

// Conn represents a connection that packets are read from and written to by a Session. In addition, it holds some
// information on the identity of the Session.
type Conn interface {
	io.Closer
	// IdentityData returns the login.Identity of a Conn. It contains the UUID, XUID and username of the connection.
	IdentityData() login.Identity
	// ClientData returns the login.ClientData of a Conn. This includes less sensitive data of the player like its skin,
	// language code and other non-essential information.
	ClientData() login.ClientData
	// ClientCacheEnabled specifies if the Conn has the client cache, used for caching chunks client-side, enabled or
	// not. Some platforms, like the Nintendo Switch, have this disabled at all times.
	ClientCacheEnabled() bool
	// ChunkRadius returns the chunk radius as requested by the client at the other end of the Conn.
	ChunkRadius() int
	// Latency returns the current latency measured over the Conn.
	Latency() time.Duration
	// Flush flushes the packets buffered by the Conn, sending all of them out immediately.
	Flush() error
	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr
	// ReadPacket reads a packet of the codecs of protocol 2193 from the Conn. An error is returned if a deadline
	// was set that was exceeded or if the Conn was closed while awaiting a packet.
	ReadPacket() (pk proto.Packet, err error)
	// WritePacket writes a packet of the codecs of protocol 2193 to the Conn. An error is returned if the Conn was
	// closed before sending the packet.
	WritePacket(pk proto.Packet) error
	// StartGameContext starts the game for the Conn with a context to cancel it.
	StartGameContext(ctx context.Context, data netconn.GameData) error
}

// Nop represents a no-operation session. It does not do anything when sending a packet to it.
var Nop = &Session{conf: Config{Log: slog.New(slog.DiscardHandler)}}

// selfEntityRuntimeID is the entity runtime (or unique) ID of the controllable that the session holds.
const selfEntityRuntimeID = 1

const defaultAbilityResendDelay = 50 * time.Millisecond

// errSelfRuntimeID is an error returned during packet handling for fields that refer to the player itself and
// must therefore always be 1.
var errSelfRuntimeID = errors.New("invalid entity runtime ID: runtime ID for self must always be 1")

type Config struct {
	Log *slog.Logger

	MaxChunkRadius int

	EmoteChatMuted bool
	// IsOperator reports the current operator status of a player by name.
	// If nil, players receive member permissions.
	IsOperator func(name string) bool

	JoinMessage, QuitMessage chat.Translation

	// HandleStop is called once when the Session is closed. The transaction is
	// nil if the Controllable could not be restored to any world, such as when
	// both its current world and respawn destination closed during teardown.
	HandleStop func(*world.Tx, Controllable)
	// BlockRegistry overrides the registry used for network serialization. If nil, world.DefaultBlockRegistry is used.
	BlockRegistry world.BlockRegistry
}

func (conf Config) New(conn Conn) *Session {
	r := conn.ChunkRadius()
	requestedRadius := r
	maxChunkRadius := conf.MaxChunkRadius
	if maxChunkRadius < 0 {
		maxChunkRadius = 0
	}
	if r < 0 {
		r = 0
	}
	if r > maxChunkRadius {
		r = maxChunkRadius
	}
	if r != requestedRadius {
		_ = conn.WritePacket(&v2193.ChunkRadiusUpdatedPacket{ChunkRadius: int32(r)})
	}
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	conf.Log = conf.Log.With("name", conn.IdentityData().DisplayName, "uuid", conn.IdentityData().Identity, "raddr", conn.RemoteAddr().String())

	s := &Session{}
	*s = Session{
		conf:                  conf,
		openChunkTransactions: make([]map[uint64]struct{}, 0, 8),
		handlers:              map[uint32]packetHandler{},
		entityRuntimeIDs:      map[*world.EntityHandle]uint64{},
		entities:              map[uint64]*world.EntityHandle{},
		hiddenEntities:        map[uuid.UUID]struct{}{},
		blobs:                 map[uint64][]byte{},
		maxChunkRadius:        int32(maxChunkRadius),
		emoteChatMuted:        conf.EmoteChatMuted,
		conn:                  conn,
		recipes:               make(map[uint32]recipe.Recipe),
		javaRecipeIDs:         make(map[string][]string),
		hudUpdates:            make(map[hud.Element]bool),
		hiddenHud:             make(map[hud.Element]struct{}),
		debugShapes:           make(map[int]debug.Shape),
		debugShapeUpdates:     make([]debugShapeUpdate, 0, 256),
		abilityResendDelay:    defaultAbilityResendDelay,
		lastActivity:          atomic.Int64{},
	}
	s.lastActivity.Store(time.Now().UnixNano())
	s.viewLayer = world.NewViewLayer(s)
	// Initialize heldSlot before any inventory callbacks can fire.
	s.heldSlot.Store(new(uint32))
	s.chunkRadius.Store(int32(r))
	s.openedWindow.Store(inventory.New(1, nil))
	s.openedPos.Store(&cube.Pos{})

	var scoreboardName string
	var scoreboardLines []string
	s.currentScoreboard.Store(&scoreboardName)
	s.currentLines.Store(&scoreboardLines)
	var origin v2193.CommandOrigin
	s.commandOrigin.Store(&origin)

	if conf.BlockRegistry == nil {
		s.br = world.DefaultBlockRegistry
	} else {
		s.br = conf.BlockRegistry
	}

	s.registerHandlers()
	s.sendBiomes()
	s.writePacket(creativePacket(s.br))
	s.sendRecipes()
	s.sendArmourTrimData()
	s.SendSpeed(0.1)
	return s
}

// SetHandle sets the world.EntityHandle of the Session and attaches a skin to
// other players on join.
func (s *Session) SetHandle(handle *world.EntityHandle, skin skin.Skin) {
	s.ent = handle
	s.entityRuntimeIDs[handle] = selfEntityRuntimeID
	s.entities[selfEntityRuntimeID] = handle

	s.listEntry.Store(s.newListEntry(skin))
	sessions.Add(s)
	sessionsOpen.Add(1)
}

// ViewerEntity identifies the observer for source-defined particle range checks.
// The handle must be opened in the emitting world's transaction before use.
func (s *Session) ViewerEntity() *world.EntityHandle { return s.ent }

// Spawn makes the Controllable passed spawn in the world.World.
// The function passed will be called when the session stops running.
func (s *Session) Spawn(c Controllable, tx *world.Tx) {
	s.SendHealth(c.Health(), c.MaxHealth(), c.Absorption())
	s.SendExperience(c.ExperienceLevel(), c.ExperienceProgress())
	s.SendFood(c.Food(), 0, 0)

	pos := c.Position()
	chunkRadius := s.chunkRadius.Load()
	s.chunkLoader = world.NewLoader(int(chunkRadius), tx.World(), s)
	s.chunkLoader.Move(tx, pos)
	s.writePacket(&v2193.NetworkChunkPublisherUpdatePacket{
		Position: v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		Radius:   uint32(chunkRadius) << 4,
	})

	s.sendAvailableEntities(tx.World())

	c.SetGameMode(c.GameMode())
	s.SendPermissions(c)
	for _, e := range c.Effects() {
		s.SendEffect(e)
	}
	s.ViewEntityState(c)

	s.sendInv(s.inv, v2193.WindowIDInventory)
	s.sendInv(s.ui, v2193.WindowIDUI)
	s.sendInv(s.offHand, v2193.WindowIDOffHand)
	s.sendInv(s.armour.Inventory(), v2193.WindowIDArmour)

	chat.Global.Subscribe(c)
	if !s.conf.JoinMessage.Zero() {
		chat.Global.Writet(s.conf.JoinMessage, s.conn.IdentityData().DisplayName)
	}

	// What the client sends waits in the inbox for the next tick of the
	// world of the player, which hands it to Control.
	s.ent.SetController(s)
	if r, ok := s.conn.(receiver); ok {
		r.Receive(s.receive, s.connClosed)
	} else {
		go s.readPackets()
	}
}

// receiver is a Conn that hands what the client sends to a function on the
// goroutine of its transport rather than having it read.
type receiver interface {
	// Receive has handle called with each packet the client sends and
	// closed once the connection closed.
	Receive(handle func(pk proto.Packet), closed func(err error))
}

// maxInbox bounds the packets a client may send ahead of the world of its
// player: a client sends a few a tick, so one this far ahead floods it.
const maxInbox = 2048

// receive puts a packet the client sent in the inbox.
func (s *Session) receive(pk proto.Packet) {
	s.inboxMu.Lock()
	defer s.inboxMu.Unlock()
	if s.inboxClosed {
		return
	}
	if len(s.inbox) == maxInbox {
		inboxCut.Inc()
		s.conf.Log.Debug("client sends more than the world of its player handles", "packets", len(s.inbox))
		s.inboxClosed = true
		go s.CloseConnection()
		return
	}
	s.inbox = append(s.inbox, pk)
}

// connClosed marks the inbox closed once the connection closed: the next
// tick of the world of the player handles what is left and closes it.
func (s *Session) connClosed(err error) {
	if err != nil && !errors.Is(err, net.ErrClosed) {
		s.conf.Log.Debug("connection closed: " + err.Error())
	}
	s.inboxMu.Lock()
	s.inboxClosed = true
	s.inboxMu.Unlock()
}

// readPackets reads the packets of a Conn that hands them to nothing into
// the inbox.
func (s *Session) readPackets() {
	for {
		pk, err := s.conn.ReadPacket()
		if err != nil {
			s.connClosed(err)
			return
		}
		s.receive(pk)
	}
}

// Close closes the session, which in turn closes the controllable and the connection that the session
// manages. Close ensures the method only runs code on the first call.
// Calls made after teardown has started return without waiting, so teardown
// callbacks may close the session again.
// A nil transaction may be passed for a Controllable that is no longer in any
// world; world-bound teardown (container close, chunk loader, entity removal)
// is then skipped.
func (s *Session) Close(tx *world.Tx, c Controllable) {
	if s.teardownStarted.CompareAndSwap(false, true) {
		s.close(tx, c)
	}
}

// close closes the session, which in turn closes the controllable and the connection that the session
// manages.
func (s *Session) close(tx *world.Tx, c Controllable) {
	// Install all teardown before calling user- or entity-owned code. Each step
	// still runs if an earlier one panics, after which the first panic propagates.
	defer func() {
		panicValue := recover()
		run := func(f func()) {
			defer func() {
				if r := recover(); r != nil && panicValue == nil {
					panicValue = r
				}
			}()
			f()
		}

		if tx != nil {
			run(c.MoveItemsToInventory)
			run(func() { s.closeCurrentContainer(tx, false) })
		}
		if s.viewLayer != nil {
			run(func() { _ = s.viewLayer.Close() })
		}
		if s.conf.HandleStop != nil {
			run(func() { s.conf.HandleStop(tx, c) })
		}

		// Clear the inventories so that they no longer hold references to the connection.
		run(func() { _ = s.inv.Close() })
		run(func() { _ = s.offHand.Close() })
		run(func() { _ = s.armour.Close() })

		if !s.conf.QuitMessage.Zero() {
			run(func() { chat.Global.Writet(s.conf.QuitMessage, s.conn.IdentityData().DisplayName) })
		}
		run(func() { chat.Global.Unsubscribe(c) })

		// Remove the controllable before closing its loader. Closing the last
		// loader may unload the chunk and close its remaining entities.
		if tx != nil {
			run(func() { tx.RemoveEntity(c) })
		}
		if tx != nil && s.chunkLoader != nil {
			run(func() { s.chunkLoader.Close(tx) })
		}
		if s.ent != nil {
			run(func() { _ = s.ent.Close() })
		}

		// This should always be called last due to the timing of the removal of
		// entity runtime IDs.
		if s.ent != nil {
			run(func() { sessions.Remove(s, c); sessionsOpen.Add(-1) })
			run(func() {
				s.entityMutex.Lock()
				clear(s.entityRuntimeIDs)
				clear(s.entities)
				clear(s.deathAnimations)
				s.entityMutex.Unlock()
			})
		}
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// Ensure background workers and packet writers stop even if the network read loop exited first.
	s.CloseConnection()
}

// CloseConnection closes the underlying connection of the session so that the session ends up being closed
// eventually.
func (s *Session) CloseConnection() {
	s.connOnce.Do(func() {
		s.closing.Store(true)
		s.clearPacketQueue()
		s.abilityResendMu.Lock()
		s.abilityResendGen++
		resend := s.abilityResend
		s.abilityResend = nil
		s.abilityResendMu.Unlock()
		if resend != nil {
			resend.Cancel()
		}
		_ = s.conn.Close()
	})
}

// Addr returns the net.Addr of the client.
func (s *Session) Addr() net.Addr {
	return s.conn.RemoteAddr()
}

// Latency returns the latency of the connection.
func (s *Session) Latency() time.Duration {
	return s.conn.Latency()
}

// NetStats are the state of the connection of a session, as a netstat
// shows it.
type NetStats struct {
	// Addr is the address of the client.
	Addr net.Addr
	// Latency is half the round trip time.
	Latency time.Duration
	// Queued counts the packets waiting for the connection, Inbox those of
	// the client waiting for the world of the player.
	Queued, Inbox int
	// Conn is the state of the connection, if it tells it.
	Conn    netconn.Stats
	HasConn bool
}

// NetStats returns the state of the connection of the session.
func (s *Session) NetStats() NetStats {
	st := NetStats{Addr: s.conn.RemoteAddr(), Latency: s.conn.Latency()}
	s.packetsMu.Lock()
	st.Queued = len(s.pendingPackets) + len(s.heldPackets)
	s.packetsMu.Unlock()
	s.inboxMu.Lock()
	st.Inbox = len(s.inbox)
	s.inboxMu.Unlock()
	if c, ok := s.conn.(interface{ Stats() netconn.Stats }); ok {
		st.Conn, st.HasConn = c.Stats(), true
	}
	return st
}

// withControllable runs f with the current Controllable on its world owner.
// It is for off-owner session goroutines; callbacks that already have a
// *world.Tx should use it directly instead.
func (s *Session) withControllable(ctx context.Context, f func(tx *world.Tx, c Controllable) error) error {
	_, err := world.CallRef(ctx, world.NewEntityRef[Controllable](s.ent), func(tx *world.Tx, c Controllable) (struct{}, error) {
		return struct{}{}, f(tx, c)
	})
	return err
}

// callControllable isolates panics at the asynchronous session boundary.
func (s *Session) callControllable(ctx context.Context, f func(tx *world.Tx, c Controllable) error) (err error) {
	defer func() {
		if recover() != nil {
			err = world.ErrTaskPanicked
		}
	}()
	return s.withControllable(ctx, f)
}

// sessionOwnerStopped reports whether err means the session's player can no
// longer run owner callbacks, so session goroutines should stop quietly.
func sessionOwnerStopped(err error) bool {
	return errors.Is(err, world.ErrEntityClosed) || errors.Is(err, world.ErrWorldClosed) || errors.Is(err, world.ErrTaskCancelled)
}

// ClientData returns the login.ClientData of the underlying *minecraft.Conn.
func (s *Session) ClientData() login.ClientData {
	return s.conn.ClientData()
}

// LogError writes an error through the session's configured logger.
func (s *Session) LogError(message string, args ...any) {
	if s.conf.Log != nil {
		s.conf.Log.Error(message, args...)
	}
}

// Control handles what the client sent since the last tick of the world of
// the player, in that tick, then sends chunks and updates commands. Once
// the connection closed, it closes the player.
func (s *Session) Control(tx *world.Tx, e world.Entity) {
	if s.teardownStarted.Load() {
		return
	}
	c, ok := e.(Controllable)
	if !ok {
		return
	}
	s.inboxMu.Lock()
	packets, closed := s.inbox, s.inboxClosed
	s.inbox = s.inboxSpare
	s.inboxSpare = nil
	s.inboxMu.Unlock()
	for i, pk := range packets {
		packets[i] = nil
		if err := s.handlePacketSafely(pk, tx, c); err != nil {
			s.conf.Log.Debug("process packet: " + err.Error())
			clear(packets[i+1:])
			closed = true
			break
		}
		if _, here := s.ent.Entity(tx); !here {
			// The packet took the player to another world, whose ticks
			// handle the rest.
			s.requeue(packets[i+1:])
			clear(packets[i+1:])
			s.inboxSpare = packets[:0]
			return
		}
	}
	s.inboxSpare = packets[:0]
	if closed {
		s.closeControllable(c)
		return
	}
	s.commandState.update(s, c)
	s.updateCongestion(tx)
	s.sendChunks(tx, c)
}

// requeue puts packets back at the front of the inbox.
func (s *Session) requeue(packets []proto.Packet) {
	if len(packets) == 0 {
		return
	}
	s.inboxMu.Lock()
	s.inbox = append(append([]proto.Packet(nil), packets...), s.inbox...)
	s.inboxMu.Unlock()
}

// handlePacketSafely handles a packet, turning a panic of its handler into
// an error that closes this session rather than taking down the tick of
// the world.
func (s *Session) handlePacketSafely(pk proto.Packet, tx *world.Tx, c Controllable) (err error) {
	defer func() {
		if v := recover(); v != nil {
			s.conf.Log.Error("panic handling a packet", "packet", fmt.Sprintf("%T", pk), "panic", v, "stack", string(runtimedebug.Stack()))
			err = fmt.Errorf("%T: panic: %v", pk, v)
		}
	}()
	return s.handlePacket(pk, tx, c)
}

// closeControllable closes the Controllable of a session whose connection
// closed. A player closes the session itself, possibly after it respawned
// in another world; the session is closed in any case once that is done.
func (s *Session) closeControllable(c Controllable) {
	s.CloseConnection()
	func() {
		// A panic of code closing the player, a handler of the user for
		// one, must not take down the tick: the session still closes.
		defer func() {
			if v := recover(); v != nil {
				s.conf.Log.Error("panic closing the player of a session", "panic", v, "stack", string(runtimedebug.Stack()))
			}
		}()
		_ = c.Close()
	}()
	if s.teardownStarted.Load() {
		return
	}
	go func() {
		// The Controllable might no longer be in the same world after
		// closing, so the session closes in a transaction of its own.
		if err := s.callControllable(context.Background(), func(tx *world.Tx, c Controllable) error {
			s.Close(tx, c)
			return nil
		}); err != nil && !sessionOwnerStopped(err) {
			s.conf.Log.Debug("close session: " + err.Error())
		}
	}()
}

// commandUpdates is what the command updates of a session start from: the
// commands, enums and soft enums the client was sent last.
type commandUpdates struct {
	ready      bool
	ticks      int
	runnables  map[string]map[int]cmd.Runnable
	enums      map[string]cmd.Enum
	enumValues map[string][]string
	softEnums  map[string]struct{}
}

// update sends the commands of the player first, then updates them: enums
// every second, commands once they may have changed and every five
// seconds otherwise.
func (u *commandUpdates) update(s *Session, c Controllable) {
	if !u.ready {
		u.softEnums = make(map[string]struct{})
		u.runnables = s.sendAvailableCommands(c, u.softEnums)
		u.enums, u.enumValues = s.enums(c)
		s.commandsDirty.Store(false)
		u.ready = true
		return
	}
	if u.ticks++; u.ticks%20 == 0 {
		// Enum resending happens relatively often and frequent updates are more important than with full
		// command changes. Those are generally only related to permission changes, which doesn't happen often.
		u.runnables = s.resendEnums(u.enums, u.enumValues, u.softEnums, u.runnables, c)
	}
	if dirty := s.commandsDirty.Swap(false); dirty || u.ticks%100 == 0 {
		// Permission changes refresh commands on the next tick; otherwise
		// check for command changes every five seconds.
		if r, ok := s.resendCommands(u.runnables, c, u.softEnums); ok {
			u.runnables = r
			u.enums, u.enumValues = s.enums(c)
		}
	}
}

// sendChunks sends the next up to 4 chunks to the connection. What chunks are loaded depends on the connection of
// the chunk loader and the chunks that were previously loaded.
func (s *Session) sendChunks(tx *world.Tx, c Controllable) {
	var worldSwitched bool
	if w := tx.World(); s.chunkLoader.World() != w && w != nil {
		worldSwitched = true
		s.handleWorldSwitch(w, tx, c)
	}
	pos := c.Position()
	chunkRadius := s.chunkRadius.Load()
	s.chunkLoader.Move(tx, pos)

	blockPos := cube.PosFromVec3(pos)
	chunkPos := world.ChunkPos{int32(blockPos[0] >> 4), int32(blockPos[2] >> 4)}
	if s.lastChunkPos != chunkPos || worldSwitched {
		s.lastChunkPos = chunkPos
		s.writePacket(&v2193.NetworkChunkPublisherUpdatePacket{
			Position: v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			Radius:   uint32(chunkRadius) << 4,
		})
	}

	s.blobMu.Lock()
	const maxChunkTransactions = 8
	toLoad := maxChunkTransactions - len(s.openChunkTransactions)
	s.blobMu.Unlock()
	if toLoad > 4 {
		toLoad = 4
	}
	s.chunkLoader.Load(tx, toLoad)
}

// handleWorldSwitch handles the player of the Session switching worlds.
func (s *Session) handleWorldSwitch(w *world.World, tx *world.Tx, c Controllable) {
	if s.conn.ClientCacheEnabled() {
		s.blobMu.Lock()
		s.blobs = map[uint64][]byte{}
		s.openChunkTransactions = nil
		s.blobMu.Unlock()
	}

	dim, _ := world.DimensionID(w.Dimension())
	same := w.Dimension() == s.chunkLoader.World().Dimension()
	if !same {
		s.changeDimension(int32(dim), false, c)
	}
	s.ViewEntityTeleport(c, c.Position())
	s.chunkLoader.ChangeWorld(tx, w)
}

// changeDimension changes the dimension of the client. If silent is set to true, the portal noise will be stopped
// immediately.
func (s *Session) changeDimension(dim int32, silent bool, c Controllable) {
	s.changingDimension.Store(true)
	h := s.handlers[v2193.IDServerBoundLoadingScreen].(*ServerBoundLoadingScreenHandler)
	id := h.currentID.Add(1)
	h.expectedID.Store(id)

	s.writePacket(&v2193.ChangeDimensionPacket{
		Dimension:       dim,
		Position:        vec64To32(entityNetworkPosition(c, c.Position())),
		LoadingScreenID: proto.Some(id),
	})
	s.writePacket(&v2193.StopSoundPacket{StopAll: silent})
	s.writePacket(&v2193.PlayStatusPacket{Status: v2193.PlayStatusPlayerSpawn})

	// As of v1.19.50, the dimension ack that is meant to be sent by the client is now sent by the server. The client
	// still sends the ack, but after the server has sent it. Thanks to Mojang for another groundbreaking change.
	s.writePacket(&v2193.PlayerActionPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		ActionType:      v2193.PlayerActionDimensionChangeDone,
	})
}

// ChangingDimension returns whether the session is currently changing dimension or not.
func (s *Session) ChangingDimension() bool {
	return s.changingDimension.Load()
}

// ChunkRadius returns the chunk radius of the session.
func (s *Session) ChunkRadius() int32 {
	return s.chunkRadius.Load()
}

// handlePacket handles an incoming packet, processing it accordingly. If the packet had invalid data or was
// otherwise not valid in its context, an error is returned.
func (s *Session) handlePacket(pk proto.Packet, tx *world.Tx, c Controllable) (err error) {
	s.lastActivity.Store(time.Now().UnixNano())
	handler, ok := s.handlers[pk.ID()]
	if !ok {
		s.conf.Log.Debug("unhandled packet", "packet", fmt.Sprintf("%T", pk), "data", fmt.Sprintf("%+v", pk)[1:])
		return nil
	}
	if handler == nil {
		// A nil handler means it was explicitly unhandled.
		return nil
	}
	if err := handler.Handle(pk, s, tx, c); err != nil {
		return fmt.Errorf("%T: %w", pk, err)
	}
	return nil
}

// registerHandlers registers all packet handlers found in the packetHandler package.
func (s *Session) registerHandlers() {
	s.handlers = map[uint32]packetHandler{
		v2193.IDActorEvent:                     nil,
		v2193.IDAdventureSettings:              nil, // Deprecated, the client still sends this though.
		v2193.IDAnimate:                        nil,
		v2193.IDAnvilDamage:                    nil,
		v2193.IDBlockActorData:                 &BlockActorDataHandler{},
		v2193.IDBlockPickRequest:               &BlockPickRequestHandler{},
		v2193.IDBookEdit:                       &BookEditHandler{},
		v2193.IDBossEvent:                      nil,
		v2193.IDClientCacheBlobStatus:          &ClientCacheBlobStatusHandler{},
		v2193.IDCommandRequest:                 &CommandRequestHandler{},
		v2193.IDCommandBlockUpdate:             &CommandBlockUpdateHandler{},
		v2193.IDContainerClose:                 &ContainerCloseHandler{},
		v2193.IDEmote:                          &EmoteHandler{},
		v2193.IDEmoteList:                      nil,
		v2193.IDFilterText:                     nil,
		v2193.IDInteract:                       &InteractHandler{},
		v2193.IDInventoryTransaction:           &InventoryTransactionHandler{},
		v2193.IDItemStackRequest:               &ItemStackRequestHandler{changes: map[byte]map[byte]changeInfo{}, responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{}},
		v2193.IDLecternUpdate:                  &LecternUpdateHandler{},
		v2193.IDMobEquipment:                   &MobEquipmentHandler{},
		v2193.IDModalFormResponse:              &ModalFormResponseHandler{forms: make(map[uint32]form.Form)},
		v2193.IDMovePlayer:                     nil,
		v2193.IDNPCRequest:                     &NPCRequestHandler{},
		v2193.IDPlayerAction:                   &PlayerActionHandler{},
		v2193.IDPlayerAuthInput:                &PlayerAuthInputHandler{},
		v2193.IDPlayerSkin:                     &PlayerSkinHandler{},
		v2193.IDMapInfoRequest:                 &MapInfoRequestHandler{},
		v2193.IDPlayerToggleCrafterSlotRequest: &CrafterSlotToggleHandler{},
		v2193.IDRequestAbility:                 &RequestAbilityHandler{},
		v2193.IDRequestChunkRadius:             &RequestChunkRadiusHandler{},
		v2193.IDRespawn:                        &RespawnHandler{},
		v2193.IDSetPlayerInventoryOptions:      nil,
		v2193.IDSubChunkRequest:                &SubChunkRequestHandler{},
		v2193.IDStructureBlockUpdate:           &StructureBlockUpdateHandler{},
		v2193.IDStructureTemplateDataRequest:   &StructureTemplateDataRequestHandler{},
		v2193.IDText:                           &TextHandler{},
		v2193.IDServerBoundLoadingScreen:       &ServerBoundLoadingScreenHandler{},
		v2193.IDServerBoundDiagnostics:         &ServerBoundDiagnosticsHandler{},
	}
	s.handlers[v2193.IDPositionTrackingDBClientRequest] = &PositionTrackingDBHandler{}
}

// actorIdentifier represents the structure of an actor identifier sent over the network.
type actorIdentifier struct {
	// ID is a unique namespaced identifier for the entity.
	ID string `nbt:"id"`
}

// sendAvailableEntities sends all registered entities to the player.
func (s *Session) sendAvailableEntities(w *world.World) {
	var identifiers []actorIdentifier
	for _, t := range w.EntityRegistry().Types() {
		identifiers = append(identifiers, actorIdentifier{ID: networkID(t)})
	}
	serialisedEntityData, err := nbt.Marshal(map[string]any{"idlist": identifiers})
	if err != nil {
		// Avoid crashing the session if serialization fails; log and skip the packet.
		s.conf.Log.Error("failed to marshal entity identifiers", "err", err)
		return
	}
	s.writePacket(&v2193.AvailableActorIdentifiersPacket{SerialisedEntityIdentifiers: serialisedEntityData})
	s.sendActorProperties(w)
}
