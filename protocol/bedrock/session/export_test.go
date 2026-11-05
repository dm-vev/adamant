package session

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// HandlePacket handles a packet as the session does once it read one, for
// the tests of other packages, without the recovery of the goroutine that
// reads: a panic of a handler reaches the test.
func (s *Session) HandlePacket(pk proto.Packet, tx *world.Tx, c Controllable) error {
	return s.handlePacket(pk, tx, c)
}

// Inventories returns the inventories of the player of the session, as the
// session holds them.
func (s *Session) Inventories() (inv, offHand, enderChest, ui *inventory.Inventory, armour *inventory.Armour) {
	return s.inv, s.offHand, s.enderChest, s.ui, s.armour
}

// WindowGraceTicks is how many ticks after a window was shown to the client
// the session takes what the client does for what it did before it saw it.
const WindowGraceTicks = 20

// Idle reports whether the session has no packet left to write: those it
// queued went to its connection.
func (s *Session) Idle() bool {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	return len(s.pendingPackets)+len(s.heldPackets) == 0 && !s.flushing
}

// Window returns the menu the session shows open, and the ID and type of
// its window.
func (s *Session) Window() (m *menu.Menu, id, containerType uint32) {
	return s.menu.Load(), s.openedWindowID.Load(), s.openedContainerID.Load()
}

// EncodeSubChunk and EncodeBiomes encode the parts of a chunk as the session
// sends them to the clients of the default protocol, for the tests of the
// encoding.
func EncodeSubChunk(c *chunk.Chunk, ind int) []byte {
	return encodeSubChunk(c, ind, blockTable(oldProtocol(), c.BlockRegistry()))
}
func EncodeBiomes(c *chunk.Chunk) []byte { return encodeBiomes(c) }

// StatesOfDefault returns a runtime ID of each state of a finalized
// registry the clients of the default protocol know, the one they read its
// hash back as, sorted by its hash: the same states whatever the protocol
// whose blocks the game holds, as the scenes of the tests of the encoding
// take them.
func StatesOfDefault(br world.BlockRegistry) []uint32 {
	h := blockTable(oldProtocol(), br)
	hashes := slices.Sorted(maps.Keys(h.rid))
	rids := make([]uint32, len(hashes))
	for i, hash := range hashes {
		rids[i] = h.rid[hash]
	}
	return rids
}

// NetworkItemNBT returns the item NBT ItemStack sends for a stack, for the
// tests that build the stacks clients send.
func NetworkItemNBT(s item.Stack) map[string]any { return networkItemNBT(s) }

// SoundPackets returns the packets a session on the default block registry
// hands out to play a sound at a position, for the tests of the mapping of
// sounds.
func SoundPackets(pos mgl64.Vec3, t world.Sound) []proto.Packet {
	s := &Session{br: world.DefaultBlockRegistry}
	var pks []proto.Packet
	s.soundPackets(pos, t, false, func(pk proto.Packet) { pks = append(pks, pk) })
	return pks
}

// ParticlePackets returns the packets a session on the default block registry
// hands out to show a particle at a position, for the tests of the mapping of
// particles: those it writes itself, which come after those it hands out.
func ParticlePackets(pos mgl64.Vec3, p world.Particle) []proto.Packet {
	s := &Session{br: world.DefaultBlockRegistry, holdPackets: 1}
	var pks []proto.Packet
	s.particlePackets(pos, p, func(pk proto.Packet) { pks = append(pks, pk) })
	return append(pks, s.heldPackets...)
}

// ActionScene is the session the tests of the mapping of entity actions show
// an action to: the entity of its own player, and the entities it shows, by
// the runtime IDs of their handles.
type ActionScene struct {
	Self  world.Entity
	Shown []world.Entity
	// Congested and SwingingArm set the state of the session the actions
	// that depend on it are shown in.
	Congested, SwingingArm bool
}

// ActionResult is what a session did to show an action: the packets it
// queued, and the runtime IDs of the dying mobs it remembered.
type ActionResult struct {
	Packets         []proto.Packet
	DeathAnimations []uint64
}

// show returns what a session of the scene does as view shows an action.
func (sc ActionScene) show(view func(s *Session)) ActionResult {
	s := &Session{
		br:               world.DefaultBlockRegistry,
		conf:             Config{Log: slog.New(slog.DiscardHandler)},
		holdPackets:      1,
		ent:              sc.Self.H(),
		entityRuntimeIDs: map[*world.EntityHandle]uint64{sc.Self.H(): selfEntityRuntimeID},
		entities:         map[uint64]*world.EntityHandle{selfEntityRuntimeID: sc.Self.H()},
	}
	for _, e := range sc.Shown {
		s.entityRuntimeIDs[e.H()] = e.H().RuntimeID()
		s.entities[e.H().RuntimeID()] = e.H()
	}
	s.congested.Store(sc.Congested)
	s.swingingArm.Store(sc.SwingingArm)
	view(s)
	return ActionResult{Packets: slices.Clone(s.heldPackets), DeathAnimations: slices.Sorted(maps.Keys(s.deathAnimations))}
}

