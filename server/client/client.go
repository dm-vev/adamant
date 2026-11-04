package client

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
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
)

// Client is the client of a player: what the game tells it, in groups. A
// Player holds the Client of the remote client that plays it, and uses Nop
// if there is none.
//
// The adapter binds the Client to the entity of its player as the player
// joins: the Client is then the Joining that Join.Start returned. A
// *player.Player is a view of the player valid within one transaction, so a
// Client keeps none: it is passed the player as a Controllable where it needs
// its state.
//
// Methods are called within a transaction of the world of the player, on the
// goroutine of that world, unless their doc says otherwise. A Client must be
// comparable, as the player looks for it among the viewers of its world: a
// pointer type is.
type Client interface {
	// A Client views the world around its player as any viewer does. The
	// player counts its own Client among the viewers it shows itself to.
	world.Viewer
	MapViewer
	Connection
	Messenger
	Screens
	HUD
	Self
	Views
}

// A Client is what the menus of its player show it through.
var _ menu.Viewer = Client(nil)

// MapViewer is a viewer shown the content of maps: the client of a player
// that carries a map, and the viewers of a map in an item frame.
type MapViewer interface {
	// ViewMapData shows the content of a map: its colours, those changed, and
	// the objects and decorations it tracks. It returns an error for a map
	// the client cannot be shown, such as one of a dimension its protocol
	// cannot name.
	ViewMapData(data world.MapData) error
}

// Connection is the connection of a client and the lifecycle of its player.
type Connection interface {
	// Profile returns what the client told of the device it runs on when it
	// joined.
	Profile() Profile
	// Addr returns the address of the client, nil if it has none. It may be
	// called from any goroutine.
	Addr() net.Addr
	// Latency returns half the round trip time to the client, 0 if it is not
	// known. It may be called from any goroutine.
	Latency() time.Duration
	// NetStats returns the state of the connection, as a netstat shows it.
	// It may be called from any goroutine.
	NetStats() NetStats
	// LastActivity returns when the client was last heard from. It may be
	// called from any goroutine.
	LastActivity() time.Time
	// ChunkRadius returns the radius, in chunks, of the world around the
	// player that the client is shown.
	ChunkRadius() int32
	// ClientMoving reports whether the player is being moved as its client
	// moved it: a move the client made, which it is not shown again.
	ClientMoving() bool
	// ViewInventories hands the client the inventories of its player c, as c
	// opens in tx, and returns what the client shows of their changes.
	// heldSlot points at the index of the slot held. Player installs the
	// slot functions of the inventories, which show the viewers of the player
	// the items it holds and the armour it wears, and call the functions
	// returned.
	ViewInventories(tx *world.Tx, c Controllable, inv menu.Inventories, heldSlot *uint32) InventoryViews
	// Transfer has the client leave for the server at ip and port.
	Transfer(ip net.IP, port int)
	// Disconnect shows the client message as the reason it is disconnected
	// and disconnects it; a message that renders empty disconnects it with no
	// screen. It does not end the session of the client with its player:
	// Close does.
	Disconnect(message text.Text)
	// Close ends the session of the client after its player left: in this
	// order, it closes the view layer, closes the chunk loader if tx is not
	// nil, and unlists the player from the Roster of its Game. tx is the
	// transaction of the world the player was in, nil if it was in none.
	// The player calls it once, after it removed itself from the world of
	// tx and before it closes its entity handle. Only the first call does
	// anything.
	Close(tx *world.Tx)
	// CloseConnection closes the connection of the client, which ends its
	// session eventually. It may be called from any goroutine, any number of
	// times.
	CloseConnection()
}

// InventoryViews are what a client shows of the changes of the inventories
// of its player, which the slot functions of the inventories call. A nil
// function shows nothing.
type InventoryViews struct {
	Main, OffHand, EnderChest, UI, Armour inventory.SlotFunc
}

// Messenger shows text to the client: in the chat, on the screen, in boss
// bars and in the sidebar. The adapter renders the text as its protocol
// shows text.
type Messenger interface {
	// SendMessage shows t in the chat. Where its protocol can, the client
	// translates the translations t holds into its language.
	SendMessage(t text.Text)
	// SendTip shows t in the middle of the screen.
	SendTip(t text.Text)
	// SendPopup shows t above the hotbar, in place of the name of the item
	// held.
	SendPopup(t text.Text)
	// SendJukeboxPopup shows t above the hotbar, as a jukebox shows the
	// record it plays.
	SendJukeboxPopup(t text.Text)
	// SendToast shows a toast with title and message at the top of the
	// screen.
	SendToast(title, message text.Text)
	// SetTitleDurations sets how long the titles shown next fade in, remain
	// and fade out.
	SetTitleDurations(fadeIn, remain, fadeOut time.Duration)
	// SendTitle shows t as a title.
	SendTitle(t text.Text)
	// SendSubtitle shows t under the title.
	SendSubtitle(t text.Text)
	// SendActionBarMessage shows t above the hotbar, in step with the title.
	SendActionBarMessage(t text.Text)
	// SendBossBarID shows the boss bar of the ID passed, or updates it: title
	// above a bar of the colour passed, filled to health, from 0 to 1. Bars
	// of different IDs are shown together; "default" is the ID of the bar
	// RemoveBossBar removes.
	SendBossBarID(id string, title text.Text, colour bossbar.Colour, health float64)
	// RemoveBossBar removes the boss bar of the ID "default".
	RemoveBossBar()
	// RemoveBossBarID removes the boss bar of the ID passed.
	RemoveBossBarID(id string)
	// SendScoreboard shows sb in the sidebar, in place of any scoreboard
	// shown there. The client reads sb as it shows it; later changes to sb
	// are shown when it is sent again.
	SendScoreboard(sb *scoreboard.Scoreboard)
	// RemoveScoreboard removes the scoreboard of the sidebar, if any.
	RemoveScoreboard()
	// SendCommandOutput shows the output of a command the player ran.
	SendCommandOutput(o CommandOutput)
}

