package player

import (
	"slices"

	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SetNameTag changes the name tag displayed over the player in-game. Changing the name tag does not change
// the player's name in, for example, the player list or the chat.
func (p *Player) SetNameTag(name string) {
	p.nameTag = name
	p.updateState()
}

// NameTag returns the current name tag of the Player as shown in-game. It can be changed using SetNameTag.
func (p *Player) NameTag() string {
	return p.nameTag
}

// SetAlwaysShowNameTag changes whether the name tag of the player is shown at all distances instead of only
// when the player is looked at from up close. By default, the name tag is always shown.
func (p *Player) SetAlwaysShowNameTag(alwaysShow bool) {
	p.alwaysShowNameTag = alwaysShow
	p.updateState()
}

// AlwaysShowNameTag returns whether the name tag of the player is shown at all distances. It can be changed
// using SetAlwaysShowNameTag.
func (p *Player) AlwaysShowNameTag() bool {
	return p.alwaysShowNameTag
}

// SetScoreTag changes the score tag displayed over the player in-game. The score tag is displayed under the player's
// name tag. It is formatted following the rules of fmt.Sprintln without a newline at the end; a lone text.Text is
// stored as its legacy string, with § codes, as ScoreTag returns the score tag.
func (p *Player) SetScoreTag(a ...any) {
	tag := format(a)
	if len(a) == 1 {
		if t, ok := a[0].(text.Text); ok {
			tag = t.Legacy()
		}
	}
	p.scoreTag = tag
	p.updateState()
}

// ScoreTag returns the current score tag of the player. It can be changed using SetScoreTag and by default is empty.
func (p *Player) ScoreTag() string {
	return p.scoreTag
}

// HideEntity hides a world.Entity from the Player so that it can under no circumstance see it. Hidden entities can be
// made visible again through a call to ShowEntity.
func (p *Player) HideEntity(e world.Entity) {
	if p.H() != e.H() {
		p.client().StopShowingEntity(e)
	}
}

// ShowEntity shows a world.Entity previously hidden from the Player using HideEntity. It does nothing if the entity
// wasn't currently hidden.
func (p *Player) ShowEntity(e world.Entity) {
	p.client().StartShowingEntity(e)
}

// ViewLayer returns the ViewLayer attached to the player's session.
func (p *Player) ViewLayer() *world.ViewLayer {
	return p.client().ViewLayer()
}

// ViewNameTag overrides the public name tag of the entity for this player. The name tag is a text.Text, taken
// as it is, or a string or another value, formatted by fmt and read as a legacy string, with § codes.
func (p *Player) ViewNameTag(entity world.Entity, nameTag any) {
	p.client().ViewNameTag(entity, textOf(nameTag))
}

// ViewPublicNameTag removes the name tag override of the entity for this player.
func (p *Player) ViewPublicNameTag(entity world.Entity) {
	p.client().ViewPublicNameTag(entity)
}

// ViewAlwaysShowNameTag overrides whether the entity's name tag is shown at all distances for this player.
func (p *Player) ViewAlwaysShowNameTag(entity world.Entity, alwaysShow bool) {
	p.client().ViewAlwaysShowNameTag(entity, alwaysShow)
}

// ViewPublicAlwaysShowNameTag removes the always-show name tag override of the entity for this player.
func (p *Player) ViewPublicAlwaysShowNameTag(entity world.Entity) {
	p.client().ViewPublicAlwaysShowNameTag(entity)
}

// ViewScoreTag overrides the public score tag of the entity for this player. The score tag is read as
// ViewNameTag reads a name tag.
func (p *Player) ViewScoreTag(entity world.Entity, scoreTag any) {
	p.client().ViewScoreTag(entity, textOf(scoreTag))
}

// ViewPublicScoreTag removes the score tag override of the entity for this player.
func (p *Player) ViewPublicScoreTag(entity world.Entity) {
	p.client().ViewPublicScoreTag(entity)
}

// ViewVisibility overrides the public visibility of the entity for this player.
func (p *Player) ViewVisibility(entity world.Entity, level world.VisibilityLevel) {
	p.client().ViewVisibility(entity, level)
}

// RemoveViewLayer removes all view-layer overrides of the entity for this player.
func (p *Player) RemoveViewLayer(entity world.Entity) {
	p.client().RemoveViewLayer(entity)
}

// updateState updates the state of the player to all viewers of the player.
func (p *Player) updateState() {
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityState(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// viewers returns a list of all viewers of the Player.
func (p *Player) viewers() ([]world.Viewer, bool) {
	viewers, ok := borrowViewers(p.tx, p.Position())
	// viewers() centralises acquisition of the pooled slice so every broadcast path can share the same reuse logic.
	// We append the session lazily if needed, but crucially we still return the borrowed slice so callers can hand it
	// back to the pool once done.
	if !ok {
		if p.c != nil {
			return []world.Viewer{p.c}, false
		}
		return nil, false
	}
	if p.c != nil && slices.Index(viewers, world.Viewer(p.c)) == -1 {
		viewers = append(viewers, p.c)
	}
	return viewers, true
}

// withinChunkRadius checks if the position provided is within the chunk radius of the player.
func (p *Player) withinChunkRadius(pos mgl64.Vec3) bool {
	playerChunkX, playerChunkZ := int(p.Position().X())>>4, int(p.Position().Z())>>4
	posChunkX, posChunkZ := int(pos.X())>>4, int(pos.Z())>>4
	dx, dz := playerChunkX-posChunkX, playerChunkZ-posChunkZ
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	r := int(p.client().ChunkRadius())
	return dx <= r && dz <= r
}
