// Package clienttest provides a client.Client for the tests of the game: a
// Recorder records what the game tells the client of a player, so that a
// test can check it without a protocol.
package clienttest

import (
	"maps"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Call is a call made to a Recorder: the name of the method and its
// arguments, as passed.
type Call struct {
	Method string
	Args   []any
}

// Answers is what a Recorder answers to queries.
type Answers struct {
	Profile      client.Profile
	Addr         net.Addr
	Latency      time.Duration
	NetStats     client.NetStats
	LastActivity time.Time
	ChunkRadius  int32
	// ClientMoving is what ClientMoving reports.
	ClientMoving bool
}

// Recorder is a client.Client that records every call made to it, those of
// world.Viewer too, in order. It is safe for concurrent use and never calls
// back into the player while it holds its lock.
//
// It keeps the state its queries report: HideHudElement and ShowHudElement
// change what HudElementHidden reports, LockInput, UnlockInput and
// ClearInputLocks what InputLocked reports, AddDebugShape, RemoveDebugShape
// and RemoveAllDebugShapes what VisibleDebugShapes returns, in the order of
// their IDs. ViewLayer returns a layer of its own; ViewNameTag and the other
// view-layer methods are only recorded and do not change it.
// ViewInventories is only recorded too, and shows nothing of the changes of
// the inventories: the viewers of the player are shown them, as Player
// installs the slot functions itself.
//
// Close leaves the world alone: the player removes itself and closes its
// entity handle as it leaves. A Recorder that Join.Start returned keeps the
// Game it was started with: its Bind lists its player on the Roster of the
// Game, one of its own if the Game has none, and the first call to its Close
// unlists the player.
//
// As a client.Joining, the Recorder records Bind, Spawn, ListPlayer and
// UnlistPlayer and does nothing else for them.
type Recorder struct {
	// Answers are set before the player uses the Recorder.
	Answers Answers

	mu     sync.Mutex
	calls  []Call
	closed bool
	hidden map[hud.Element]struct{}
	locks  map[input.Lock]struct{}
	shapes map[int]debug.Shape
	layer  *world.ViewLayer
	// game is the Game of the Recorder, nil for one New made, and id the
	// identity of the client Join.Start started it for.
	game *client.Game
	id   client.Identity
}

// Compile time check to make sure Recorder implements client.Joining.
var _ client.Joining = (*Recorder)(nil)

// New returns a Recorder with no calls and zero Answers.
func New() *Recorder {
	return &Recorder{
		hidden: map[hud.Element]struct{}{},
		locks:  map[input.Lock]struct{}{},
		shapes: map[int]debug.Shape{},
		layer:  world.NewViewLayer(nil),
	}
}

// Calls returns a copy of the calls made so far, in order.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// CallsTo returns the calls made to the method of the name passed, in order.
func (r *Recorder) CallsTo(method string) []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	var calls []Call
	for _, c := range r.calls {
		if c.Method == method {
			calls = append(calls, c)
		}
	}
	return calls
}

// Reset forgets the calls made so far. It keeps the state of the queries.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// record records a call to the method of the name passed.
func (r *Recorder) record(method string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked(method, args...)
}

// recordLocked records a call while r.mu is held.
func (r *Recorder) recordLocked(method string, args ...any) {
	r.calls = append(r.calls, Call{Method: method, Args: args})
}

func (r *Recorder) Profile() client.Profile {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("Profile")
	return r.Answers.Profile
}

func (r *Recorder) Addr() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("Addr")
	return r.Answers.Addr
}

func (r *Recorder) Latency() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("Latency")
	return r.Answers.Latency
}

func (r *Recorder) NetStats() client.NetStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("NetStats")
	return r.Answers.NetStats
}

func (r *Recorder) LastActivity() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("LastActivity")
	return r.Answers.LastActivity
}

func (r *Recorder) ChunkRadius() int32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("ChunkRadius")
	return r.Answers.ChunkRadius
}

func (r *Recorder) ClientMoving() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("ClientMoving")
	return r.Answers.ClientMoving
}

func (r *Recorder) ViewRecipesUnlocked(javaIDs []string) {
	r.record("ViewRecipesUnlocked", javaIDs)
}