// Screens opens and closes the screens of the client: forms, dialogues,
// menus and signs. With them, a Client is the menu.Viewer of the menus of
// its player.
type Screens interface {
	// SendForm shows f; its Submit method is called with the answer of the
	// client.
	SendForm(f form.Form)
	// CloseForm closes the forms the client has open, if any.
	CloseForm()
	// SendDialogue shows d as said by the entity e.
	SendDialogue(d dialogue.Dialogue, e world.Entity)
	// CloseDialogue closes the dialogue the client has open, if any.
	CloseDialogue()
	// OpenMenu shows the menu m that the player just opened in tx: the
	// container or the screen of a block, the inventory of an entity, or the
	// trades of a merchant, whose offers ViewTrades shows next. The player
	// registered the client as the viewer of what m is open for before.
	OpenMenu(m *menu.Menu, tx *world.Tx)
	// CloseMenu closes the menu m that the player had open: as the client
	// closed it if clientRequested, or else as the server closes it, which
	// the client is shown. The player releases what m held after.
	CloseMenu(m *menu.Menu, clientRequested bool)
	// ViewTrades shows offers, the offers of merchant, the merchant of the
	// menu m the player has open.
	ViewTrades(m *menu.Menu, merchant entity.Merchant, offers []entity.MerchantOffer)
	// ViewEnchantmentOptions shows the three options of the enchanting table
	// the player has open.
	ViewEnchantmentOptions(options [3]menu.EnchantmentOption)
	// ViewSlotChange shows that a slot of the inventory of the menu the
	// player has open now holds newItem.
	ViewSlotChange(slot int, newItem item.Stack)
	// ViewCrafterSlotState shows whether a slot of the crafter the player
	// has open is disabled.
	ViewCrafterSlotState(slot int, disabled bool)
	// ViewCrafterTriggered shows whether the crafter the player has open is
	// powered.
	ViewCrafterTriggered(triggered bool)
	// OpenSign opens the editor of the sign at pos, of its front side or its
	// back.
	OpenSign(pos cube.Pos, frontSide bool)
}

// HUD controls what the client shows over the world and what input it
// takes: HUD elements, debug shapes, input locks, the coordinates and
// instant respawn.
type HUD interface {
	// ShowHudElement shows e if it is hidden; SendHudUpdates sends the change.
	ShowHudElement(e hud.Element)
	// HideHudElement hides e if it is shown; SendHudUpdates sends the change.
	HideHudElement(e hud.Element)
	// HudElementHidden reports whether e is hidden, or will be once the
	// changes are sent.
	HudElementHidden(e hud.Element) bool
	// SendHudUpdates sends the changes of HUD elements made since it was last
	// called.
	SendHudUpdates()
	// AddDebugShape shows shape, or updates it; SendDebugShapes sends it.
	AddDebugShape(shape debug.Shape)
	// RemoveDebugShape removes shape; SendDebugShapes sends the change.
	RemoveDebugShape(shape debug.Shape)
	// VisibleDebugShapes returns the debug shapes shown.
	VisibleDebugShapes() []debug.Shape
	// RemoveAllDebugShapes removes every debug shape, those not sent yet too.
	RemoveAllDebugShapes()
	// SendDebugShapes sends the changes of debug shapes made since it was
	// last called; dim is the dimension of the world of the player.
	SendDebugShapes(dim world.Dimension)
	// LockInput locks the input l; SendInputLocks sends the change.
	LockInput(l input.Lock)
	// UnlockInput unlocks the input l; SendInputLocks sends the change.
	UnlockInput(l input.Lock)
	// ClearInputLocks unlocks every input; SendInputLocks sends the change.
	ClearInputLocks()
	// InputLocked reports whether the input l is locked.
	InputLocked(l input.Lock) bool
	// SendInputLocks sends the inputs locked.
	SendInputLocks()
	// EnableCoordinates shows the coordinates of the player, or hides them.
	EnableCoordinates(enable bool)
	// EnableInstantRespawn has the player respawn at once when it dies, with
	// no death screen, or not.
	EnableInstantRespawn(enable bool)
}

