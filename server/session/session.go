package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Session handles incoming packets from connections and sends outgoing packets by providing a thin layer
// of abstraction over direct packets. A Session basically 'controls' an entity.
type Session struct {
	conf           Config
	once, connOnce sync.Once

	ent  *world.EntityHandle
	conn Conn
	// connWriteMu serializes connection writes to avoid concurrent WritePacket/Flush calls.
	connWriteMu sync.Mutex
	handlers    map[uint32]packetHandler
	packets     chan packet.Packet
	// abilityResend is the delayed ability update queued after a game mode change.
	abilityResendMu    sync.RWMutex
	abilityResend      *world.Task
	abilityResendDelay time.Duration
	closing            atomic.Bool

	// commandOrigin holds the last command origin so output can match request metadata.
	commandOrigin atomic.Pointer[protocol.CommandOrigin]

	currentScoreboard atomic.Pointer[string]
	currentLines      atomic.Pointer[[]string]

	chunkLoader *world.Loader
	// chunkRadius is updated by packet handlers while the background tick reads it.
	chunkRadius    atomic.Int32
	maxChunkRadius int32

	emoteChatMuted bool

	teleportPos atomic.Pointer[mgl64.Vec3]

	entityMutex sync.RWMutex
	// currentEntityRuntimeID holds the runtime ID assigned to the last entity. It is incremented for every
	// entity spawned to the session.
	currentEntityRuntimeID uint64
	// entityRuntimeIDs holds the runtime IDs of entities shown to the session.
	entityRuntimeIDs map[*world.EntityHandle]uint64
	entities         map[uint64]*world.EntityHandle
	hiddenEntities   map[uuid.UUID]struct{}

	// heldSlot points to the current hotbar slot index. It is stored in an atomic pointer so inventory callbacks can
	// safely access the pointer while the session is still wiring up inventory state.
	heldSlot                     atomic.Pointer[uint32]
	inv, offHand, enderChest, ui *inventory.Inventory
	armour                       *inventory.Armour

	// joinSkin is the first skin that the player joined with. It is sent on
	// spawn for the player list, but otherwise updated immediately when the
	// player is viewed.
	joinSkin skin.Skin

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

	recipes map[uint32]recipe.Recipe

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

	blockUpdatesMu     sync.Mutex
	blockUpdates       map[blockUpdateKey]blockUpdateData
	blockUpdateDrops   atomic.Uint32
	blockUpdateLastLog atomic.Int64
	closeBackground    chan struct{}

	overflowStreak  atomic.Uint32
	overflowLastLog atomic.Int64

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
	// IdentityData returns the login.IdentityData of a Conn. It contains the UUID, XUID and username of the connection.
	IdentityData() login.IdentityData
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
	// ReadPacket reads a packet.Packet from the Conn. An error is returned if a deadline was set that was
	// exceeded or if the Conn was closed while awaiting a packet.
	ReadPacket() (pk packet.Packet, err error)
	// WritePacket writes a packet.Packet to the Conn. An error is returned if the Conn was closed before sending the
	// packet.
	WritePacket(pk packet.Packet) error
	// StartGameContext starts the game for the Conn with a context to cancel it.
	StartGameContext(ctx context.Context, data minecraft.GameData) error
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
		_ = conn.WritePacket(&packet.ChunkRadiusUpdated{ChunkRadius: int32(r)})
	}
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	conf.Log = conf.Log.With("name", conn.IdentityData().DisplayName, "uuid", conn.IdentityData().Identity, "raddr", conn.RemoteAddr().String())

	s := &Session{}
	*s = Session{
		openChunkTransactions:  make([]map[uint64]struct{}, 0, 8),
		closeBackground:        make(chan struct{}),
		handlers:               map[uint32]packetHandler{},
		packets:                make(chan packet.Packet, 256),
		entityRuntimeIDs:       map[*world.EntityHandle]uint64{},
		entities:               map[uint64]*world.EntityHandle{},
		hiddenEntities:         map[uuid.UUID]struct{}{},
		blobs:                  map[uint64][]byte{},
		maxChunkRadius:         int32(maxChunkRadius),
		emoteChatMuted:         conf.EmoteChatMuted,
		conn:                   conn,
		currentEntityRuntimeID: 1,
		recipes:                make(map[uint32]recipe.Recipe),
		conf:                   conf,
		hudUpdates:             make(map[hud.Element]bool),
		hiddenHud:              make(map[hud.Element]struct{}),
		debugShapes:            make(map[int]debug.Shape),
		debugShapeUpdates:      make([]debugShapeUpdate, 0, 256),
		abilityResendDelay:     defaultAbilityResendDelay,
	}
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
	var origin protocol.CommandOrigin
	s.commandOrigin.Store(&origin)

	if conf.BlockRegistry == nil {
		s.br = world.DefaultBlockRegistry
	} else {
		s.br = conf.BlockRegistry
	}

	s.registerHandlers()
	s.sendBiomes()
	groups, items := creativeContent(s.br)
	s.writePacket(&packet.CreativeContent{Groups: groups, Items: items})
	s.sendRecipes()
	s.sendArmourTrimData()
	s.SendSpeed(0.1)
	go func() {
		for {
			select {
			case <-s.closeBackground:
				return
			case pk := <-s.packets:
				s.connWriteMu.Lock()
				_ = conn.WritePacket(pk)
				s.connWriteMu.Unlock()
			}
		}
	}()
	return s
}