func (r *Recorder) ViewMapData(data world.MapData) error {
	r.record("ViewMapData", data)
	return nil
}

func (r *Recorder) ViewLayer() *world.ViewLayer {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("ViewLayer")
	return r.layer
}

// Close records the call and, on the first call, unlists its player from
// the Roster of the Game of the Recorder, if it has one, outside the lock:
// the Roster calls the Recorder. The player removes itself from the world
// and closes its handle.
func (r *Recorder) Close(tx *world.Tx) {
	r.mu.Lock()
	r.recordLocked("Close", tx)
	first := !r.closed
	r.closed = true
	game := r.game
	r.mu.Unlock()
	if first && game != nil {
		game.Roster.Unlist(r)
	}
}

// Bind records the call and, for a Recorder that Join.Start returned, lists
// the player of h on the Roster of its Game, outside the lock: the Roster
// calls ListPlayer.
func (r *Recorder) Bind(h *world.EntityHandle, sk skin.Skin) {
	r.mu.Lock()
	r.recordLocked("Bind", h, sk)
	game, id := r.game, r.id
	r.mu.Unlock()
	if game != nil {
		game.Roster.List(client.Listed{Identity: id, Handle: h, Client: r})
	}
}

func (r *Recorder) ShowHudElement(e hud.Element) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("ShowHudElement", e)
	delete(r.hidden, e)
}

func (r *Recorder) HideHudElement(e hud.Element) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("HideHudElement", e)
	r.hidden[e] = struct{}{}
}

func (r *Recorder) HudElementHidden(e hud.Element) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("HudElementHidden", e)
	_, ok := r.hidden[e]
	return ok
}

func (r *Recorder) AddDebugShape(shape debug.Shape) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("AddDebugShape", shape)
	r.shapes[shape.ShapeID()] = shape
}

func (r *Recorder) RemoveDebugShape(shape debug.Shape) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("RemoveDebugShape", shape)
	delete(r.shapes, shape.ShapeID())
}

func (r *Recorder) RemoveAllDebugShapes() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("RemoveAllDebugShapes")
	clear(r.shapes)
}

func (r *Recorder) VisibleDebugShapes() []debug.Shape {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("VisibleDebugShapes")
	shapes := make([]debug.Shape, 0, len(r.shapes))
	for _, id := range slices.Sorted(maps.Keys(r.shapes)) {
		shapes = append(shapes, r.shapes[id])
	}
	return shapes
}

func (r *Recorder) LockInput(l input.Lock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("LockInput", l)
	r.locks[l] = struct{}{}
}

func (r *Recorder) UnlockInput(l input.Lock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("UnlockInput", l)
	delete(r.locks, l)
}

func (r *Recorder) ClearInputLocks() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("ClearInputLocks")
	clear(r.locks)
}

func (r *Recorder) InputLocked(l input.Lock) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked("InputLocked", l)
	_, ok := r.locks[l]
	return ok
}

// The methods below are only recorded.

func (r *Recorder) Spawn(a0 client.Controllable, a1 *world.Tx) { r.record("Spawn", a0, a1) }
func (r *Recorder) ListPlayer(a0 client.Listed)                { r.record("ListPlayer", a0) }
func (r *Recorder) UnlistPlayer(a0 client.Listed)              { r.record("UnlistPlayer", a0) }