// Self shows the client the state of its own player.
type Self interface {
	// SendHealth shows the health, maximum health and absorption of the player.
	SendHealth(health, max, absorption float64)
	// SendFood shows the food level, saturation and exhaustion of the player.
	SendFood(food int, saturation, exhaustion float64)
	// SendExperience shows the experience level and the progress to the next.
	SendExperience(level int, progress float64)
	// SendEffect shows an effect newly added to the player.
	SendEffect(e effect.Effect)
	// SendEffectModification shows an effect that replaced one of its type.
	SendEffectModification(e effect.Effect)
	// SendEffectRemoval shows that the effect of type t ended.
	SendEffectRemoval(t effect.Type)
	// SendSpeed shows the speed of the player, in blocks a tick.
	SendSpeed(speed float64)
	// SendAbilities shows the abilities of the player c: flight, its speeds,
	// building and the like.
	SendAbilities(c Controllable)
	// SendGameMode shows the game mode of the player c.
	SendGameMode(c Controllable)
	// SendPermissions shows the permissions of the player c, to it and to the
	// clients that list it, and what commands it may run.
	SendPermissions(c Controllable)
	// SendHeldSlot shows that the player c holds the hotbar slot passed. It
	// does nothing while the client changes the slot itself, unless force is
	// set.
	SendHeldSlot(slot int, c Controllable, force bool)
	// SendRespawn respawns the dead player c at pos.
	SendRespawn(pos mgl64.Vec3, c Controllable)
	// SendPlayerSpawn shows the spawn point of the player.
	SendPlayerSpawn(pos mgl64.Vec3)
	// SetPlayerSpawn shows the personal respawn point of the player, in the
	// dimension dim.
	SetPlayerSpawn(pos cube.Pos, dim world.Dimension)
	// SendChargeItemComplete shows that the item the player charges, such as
	// a crossbow, is charged.
	SendChargeItemComplete()
	// ResyncInventory shows the inventories of the player again, after the
	// client predicted a change the player did not make.
	ResyncInventory()
	// ViewRecipesUnlocked shows the recipes of the Java Edition IDs passed
	// as newly unlocked, those of them the client has a recipe for.
	ViewRecipesUnlocked(javaIDs []string)
	// ShowSkin shows every client the new skin of the player, those that
	// only list it too, and lists it so for clients that join later.
	ShowSkin(sk skin.Skin)
}

// Views shows the client parts of the world beyond world.Viewer: to it
// alone, or differently from other viewers.
type Views interface {
	// ViewSleepingPlayers shows how many players sleep, of the max needed.
	ViewSleepingPlayers(sleeping, max int)
	// ViewElderGuardianCurse shows the ghost of an elder guardian at pos.
	ViewElderGuardianCurse(pos mgl64.Vec3)
	// ViewSpectatedEntity attaches the camera to e, or detaches it for nil.
	ViewSpectatedEntity(e world.Entity)
	// ViewItemCooldown shows the cooldown of item for d.
	ViewItemCooldown(item world.Item, d time.Duration)
	// PlaySound plays s at pos to the client alone.
	PlaySound(s world.Sound, pos mgl64.Vec3)
	// StopSound stops the sound of the name passed, or every sound if all is
	// set. The name is a sound name as sound.Custom has it: the ID of a Java
	// Edition sound event or the name of a sound of a resource pack. It is
	// empty with all.
	StopSound(name string, all bool)
	// ViewNamedParticle shows the particle of the name at pos in dim: a particle
	// type of Java Edition, such as minecraft:flame, or another namespaced name a
	// client may know from its resources, which the adapter passes on.
	ViewNamedParticle(pos mgl64.Vec3, name string, dim world.Dimension)
	// StartShowingEntity shows e again after StopShowingEntity.
	StartShowingEntity(e world.Entity)
	// StopShowingEntity hides e from the client until StartShowingEntity.
	StopShowingEntity(e world.Entity)
	// ViewLayer returns the overrides of how the client views entities, nil
	// if it has none.
	ViewLayer() *world.ViewLayer
	// ViewNameTag shows e with the name tag passed in place of its own.
	ViewNameTag(e world.Entity, nameTag text.Text)
	// ViewPublicNameTag shows e with its own name tag again.
	ViewPublicNameTag(e world.Entity)
	// ViewAlwaysShowNameTag shows the name tag of e at any distance, or only
	// near, in place of its own setting.
	ViewAlwaysShowNameTag(e world.Entity, alwaysShow bool)
	// ViewPublicAlwaysShowNameTag shows the name tag of e by its own setting
	// again.
	ViewPublicAlwaysShowNameTag(e world.Entity)
	// ViewScoreTag shows e with the score tag passed in place of its own.
	ViewScoreTag(e world.Entity, scoreTag text.Text)
	// ViewPublicScoreTag shows e with its own score tag again.
	ViewPublicScoreTag(e world.Entity)
	// ViewVisibility shows e as visible as level says, in place of its own
	// visibility.
	ViewVisibility(e world.Entity, level world.VisibilityLevel)
	// RemoveViewLayer removes every override of how the client views e.
	RemoveViewLayer(e world.Entity)
}
