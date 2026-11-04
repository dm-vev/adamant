package player

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// StopSound stops a named sound, or all sounds if name is empty.
func (p *Player) StopSound(name string) {
	p.client().StopSound(name, name == "")
}

// ViewNamedParticle shows the player the particle of the name at pos, in the
// Overworld: a particle type of Java Edition, such as minecraft:flame, or
// another namespaced name the client may know from its resources.
func (p *Player) ViewNamedParticle(pos mgl64.Vec3, name string) {
	p.client().ViewNamedParticle(pos, name, world.Overworld)
}
