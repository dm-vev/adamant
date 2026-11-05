package client

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The role interfaces below name what a client asks of its player, in groups
// that a Bedrock Edition session handles together. Controllable embeds all of
// them, so an adapter that needs one role only may ask for that role.

// Mover is the movement of a player: the moves its client reports, the flags
// it sets and the vehicle it steers.
type Mover interface {
	// ClientMove moves the player as its client moved it in a tick, unless
	// the game refuses the move, and reports what it made of it.
	ClientMove(m Move) MoveResult

	// Speed, FlightSpeed and VerticalFlightSpeed are the speeds the game
	// validates a move against.
	Speed() float64
	FlightSpeed() float64
	VerticalFlightSpeed() float64

	Jump()

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

	SteerVehicle(strafe, forward float64, jumping bool) (riding bool)
	DashVehicle() bool
	RideJump() bool
	DismountVehicle() bool
	LeaveVehicle(e world.Entity)
}

// Breaker is the breaking of blocks and what goes with it: the stages of
// breaking a block, the instant destruction, picking a block and the reach.
type Breaker interface {
	StartBreaking(pos cube.Pos, face cube.Face)
	ContinueBreaking(face cube.Face)
	FinishBreaking()
	AbortBreaking()

	// DestroyBlock breaks the block at pos at once, as the client did:
	// in creative, or a block that breaks in a tick.
	DestroyBlock(pos cube.Pos)
	PickBlock(pos cube.Pos)
	CanReach(pos mgl64.Vec3) bool
}

// ItemUser is what the player does with the items it holds: using them on
// blocks, entities and in the air, attacking, and moving them around. UseItem,
// ReleaseItem, SetHeldItems and Drop were added after the role was named and
// belong to it.
type ItemUser interface {
	UseItem()
	ReleaseItem()
	UseItemOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3)
	UseItemOnEntity(e world.Entity) bool
	AttackEntity(e world.Entity) bool

	SetHeldItems(right, left item.Stack)
	SetHeldSlot(slot int) error
	Drop(s item.Stack) (n int)
	DropFromInventory(slot, count int) error
	MoveItemsToInventory()

	SwingArm()
	PunchAir()
}

// Editor is the editing of what a block, an entity or an item holds, as the
// client's screens for them submit it. OpenSign, which opens the screen that
// EditSign submits, belongs to it.
type Editor interface {
	OpenSign(pos cube.Pos, frontSide bool)
	EditSign(pos cube.Pos, hanging bool, frontText, backText string) error
	EditBook(slot int, edit item.BookEdit) error
	EditCommandBlock(pos cube.Pos, edit block.CommandBlockEdit) error
	EditCommandMinecart(e world.Entity, edit block.CommandBlockEdit) error
	EditStructureBlock(pos cube.Pos, edit block.StructureBlockEdit, trigger bool) error
	EditJigsaw(pos cube.Pos, edited block.Jigsaw) error
	TurnLecternPage(pos cube.Pos, page int) error
	SetCrafterSlotDisabled(pos cube.Pos, slot int, disabled bool) error
	CanUseGameMasterBlocks() bool
	ExploreMap(id int64) (world.MapData, error)
}

// MenuUser is the menu a player has open. CloseMenu was added after the role
// was named and belongs to it.
type MenuUser interface {
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
}

// Speaker is what a player says and does for others to see: chat, commands
// and emotes.
type Speaker interface {
	Chat(msg ...any)
	ExecuteCommand(commandLine string)
	Emote(emote uuid.UUID)
}

// Respawner is the player's life cycle as its client drives it: coming back
// after death and going to bed. Dead, which tells whether there is anything to
// respawn from, belongs to it.
type Respawner interface {
	Respawn() *world.EntityHandle
	Dead() bool

	Sleep(pos cube.Pos)
	Wake()
}
