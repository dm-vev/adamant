package world

import "github.com/go-gl/mathgl/mgl64"

// globalSoundAudibleRange is the distance within which the positional sound
// is still heard, so nearer players are not sent the sound a second time.
const globalSoundAudibleRange = 16

// PlayGlobalSound ports ServerLevel.globalLevelEvent (wither spawning, the
// End portal opening, the ender dragon dying): with global_sound_events on,
// every player in every dimension hears the sound, nearby players at its
// position and everyone else at their own position. With the rule off it is
// an ordinary positional sound.
func (tx *Tx) PlayGlobalSound(pos mgl64.Vec3, s Sound) {
	w := tx.World()
	tx.PlaySound(pos, s)
	if !w.GameRuleBool("global_sound_events") {
		return
	}
	playFar := func(tx *Tx) {
		for p := range tx.Players() {
			if tx.World() == w && p.Position().Sub(pos).Len() < globalSoundAudibleRange {
				continue
			}
			if listener, ok := p.(interface{ PlaySound(Sound) }); ok {
				listener.PlaySound(s)
			}
		}
	}
	playFar(tx)
	w.set.Lock()
	others := make([]*World, 0, len(w.set.worlds))
	for other := range w.set.worlds {
		if other != w {
			others = append(others, other)
		}
	}
	w.set.Unlock()
	for _, other := range others {
		other.Exec(playFar)
	}
}
