package client

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// Controllable is a player as its client drives it: what the client of a
// player, such as a Bedrock Edition session, may ask of the player as it
// handles what the client sends. *player.Player implements it. Methods are
// added as clients need them.
//
// The game checks what a client asks: ClientMove and DestroyBlock may
// refuse, and the other actions refuse as they refuse a plugin out of
// reach.
type Controllable interface {
	Name() string
	world.Entity
	item.User
	dialogue.Submitter
	form.Submitter
	cmd.Source
	chat.Subscriber
	hud.Renderer
	debug.Renderer
	input.Restricter

	Locale() language.Tag

	SetHeldItems(right, left item.Stack)
	SetHeldSlot(slot int) error

	// ClientMove moves the player as its client moved it in a tick, unless
	// the game refuses the move, and reports what it made of it.
	ClientMove(m Move) MoveResult

	Speed() float64
	FlightSpeed() float64
	VerticalFlightSpeed() float64

	Sleep(pos cube.Pos)
	Wake()

	Chat(msg ...any)
	ExecuteCommand(commandLine string)
	GameMode() world.GameMode
	SetGameMode(mode world.GameMode)
	Effects() []effect.Effect

	UseItem()
	ReleaseItem()
	UseItemOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3)
	UseItemOnEntity(e world.Entity) bool
	// DestroyBlock breaks the block at pos at once, as the client did:
	// in creative, or a block that breaks in a tick.
	DestroyBlock(pos cube.Pos)
	PickBlock(pos cube.Pos)
	AttackEntity(e world.Entity) bool
	Drop(s item.Stack) (n int)
	DropFromInventory(slot, count int) error
	SwingArm()
	PunchAir()
	Emote(emote uuid.UUID)

	Health() float64
	MaxHealth() float64
	Absorption() float64
	Food() int

	ExperienceLevel() int
	ExperienceProgress() float64

	Respawn() *world.EntityHandle
	Dead() bool

	StartSneaking()
	Sneaking() bool
	StopSneaking()
	StartSprinting()
	Sprinting() bool
	StopSprinting()
	StartSwimming()
	Swimming() bool
	StopSwimming()
	StartCrawling()
	Crawling() bool
	StopCrawling()
	StartFlying()
	Flying() bool
	StopFlying()
	StartGliding()
	Gliding() bool
	StopGliding()
	Jump()

	SteerVehicle(strafe, forward float64, jumping bool) (riding bool)
	DashVehicle() bool
	RideJump() bool
	DismountVehicle() bool
	LeaveVehicle(e world.Entity)

	StartBreaking(pos cube.Pos, face cube.Face)
	ContinueBreaking(face cube.Face)
	FinishBreaking()
	AbortBreaking()

	Exhaust(points float64)

	OpenSign(pos cube.Pos, frontSide bool)
	CanReach(pos mgl64.Vec3) bool
	CanUseGameMasterBlocks() bool

	EditSign(pos cube.Pos, hanging bool, frontText, backText string) error
	TurnLecternPage(pos cube.Pos, page int) error
	EditJigsaw(pos cube.Pos, edited block.Jigsaw) error
	EditCommandBlock(pos cube.Pos, edit block.CommandBlockEdit) error
	EditCommandMinecart(e world.Entity, edit block.CommandBlockEdit) error
	EditStructureBlock(pos cube.Pos, edit block.StructureBlockEdit, trigger bool) error
	SetCrafterSlotDisabled(pos cube.Pos, slot int, disabled bool) error

	EditBook(slot int, edit item.BookEdit) error

	ExploreMap(id int64) (world.MapData, error)

	EnderChestInventory() *inventory.Inventory
	Inventory() *inventory.Inventory
	MoveItemsToInventory()

	// Menu returns the menu the player has open, nil if none.
	Menu() *menu.Menu
	// BeginMenu starts a batch of operations on the inventories of the
	// player and its open menu, whose slots n names as the client's
	// protocol does.
	BeginMenu(n menu.Namer) *menu.Tx
	// CloseMenu closes the menu the player has open, if any: as its client
	// closed it, or, when clientRequested is false, as the server closes it,
	// which the client is shown.
	CloseMenu(clientRequested bool)

	// UUID returns the UUID of the controllable. It must be unique for all controllable entities present in
	// the server.
	UUID() uuid.UUID
	// XUID returns the XBOX Live User ID of the controllable. Every controllable must have one of these if
	// they are authenticated via XBOX Live, as they must be connected to an XBOX Live account.
	XUID() string
	// Skin returns the skin of the controllable. Each controllable must have a skin, as it defines how the
	// entity looks in the world.
	Skin() skin.Skin
	SetSkin(skin.Skin)

	UpdateDiagnostics(Diagnostics)

	// Leave ends the session of the player with its client in tx, the
	// transaction of the world it is in, nil if it is in none, unless it has
	// ended: the player tears itself down as quitting does after its quit
	// handler ran, and closes the client. An adapter calls it when its client
	// went away and the player did not quit at once.
	Leave(tx *world.Tx)
}