// SetHandle sets the world.EntityHandle of the Session and attaches a skin to
// other players on join.
func (s *Session) SetHandle(handle *world.EntityHandle, skin skin.Skin) {
	s.ent = handle
	s.entityRuntimeIDs[handle] = selfEntityRuntimeID
	s.entities[selfEntityRuntimeID] = handle

	s.joinSkin = skin
	sessions.Add(s)
}

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
	s.writePacket(&packet.NetworkChunkPublisherUpdate{
		Position: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		Radius:   uint32(chunkRadius) << 4,
	})

	s.sendAvailableEntities(tx.World())

	c.SetGameMode(c.GameMode())
	for _, e := range c.Effects() {
		s.SendEffect(e)
	}
	s.ViewEntityState(c)

	s.sendInv(s.inv, protocol.WindowIDInventory)
	s.sendInv(s.ui, protocol.WindowIDUI)
	s.sendInv(s.offHand, protocol.WindowIDOffHand)
	s.sendInv(s.armour.Inventory(), protocol.WindowIDArmour)

	chat.Global.Subscribe(c)
	if !s.conf.JoinMessage.Zero() {
		chat.Global.Writet(s.conf.JoinMessage, s.conn.IdentityData().DisplayName)
	}

	go s.background()
	go s.handlePackets()
}

