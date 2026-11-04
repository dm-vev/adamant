package client

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
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
)

// Nop is a Client that does nothing: the client of a player that has none.
// Its queries answer as a client that is not there would: the zero Profile,
// a nil address, a latency of 0, zero NetStats, a chunk radius of 0, no move
// of its own, no hidden HUD elements, no debug shapes, no input locks, no
// recipes accepted, no ViewLayer and no error. LastActivity returns
// time.Unix(0, 0), the answer a player without a client always gave.
//
// Nop is zero-sized, so converting it to a Client does not allocate. It may
// be embedded by a Client that implements only some of the methods.
type Nop struct{ world.NopViewer }

// Compile time check to make sure Nop implements Client.
var _ Client = Nop{}

func (Nop) ViewMapData(world.MapData) error { return nil }

func (Nop) Profile() Profile        { return Profile{} }
func (Nop) Addr() net.Addr          { return nil }
func (Nop) Latency() time.Duration  { return 0 }
func (Nop) NetStats() NetStats      { return NetStats{} }
func (Nop) LastActivity() time.Time { return time.Unix(0, 0) }
func (Nop) ChunkRadius() int32      { return 0 }
func (Nop) ClientMoving() bool      { return false }
func (Nop) Transfer(net.IP, int)    {}
func (Nop) Disconnect(text.Text)    {}
func (Nop) Close(*world.Tx)         {}
func (Nop) CloseConnection()        {}
func (Nop) ViewInventories(*world.Tx, Controllable, menu.Inventories, *uint32) InventoryViews {
	return InventoryViews{}
}

func (Nop) SendMessage(text.Text)                                         {}
func (Nop) SendTip(text.Text)                                             {}
func (Nop) SendPopup(text.Text)                                           {}
func (Nop) SendJukeboxPopup(text.Text)                                    {}
func (Nop) SendToast(text.Text, text.Text)                                {}
func (Nop) SetTitleDurations(time.Duration, time.Duration, time.Duration) {}
func (Nop) SendTitle(text.Text)                                           {}
func (Nop) SendSubtitle(text.Text)                                        {}
func (Nop) SendActionBarMessage(text.Text)                                {}
func (Nop) SendBossBarID(string, text.Text, bossbar.Colour, float64)      {}
func (Nop) RemoveBossBar()                                                {}
func (Nop) RemoveBossBarID(string)                                        {}
func (Nop) SendScoreboard(*scoreboard.Scoreboard)                         {}
func (Nop) RemoveScoreboard()                                             {}
func (Nop) SendCommandOutput(CommandOutput)                               {}

func (Nop) SendForm(form.Form)                                             {}
func (Nop) CloseForm()                                                     {}
func (Nop) SendDialogue(dialogue.Dialogue, world.Entity)                   {}
func (Nop) CloseDialogue()                                                 {}
func (Nop) OpenMenu(*menu.Menu, *world.Tx)                                 {}
func (Nop) CloseMenu(*menu.Menu, bool)                                     {}
func (Nop) ViewTrades(*menu.Menu, entity.Merchant, []entity.MerchantOffer) {}
func (Nop) ViewEnchantmentOptions([3]menu.EnchantmentOption)               {}
func (Nop) ViewSlotChange(int, item.Stack)                                 {}
func (Nop) ViewCrafterSlotState(int, bool)                                 {}
func (Nop) ViewCrafterTriggered(bool)                                      {}
func (Nop) OpenSign(cube.Pos, bool)                                        {}

func (Nop) ShowHudElement(hud.Element)        {}
func (Nop) HideHudElement(hud.Element)        {}
func (Nop) HudElementHidden(hud.Element) bool { return false }
func (Nop) SendHudUpdates()                   {}
func (Nop) AddDebugShape(debug.Shape)         {}
func (Nop) RemoveDebugShape(debug.Shape)      {}
func (Nop) VisibleDebugShapes() []debug.Shape { return nil }
func (Nop) RemoveAllDebugShapes()             {}
func (Nop) SendDebugShapes(world.Dimension)   {}
func (Nop) LockInput(input.Lock)              {}
func (Nop) UnlockInput(input.Lock)            {}
func (Nop) ClearInputLocks()                  {}
func (Nop) InputLocked(input.Lock) bool       { return false }
func (Nop) SendInputLocks()                   {}
func (Nop) EnableCoordinates(bool)            {}
func (Nop) EnableInstantRespawn(bool)         {}

func (Nop) SendHealth(float64, float64, float64)     {}
func (Nop) SendFood(int, float64, float64)           {}
func (Nop) SendExperience(int, float64)              {}
func (Nop) SendEffect(effect.Effect)                 {}
func (Nop) SendEffectModification(effect.Effect)     {}
func (Nop) SendEffectRemoval(effect.Type)            {}
func (Nop) SendSpeed(float64)                        {}
func (Nop) SendAbilities(Controllable)               {}
func (Nop) SendGameMode(Controllable)                {}
func (Nop) SendPermissions(Controllable)             {}
func (Nop) SendHeldSlot(int, Controllable, bool)     {}
func (Nop) SendRespawn(mgl64.Vec3, Controllable)     {}
func (Nop) SendPlayerSpawn(mgl64.Vec3)               {}
func (Nop) SetPlayerSpawn(cube.Pos, world.Dimension) {}
func (Nop) SendChargeItemComplete()                  {}
func (Nop) ResyncInventory()                         {}
func (Nop) UnlockRecipes([]string) []string          { return nil }
func (Nop) ShowSkin(skin.Skin)                       {}

func (Nop) ViewSleepingPlayers(int, int)                          {}
func (Nop) ViewElderGuardianCurse(mgl64.Vec3)                     {}
func (Nop) ViewSpectatedEntity(world.Entity)                      {}
func (Nop) ViewItemCooldown(world.Item, time.Duration)            {}
func (Nop) PlaySound(world.Sound, mgl64.Vec3)                     {}
func (Nop) StopSound(string, bool)                                {}
func (Nop) ViewNamedParticle(mgl64.Vec3, string, world.Dimension) {}
func (Nop) StartShowingEntity(world.Entity)                       {}
func (Nop) StopShowingEntity(world.Entity)                        {}
func (Nop) ViewLayer() *world.ViewLayer                           { return nil }
func (Nop) ViewNameTag(world.Entity, text.Text)                   {}
func (Nop) ViewPublicNameTag(world.Entity)                        {}
func (Nop) ViewAlwaysShowNameTag(world.Entity, bool)              {}
func (Nop) ViewPublicAlwaysShowNameTag(world.Entity)              {}
func (Nop) ViewScoreTag(world.Entity, text.Text)                  {}
func (Nop) ViewPublicScoreTag(world.Entity)                       {}
func (Nop) ViewVisibility(world.Entity, world.VisibilityLevel)    {}
func (Nop) RemoveViewLayer(world.Entity)                          {}