// EntityActionPackets returns what a session of the scene does as it views an
// action performed by an entity, for the tests of the mapping of entity
// actions.
func EntityActionPackets(sc ActionScene, e world.Entity, a world.EntityAction) ActionResult {
	return sc.show(func(s *Session) { s.ViewEntityAction(e, a) })
}

// RepairMultiRecipe is the ID of the special recipe that repairs two tools of
// the grid in one, which the tests of the item stack requests craft.
const RepairMultiRecipe = repairMultiRecipe

// StackScene is a session of no connection and no player, whose item stack
// requests run as batches over its inventories, for the tests of the handling
// of item stack requests.
type StackScene struct {
	s  *Session
	h  *ItemStackRequestHandler
	tx *world.Tx
	c  Controllable
}

// modeController is a player of a game mode, as far as the requests ask.
type modeController struct {
	batchController
	mode world.GameMode
}

func (c modeController) GameMode() world.GameMode { return c.mode }

// NewStackScene returns a scene over a transaction, whose player is in the game
// mode passed.
func NewStackScene(tx *world.Tx, mode world.GameMode) *StackScene {
	s := craftingTestSession()
	return &StackScene{s: s, h: craftingTestHandler(), tx: tx, c: modeController{batchController: batchController{tx: tx, s: s}, mode: mode}}
}

// Main, OffHand and UI return the inventories of the scene.
func (sc *StackScene) Main() *inventory.Inventory    { return sc.s.inv }
func (sc *StackScene) OffHand() *inventory.Inventory { return sc.s.offHand }
func (sc *StackScene) UI() *inventory.Inventory      { return sc.s.ui }

// AddRecipe makes the recipe passed known to the session by the network ID
// passed.
func (sc *StackScene) AddRecipe(id uint32, r recipe.Recipe) { sc.s.recipes[id] = r }

// StackID returns the network ID of the stack passed, which the client names
// the stack by.
func (sc *StackScene) StackID(s item.Stack) int32 { return item_id(s) }

// StackResult is what a session did to handle a request: the error it
// returned, as text, and the packets it queued.
type StackResult struct {
	Err     string
	Packets []proto.Packet
}

// Request handles the request passed as the session does.
func (sc *StackScene) Request(req mc.ItemStackRequest) (res StackResult) {
	if err := sc.h.handleRequest(req, sc.s, sc.tx, sc.c); err != nil {
		res.Err = err.Error()
	}
	res.Packets = slices.Clone(sc.s.heldPackets)
	sc.s.heldPackets = nil
	return res
}

// OpenMenu opens the menu of the block passed, placed in the world of the
// transaction, as the one the player of the scene has open.
func (sc *StackScene) OpenMenu(b world.Block) {
	pos := cube.Pos{0, 64, 0}
	sc.tx.SetBlock(pos, b, nil)
	sc.s.menu.Store(menu.OpenBlock(sc.tx, pos, sc.s, sc.s.enderChest))
}

// OpenInventory makes the inventory screen the one the player of the scene has
// open, and OpenAs sets the type of the container the player has open.
func (sc *StackScene) OpenInventory() { sc.s.windows.inventory = true }
func (sc *StackScene) OpenAs(containerType uint32) {
	sc.s.openedContainerID.Store(containerType)
}

// StackSlot is a stack in an inventory of the scene.
type StackSlot struct {
	// Where is the inventory and the slot, Stack what the slot holds as
	// text and ID its network ID.
	Where, Stack string
	ID           int32
}

// State returns the stacks the inventories of the scene hold.
func (sc *StackScene) State() []StackSlot {
	var slots []StackSlot
	for _, inv := range []struct {
		name string
		inv  *inventory.Inventory
	}{{"main", sc.s.inv}, {"off hand", sc.s.offHand}, {"ui", sc.s.ui}} {
		for slot, st := range inv.inv.Slots() {
			if st.Empty() {
				continue
			}
			name, meta := st.Item().EncodeItem()
			slots = append(slots, StackSlot{fmt.Sprintf("%s %d", inv.name, slot), fmt.Sprintf("%d x %s:%d", st.Count(), name, meta), item_id(st)})
		}
	}
	return slots
}

// oldProtocol returns protocol 2193, the oldest the adapter speaks and the
// primary the goldens of testdata/golden were recorded with.
func oldProtocol() *versions.Protocol {
	p, _ := versions.ByNumber(2193)
	return p
}