// Close closes the session, which in turn closes the controllable and the connection that the session
// manages. Close ensures the method only runs code on the first call.
// A nil transaction may be passed for a Controllable that is no longer in any
// world; world-bound teardown (container close, chunk loader, entity removal)
// is then skipped.
func (s *Session) Close(tx *world.Tx, c Controllable) {
	s.once.Do(func() {
		s.close(tx, c)
	})
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
			run(func() { sessions.Remove(s, c) })
			run(func() {
				s.entityMutex.Lock()
				clear(s.entityRuntimeIDs)
				clear(s.entities)
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
		s.abilityResendMu.Lock()
		if s.abilityResend != nil {
			s.abilityResend.Cancel()
		}
		s.abilityResendMu.Unlock()
		_ = s.conn.Close()
		close(s.closeBackground)
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

// handlePackets continuously handles incoming packets from the connection. It processes them accordingly.
// Once the connection is closed, handlePackets will return.
func (s *Session) handlePackets() {
	defer func() {
		// First close the Controllable. This might lead to a world change
		// (player might be dead while disconnecting, in which case it will
		// respawn first).
		if err := s.callControllable(context.Background(), func(_ *world.Tx, c Controllable) error {
			_ = c.Close()
			return nil
		}); err != nil && !sessionOwnerStopped(err) {
			s.conf.Log.Debug("close controllable: " + err.Error())
		}
		// Because the player might no longer be in the same world after
		// closing, we create a new transaction
		if err := s.callControllable(context.Background(), func(tx *world.Tx, c Controllable) error {
			s.Close(tx, c)
			return nil
		}); err != nil && !sessionOwnerStopped(err) {
			s.conf.Log.Debug("close session: " + err.Error())
		}
	}()
	for {
		pk, err := s.conn.ReadPacket()
		if err != nil {
			return
		}
		err = s.callControllable(context.Background(), func(tx *world.Tx, c Controllable) error {
			return s.handlePacket(pk, tx, c)
		})
		if err != nil {
			if sessionOwnerStopped(err) {
				return
			}
			s.conf.Log.Debug("process packet: " + err.Error())
			return
		}
	}
}

// background performs background tasks of the Session. This includes chunk sending and automatic command updating.
// background returns when the Session's connection is closed using CloseConnection.
func (s *Session) background() {
	var (
		r          map[string]map[int]cmd.Runnable
		enums      map[string]cmd.Enum
		enumValues map[string][]string
		softEnums  = make(map[string]struct{})
		ok         bool
		i          int
	)

	if err := s.withControllable(context.Background(), func(_ *world.Tx, c Controllable) error {
		r = s.sendAvailableCommands(c, softEnums)
		enums, enumValues = s.enums(c)
		return nil
	}); err != nil {
		if !sessionOwnerStopped(err) {
			s.conf.Log.Debug("prepare command updates: " + err.Error())
		}
		return
	}
	t := time.NewTicker(time.Second / 20)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flushBlockUpdates()
			if err := s.withControllable(context.Background(), func(tx *world.Tx, c Controllable) error {
				if i++; i%20 == 0 {
					// Enum resending happens relatively often and frequent updates are more important than with full
					// command changes. Those are generally only related to permission changes, which doesn't happen often.
					r = s.resendEnums(enums, enumValues, softEnums, r, c)
				}
				if i%100 == 0 {
					// Try to resend commands only every 5 seconds.
					if r, ok = s.resendCommands(r, c, softEnums); ok {
						enums, enumValues = s.enums(c)
					}
				}
				s.sendChunks(tx, c)
				return nil
			}); err != nil {
				if !sessionOwnerStopped(err) {
					s.conf.Log.Debug("update session background: " + err.Error())
				}
				return
			}
		case <-s.closeBackground:
			return
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
		s.writePacket(&packet.NetworkChunkPublisherUpdate{
			Position: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
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
	h := s.handlers[packet.IDServerBoundLoadingScreen].(*ServerBoundLoadingScreenHandler)
	id := h.currentID.Add(1)
	h.expectedID.Store(id)

	s.writePacket(&packet.ChangeDimension{
		Dimension:       dim,
		Position:        vec64To32(entityNetworkPosition(c, c.Position())),
		LoadingScreenID: protocol.Option(id),
	})
	s.writePacket(&packet.StopSound{StopAll: silent})
	s.writePacket(&packet.PlayStatus{Status: packet.PlayStatusPlayerSpawn})

	// As of v1.19.50, the dimension ack that is meant to be sent by the client is now sent by the server. The client
	// still sends the ack, but after the server has sent it. Thanks to Mojang for another groundbreaking change.
	s.writePacket(&packet.PlayerAction{
		EntityRuntimeID: selfEntityRuntimeID,
		ActionType:      protocol.PlayerActionDimensionChangeDone,
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
func (s *Session) handlePacket(pk packet.Packet, tx *world.Tx, c Controllable) (err error) {
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
		packet.IDActorEvent:                nil,
		packet.IDAdventureSettings:         nil, // Deprecated, the client still sends this though.
		packet.IDAnimate:                   nil,
		packet.IDAnvilDamage:               nil,
		packet.IDBlockActorData:            &BlockActorDataHandler{},
		packet.IDBlockPickRequest:          &BlockPickRequestHandler{},
		packet.IDBookEdit:                  &BookEditHandler{},
		packet.IDBossEvent:                 nil,
		packet.IDClientCacheBlobStatus:     &ClientCacheBlobStatusHandler{},
		packet.IDCommandRequest:            &CommandRequestHandler{},
		packet.IDContainerClose:            &ContainerCloseHandler{},
		packet.IDEmote:                     &EmoteHandler{},
		packet.IDEmoteList:                 nil,
		packet.IDFilterText:                nil,
		packet.IDInteract:                  &InteractHandler{},
		packet.IDInventoryTransaction:      &InventoryTransactionHandler{},
		packet.IDItemStackRequest:          &ItemStackRequestHandler{changes: map[byte]map[byte]changeInfo{}, responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{}},
		packet.IDLecternUpdate:             &LecternUpdateHandler{},
		packet.IDMobEquipment:              &MobEquipmentHandler{},
		packet.IDModalFormResponse:         &ModalFormResponseHandler{forms: make(map[uint32]form.Form)},
		packet.IDMovePlayer:                nil,
		packet.IDNPCRequest:                &NPCRequestHandler{},
		packet.IDPlayerAction:              &PlayerActionHandler{},
		packet.IDPlayerAuthInput:           &PlayerAuthInputHandler{},
		packet.IDPlayerSkin:                &PlayerSkinHandler{},
		packet.IDRequestAbility:            &RequestAbilityHandler{},
		packet.IDRequestChunkRadius:        &RequestChunkRadiusHandler{},
		packet.IDRespawn:                   &RespawnHandler{},
		packet.IDSetPlayerInventoryOptions: nil,
		packet.IDSubChunkRequest:           &SubChunkRequestHandler{},
		packet.IDText:                      &TextHandler{},
		packet.IDServerBoundLoadingScreen:  &ServerBoundLoadingScreenHandler{},
		packet.IDServerBoundDiagnostics:    &ServerBoundDiagnosticsHandler{},
	}
}

// writePacket writes a packet to the session's connection if it is not Nop.
func (s *Session) writePacket(pk packet.Packet) {
	if s == Nop {
		return
	}
	select {
	case s.packets <- pk:
		s.overflowStreak.Store(0)
		return
	case <-s.closeBackground:
		return
	default:
		const overflowDisconnectThreshold = 64
		streak := s.overflowStreak.Add(1)
		now := time.Now().UnixNano()
		last := s.overflowLastLog.Load()
		if now-last > int64(time.Second) && s.overflowLastLog.CompareAndSwap(last, now) {
			s.conf.Log.Warn("session packet queue overflow, dropping packets", "packet", fmt.Sprintf("%T", pk), "streak", streak)
		}
		if streak >= overflowDisconnectThreshold {
			s.conf.Log.Warn("closing session due to packet queue overflow", "streak", streak)
			s.CloseConnection()
		}
	}
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
		identifiers = append(identifiers, actorIdentifier{ID: t.EncodeEntity()})
	}
	serialisedEntityData, err := nbt.Marshal(map[string]any{"idlist": identifiers})
	if err != nil {
		// Avoid crashing the session if serialization fails; log and skip the packet.
		s.conf.Log.Error("failed to marshal entity identifiers", "err", err)
		return
	}
	s.writePacket(&packet.AvailableActorIdentifiers{SerialisedEntityIdentifiers: serialisedEntityData})
}
