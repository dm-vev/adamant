package hud

import (
	"github.com/df-mc/dragonfly/server/player"
	phud "github.com/df-mc/dragonfly/server/player/hud"
	pinput "github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Type aliases for HUD elements and client input locks.
type (
	Element   = phud.Element
	InputLock = pinput.Lock
)

// Common HUD element constructors.
var (
	PaperDoll     = phud.PaperDoll
	Armour        = phud.Armour
	ToolTips      = phud.ToolTips
	TouchControls = phud.TouchControls
	Crosshair     = phud.Crosshair
	HotBar        = phud.HotBar
	Health        = phud.Health
	ProgressBar   = phud.ProgressBar
	Hunger        = phud.Hunger
	AirBubbles    = phud.AirBubbles
	HorseHealth   = phud.HorseHealth
	StatusEffects = phud.StatusEffects
	ItemText      = phud.ItemText
	AllElements   = phud.All
)

// Common client input lock constructors.
var (
	LockCamera          = pinput.Camera
	LockMovement        = pinput.Movement
	LockLateralMovement = pinput.LateralMovement
	LockSneak           = pinput.Sneak
	LockJump            = pinput.Jump
	LockMount           = pinput.Mount
	LockDismount        = pinput.Dismount
	LockMoveForward     = pinput.MoveForward
	LockMoveBackward    = pinput.MoveBackward
	LockMoveLeft        = pinput.MoveLeft
	LockMoveRight       = pinput.MoveRight
	AllInputLocks       = pinput.All
)

// HideHUD hides one or more HUD elements from p.
func HideHUD(p *player.Player, elems ...Element) {
	if p == nil {
		return
	}
	for _, e := range elems {
		p.HideHudElement(e)
	}
}

// ShowHUD shows one or more HUD elements to p.
func ShowHUD(p *player.Player, elems ...Element) {
	if p == nil {
		return
	}
	for _, e := range elems {
		p.ShowHudElement(e)
	}
}

// ResetHUD restores visibility of every HUD element for p.
func ResetHUD(p *player.Player) {
	ShowHUD(p, AllElements()...)
}

// HUDHidden reports whether elem is currently hidden for p.
func HUDHidden(p *player.Player, elem Element) bool {
	return p != nil && p.HudElementHidden(elem)
}

// ShowCoordinates enables the on-screen coordinates display for p.
func ShowCoordinates(p *player.Player) {
	if p != nil {
		p.ShowCoordinates()
	}
}

// HideCoordinates disables the on-screen coordinates display for p.
func HideCoordinates(p *player.Player) {
	if p != nil {
		p.HideCoordinates()
	}
}

// LockInput applies one or more client input locks to p (e.g. freezing camera
// or movement during cutscenes or tutorials).
func LockInput(p *player.Player, locks ...InputLock) {
	if p == nil {
		return
	}
	for _, l := range locks {
		p.LockInput(l)
	}
}

// UnlockInput removes one or more client input locks from p.
func UnlockInput(p *player.Player, locks ...InputLock) {
	if p == nil {
		return
	}
	for _, l := range locks {
		p.UnlockInput(l)
	}
}

// ClearInputLocks removes every active input lock on p.
func ClearInputLocks(p *player.Player) {
	if p != nil {
		p.ClearInputLocks()
	}
}

// InputLocked reports whether lock is currently applied to p.
func InputLocked(p *player.Player, lock InputLock) bool {
	return p != nil && p.InputLocked(lock)
}

// ShowParticleFor displays a particle exclusively to player p at pos.
func ShowParticleFor(p *player.Player, pos mgl64.Vec3, pt world.Particle) {
	if p != nil && pt != nil {
		p.ShowParticle(pos, pt)
	}
}

// SetNameTag updates the floating display name shown above p.
func SetNameTag(p *player.Player, name string) {
	if p != nil {
		p.SetNameTag(name)
	}
}

// SetScoreTag sets the secondary line displayed directly below p's nametag
// (commonly used for health bars, clans, or ranks).
func SetScoreTag(p *player.Player, a ...any) {
	if p != nil {
		p.SetScoreTag(a...)
	}
}

// HideEntity hides target exclusively from viewer.
func HideEntity(viewer *player.Player, target world.Entity) {
	if viewer != nil && target != nil {
		viewer.HideEntity(target)
	}
}

// ShowEntity reveals a previously hidden target exclusively to viewer.
func ShowEntity(viewer *player.Player, target world.Entity) {
	if viewer != nil && target != nil {
		viewer.ShowEntity(target)
	}
}

// Vanish hides target from every player in viewers (skipping target itself).
func Vanish(target world.Entity, viewers ...*player.Player) {
	if target == nil {
		return
	}
	for _, v := range viewers {
		if v != nil && v.H() != target.H() {
			v.HideEntity(target)
		}
	}
}

// Unvanish reveals target to every player in viewers.
func Unvanish(target world.Entity, viewers ...*player.Player) {
	if target == nil {
		return
	}
	for _, v := range viewers {
		if v != nil && v.H() != target.H() {
			v.ShowEntity(target)
		}
	}
}

// HideFrom hides every line entity of this Hologram from viewer in tx.
func (h *Hologram) HideFrom(viewer *player.Player, tx *world.Tx) {
	if h == nil || viewer == nil || tx == nil || tx.Closed() {
		return
	}
	for _, handle := range h.handles {
		if ent, ok := handle.Entity(tx); ok {
			viewer.HideEntity(ent)
		}
	}
}

// ShowTo reveals every line entity of this Hologram to viewer in tx.
func (h *Hologram) ShowTo(viewer *player.Player, tx *world.Tx) {
	if h == nil || viewer == nil || tx == nil || tx.Closed() {
		return
	}
	for _, handle := range h.handles {
		if ent, ok := handle.Entity(tx); ok {
			viewer.ShowEntity(ent)
		}
	}
}
