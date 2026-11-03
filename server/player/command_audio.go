package player

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// StopSound stops a named sound, or all sounds if name is empty.
func (p *Player) StopSound(name string) {
	p.session().StopSound(name, name == "")
}

// SpawnParticleIdentifier sends a Bedrock particle identifier at the given position, in the Overworld.
func (p *Player) SpawnParticleIdentifier(pos mgl64.Vec3, id string) {
	p.session().SpawnParticleIdentifier(pos, id, world.Overworld)
}