func (r *Recorder) ViewEntity(a0 world.Entity)         { r.record("ViewEntity", a0) }
func (r *Recorder) HideEntity(a0 world.Entity)         { r.record("HideEntity", a0) }
func (r *Recorder) ViewEntityGameMode(a0 world.Entity) { r.record("ViewEntityGameMode", a0) }
func (r *Recorder) ViewEntityMovement(a0 world.Entity, a1 mgl64.Vec3, a2 cube.Rotation, a3 bool) {
	r.record("ViewEntityMovement", a0, a1, a2, a3)
}
func (r *Recorder) ViewEntityDisplacement(a0 world.Entity, a1 mgl64.Vec3, a2 cube.Rotation, a3 bool) {
	r.record("ViewEntityDisplacement", a0, a1, a2, a3)
}
func (r *Recorder) ViewEntityVelocity(a0 world.Entity, a1 mgl64.Vec3) {
	r.record("ViewEntityVelocity", a0, a1)
}
func (r *Recorder) ViewEntityMove(a0 world.Entity, a1 *world.EntityMove) {
	r.record("ViewEntityMove", a0, a1)
}
func (r *Recorder) ViewEntityTeleport(a0 world.Entity, a1 mgl64.Vec3) {
	r.record("ViewEntityTeleport", a0, a1)
}
func (r *Recorder) ViewChunk(a0 world.ChunkPos, a1 world.Dimension, a2 *world.Column) {
	r.record("ViewChunk", a0, a1, a2)
}
func (r *Recorder) ViewTime(a0 int)                  { r.record("ViewTime", a0) }
func (r *Recorder) ViewTimeCycle(a0 bool)            { r.record("ViewTimeCycle", a0) }
func (r *Recorder) ViewEntityItems(a0 world.Entity)  { r.record("ViewEntityItems", a0) }
func (r *Recorder) ViewEntityArmour(a0 world.Entity) { r.record("ViewEntityArmour", a0) }
func (r *Recorder) ViewEntityAction(a0 world.Entity, a1 world.EntityAction) {
	r.record("ViewEntityAction", a0, a1)
}
func (r *Recorder) ViewEntityState(a0 world.Entity) { r.record("ViewEntityState", a0) }
func (r *Recorder) ViewEntityAnimation(a0 world.Entity, a1 world.EntityAnimation) {
	r.record("ViewEntityAnimation", a0, a1)
}
func (r *Recorder) ViewParticle(a0 mgl64.Vec3, a1 world.Particle) { r.record("ViewParticle", a0, a1) }
func (r *Recorder) ViewSound(a0 mgl64.Vec3, a1 world.Sound)       { r.record("ViewSound", a0, a1) }
func (r *Recorder) ViewBlockUpdate(a0 cube.Pos, a1 world.Block, a2 int) {
	r.record("ViewBlockUpdate", a0, a1, a2)
}
func (r *Recorder) ViewBlockUpdates(a0 *world.BlockUpdates) { r.record("ViewBlockUpdates", a0) }
func (r *Recorder) ViewBlockAction(a0 cube.Pos, a1 world.BlockAction) {
	r.record("ViewBlockAction", a0, a1)
}
func (r *Recorder) ViewEmote(a0 world.Entity, a1 uuid.UUID) { r.record("ViewEmote", a0, a1) }
func (r *Recorder) ViewSkin(a0 world.Entity)                { r.record("ViewSkin", a0) }
func (r *Recorder) ViewWorldSpawn(a0 cube.Pos)              { r.record("ViewWorldSpawn", a0) }
func (r *Recorder) ViewWeather(a0 bool, a1 bool)            { r.record("ViewWeather", a0, a1) }
func (r *Recorder) ViewEntityWake(a0 world.Entity)          { r.record("ViewEntityWake", a0) }
func (r *Recorder) ViewBrewingUpdate(a0 time.Duration, a1 time.Duration, a2 int32, a3 int32, a4 int32, a5 int32) {
	r.record("ViewBrewingUpdate", a0, a1, a2, a3, a4, a5)
}
func (r *Recorder) ViewFurnaceUpdate(a0 time.Duration, a1 time.Duration, a2 time.Duration, a3 time.Duration, a4 time.Duration, a5 time.Duration) {
	r.record("ViewFurnaceUpdate", a0, a1, a2, a3, a4, a5)
}
func (r *Recorder) Transfer(a0 net.IP, a1 int) { r.record("Transfer", a0, a1) }
func (r *Recorder) Disconnect(a0 text.Text)    { r.record("Disconnect", a0) }
func (r *Recorder) CloseConnection()           { r.record("CloseConnection") }
func (r *Recorder) ViewInventories(a0 *world.Tx, a1 client.Controllable, a2 menu.Inventories, a3 *uint32) client.InventoryViews {
	r.record("ViewInventories", a0, a1, a2, a3)
	return client.InventoryViews{}
}
func (r *Recorder) SendMessage(a0 text.Text)             { r.record("SendMessage", a0) }
func (r *Recorder) SendTip(a0 text.Text)                 { r.record("SendTip", a0) }
func (r *Recorder) SendPopup(a0 text.Text)               { r.record("SendPopup", a0) }
func (r *Recorder) SendJukeboxPopup(a0 text.Text)        { r.record("SendJukeboxPopup", a0) }
func (r *Recorder) SendToast(a0 text.Text, a1 text.Text) { r.record("SendToast", a0, a1) }
func (r *Recorder) SetTitleDurations(a0 time.Duration, a1 time.Duration, a2 time.Duration) {
	r.record("SetTitleDurations", a0, a1, a2)
}
func (r *Recorder) SendTitle(a0 text.Text)            { r.record("SendTitle", a0) }
func (r *Recorder) SendSubtitle(a0 text.Text)         { r.record("SendSubtitle", a0) }
func (r *Recorder) SendActionBarMessage(a0 text.Text) { r.record("SendActionBarMessage", a0) }
func (r *Recorder) SendBossBarID(a0 string, a1 text.Text, a2 bossbar.Colour, a3 float64) {
	r.record("SendBossBarID", a0, a1, a2, a3)
}
func (r *Recorder) RemoveBossBar()                            { r.record("RemoveBossBar") }
func (r *Recorder) RemoveBossBarID(a0 string)                 { r.record("RemoveBossBarID", a0) }
func (r *Recorder) SendScoreboard(a0 *scoreboard.Scoreboard)  { r.record("SendScoreboard", a0) }
func (r *Recorder) RemoveScoreboard()                         { r.record("RemoveScoreboard") }
func (r *Recorder) SendCommandOutput(a0 client.CommandOutput) { r.record("SendCommandOutput", a0) }
func (r *Recorder) SendForm(a0 form.Form)                     { r.record("SendForm", a0) }
func (r *Recorder) CloseForm()                                { r.record("CloseForm") }
func (r *Recorder) SendDialogue(a0 dialogue.Dialogue, a1 world.Entity) {
	r.record("SendDialogue", a0, a1)
}
func (r *Recorder) CloseDialogue()                       { r.record("CloseDialogue") }
func (r *Recorder) OpenMenu(a0 *menu.Menu, a1 *world.Tx) { r.record("OpenMenu", a0, a1) }
func (r *Recorder) CloseMenu(a0 *menu.Menu, a1 bool)     { r.record("CloseMenu", a0, a1) }
func (r *Recorder) ViewTrades(a0 *menu.Menu, a1 entity.Merchant, a2 []entity.MerchantOffer) {
	r.record("ViewTrades", a0, a1, a2)
}
func (r *Recorder) ViewEnchantmentOptions(a0 [3]menu.EnchantmentOption) {
	r.record("ViewEnchantmentOptions", a0)
}
func (r *Recorder) ViewSlotChange(a0 int, a1 item.Stack)          { r.record("ViewSlotChange", a0, a1) }
func (r *Recorder) ViewCrafterSlotState(a0 int, a1 bool)          { r.record("ViewCrafterSlotState", a0, a1) }
func (r *Recorder) ViewCrafterTriggered(a0 bool)                  { r.record("ViewCrafterTriggered", a0) }
func (r *Recorder) OpenSign(a0 cube.Pos, a1 bool)                 { r.record("OpenSign", a0, a1) }
func (r *Recorder) SendHudUpdates()                               { r.record("SendHudUpdates") }
func (r *Recorder) SendDebugShapes(a0 world.Dimension)            { r.record("SendDebugShapes", a0) }
func (r *Recorder) SendInputLocks()                               { r.record("SendInputLocks") }
func (r *Recorder) EnableCoordinates(a0 bool)                     { r.record("EnableCoordinates", a0) }
func (r *Recorder) EnableInstantRespawn(a0 bool)                  { r.record("EnableInstantRespawn", a0) }
func (r *Recorder) SendHealth(a0 float64, a1 float64, a2 float64) { r.record("SendHealth", a0, a1, a2) }
func (r *Recorder) SendFood(a0 int, a1 float64, a2 float64)       { r.record("SendFood", a0, a1, a2) }
func (r *Recorder) SendExperience(a0 int, a1 float64)             { r.record("SendExperience", a0, a1) }
func (r *Recorder) SendEffect(a0 effect.Effect)                   { r.record("SendEffect", a0) }
func (r *Recorder) SendEffectModification(a0 effect.Effect)       { r.record("SendEffectModification", a0) }
func (r *Recorder) SendEffectRemoval(a0 effect.Type)              { r.record("SendEffectRemoval", a0) }
func (r *Recorder) SendSpeed(a0 float64)                          { r.record("SendSpeed", a0) }
func (r *Recorder) SendAbilities(a0 client.Controllable)          { r.record("SendAbilities", a0) }
func (r *Recorder) SendGameMode(a0 client.Controllable)           { r.record("SendGameMode", a0) }
func (r *Recorder) SendPermissions(a0 client.Controllable)        { r.record("SendPermissions", a0) }
func (r *Recorder) SendHeldSlot(a0 int, a1 client.Controllable, a2 bool) {
	r.record("SendHeldSlot", a0, a1, a2)
}
func (r *Recorder) SendRespawn(a0 mgl64.Vec3, a1 client.Controllable) {
	r.record("SendRespawn", a0, a1)
}
func (r *Recorder) SendPlayerSpawn(a0 mgl64.Vec3) { r.record("SendPlayerSpawn", a0) }
func (r *Recorder) SetPlayerSpawn(a0 cube.Pos, a1 world.Dimension) {
	r.record("SetPlayerSpawn", a0, a1)
}
func (r *Recorder) SendChargeItemComplete()              { r.record("SendChargeItemComplete") }
func (r *Recorder) ResyncInventory()                     { r.record("ResyncInventory") }
func (r *Recorder) ShowSkin(a0 skin.Skin)                { r.record("ShowSkin", a0) }
func (r *Recorder) ViewSleepingPlayers(a0 int, a1 int)   { r.record("ViewSleepingPlayers", a0, a1) }
func (r *Recorder) ViewElderGuardianCurse(a0 mgl64.Vec3) { r.record("ViewElderGuardianCurse", a0) }
func (r *Recorder) ViewSpectatedEntity(a0 world.Entity)  { r.record("ViewSpectatedEntity", a0) }
func (r *Recorder) ViewItemCooldown(a0 world.Item, a1 time.Duration) {
	r.record("ViewItemCooldown", a0, a1)
}
func (r *Recorder) PlaySound(a0 world.Sound, a1 mgl64.Vec3) { r.record("PlaySound", a0, a1) }
func (r *Recorder) StopSound(a0 string, a1 bool)            { r.record("StopSound", a0, a1) }
func (r *Recorder) ViewNamedParticle(a0 mgl64.Vec3, a1 string, a2 world.Dimension) {
	r.record("ViewNamedParticle", a0, a1, a2)
}
func (r *Recorder) StartShowingEntity(a0 world.Entity)        { r.record("StartShowingEntity", a0) }
func (r *Recorder) StopShowingEntity(a0 world.Entity)         { r.record("StopShowingEntity", a0) }
func (r *Recorder) ViewNameTag(a0 world.Entity, a1 text.Text) { r.record("ViewNameTag", a0, a1) }
func (r *Recorder) ViewPublicNameTag(a0 world.Entity)         { r.record("ViewPublicNameTag", a0) }
func (r *Recorder) ViewAlwaysShowNameTag(a0 world.Entity, a1 bool) {
	r.record("ViewAlwaysShowNameTag", a0, a1)
}
func (r *Recorder) ViewPublicAlwaysShowNameTag(a0 world.Entity) {
	r.record("ViewPublicAlwaysShowNameTag", a0)
}
func (r *Recorder) ViewScoreTag(a0 world.Entity, a1 text.Text) { r.record("ViewScoreTag", a0, a1) }
func (r *Recorder) ViewPublicScoreTag(a0 world.Entity)         { r.record("ViewPublicScoreTag", a0) }
func (r *Recorder) ViewVisibility(a0 world.Entity, a1 world.VisibilityLevel) {
	r.record("ViewVisibility", a0, a1)
}
func (r *Recorder) RemoveViewLayer(a0 world.Entity) { r.record("RemoveViewLayer", a0) }
