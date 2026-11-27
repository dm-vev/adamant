package builtin

import (
	"slices"
	"strings"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

func registerPlayerSoundCommands(_ serverAdapter) {
	cmd.Register(newCommand("playsound", nil, playSoundCommand{}))
	cmd.Register(newCommand("stopsound", nil, stopSoundCommand{}))
	cmd.Register(newCommand("particle", nil, spawnParticleCommand{}))
}

type playSoundCommand struct {
	gameMaster
	Sound     string                    `cmd:"sound"`
	Source    cmd.Optional[string]      `cmd:"source"`
	Targets   cmd.Optional[cmd.Players] `cmd:"targets"`
	Position  cmd.Optional[mgl64.Vec3]  `cmd:"pos"`
	Volume    cmd.Optional[float64]     `cmd:"volume"`
	Pitch     cmd.Optional[float64]     `cmd:"pitch"`
	MinVolume cmd.Optional[float64]     `cmd:"minVolume"`
}

func (p playSoundCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos := mgl64.Vec3{}
	if s, ok := src.(interface{ Position() mgl64.Vec3 }); ok {
		pos = s.Position()
	}
	if v, ok := p.Position.Load(); ok {
		pos = v
	}
	volume, pitch := 1.0, 1.0
	if v, ok := p.Volume.Load(); ok {
		volume = v
	}
	if v, ok := p.Pitch.Load(); ok {
		pitch = v
	}
	snd := sound.Custom{Name: p.Sound, Volume: volume, Pitch: pitch}
	if targets, ok := p.Targets.Load(); ok {
		name := p.Sound
		cmd.ForEach(src, o, tx, targets,
			func(_ *world.Tx, t cmd.Target) int {
				pl, ok := t.(*player.Player)
				if !ok {
					return 0
				}
				pl.PlaySound(snd)
				return 1
			},
			func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
				tally := cmd.TallyOf(rs)
				if tally.NonZero == 0 {
					o.Error(cmd.ErrNoPlayerFound)
					return
				}
				o.SetResult(tally.Total)
				o.Printf("Played sound %s.", name)
			},
		)
		return
	}
	if tx == nil {
		o.Error(cmd.ErrNoPlayerFound)
		return
	}
	viewers := tx.Viewers(pos)
	for _, v := range viewers {
		v.ViewSound(pos, snd)
	}
	tx.ReleaseViewers(viewers)
	o.Printf("Played sound %s.", p.Sound)
}

type stopSoundCommand struct {
	gameMaster
	Targets cmd.Players          `cmd:"targets"`
	Source  cmd.Optional[string] `cmd:"source"`
	Sound   cmd.Optional[string] `cmd:"sound"`
}

func (s stopSoundCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	name := ""
	if v, ok := s.Sound.Load(); ok {
		name = v
	}
	cmd.ForEach(src, o, tx, s.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			p.StopSound(name)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Stopped sounds for %s.", joinNonZeroNames(rs))
		},
	)
}

type spawnParticleCommand struct {
	gameMaster
	Name     string                    `cmd:"name"`
	Position cmd.Optional[mgl64.Vec3]  `cmd:"pos"`
	Delta    cmd.Optional[mgl64.Vec3]  `cmd:"delta"`
	Speed    cmd.Optional[float64]     `cmd:"speed"`
	Count    cmd.Optional[int]         `cmd:"count"`
	Mode     cmd.Optional[string]      `cmd:"mode"`
	Viewers  cmd.Optional[cmd.Players] `cmd:"viewers"`
}

func (p spawnParticleCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos := mgl64.Vec3{}
	if s, ok := src.(interface{ Position() mgl64.Vec3 }); ok {
		pos = s.Position()
	}
	if v, ok := p.Position.Load(); ok {
		pos = v
	}
	name := particleName(p.Name)
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, p.Name)
		return
	}
	if viewers, ok := p.Viewers.Load(); ok {
		cmd.ForEach(src, o, tx, viewers,
			func(_ *world.Tx, t cmd.Target) int {
				pl, ok := t.(*player.Player)
				if !ok {
					return 0
				}
				pl.ViewNamedParticle(pos, name)
				return 1
			},
			func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
				tally := cmd.TallyOf(rs)
				if tally.NonZero == 0 {
					o.Error(cmd.ErrNoPlayerFound)
					return
				}
				o.SetResult(tally.Total)
				o.Printf("Spawned particle %s.", name)
			},
		)
		return
	}
	if tx != nil {
		// Java ParticleCommand's default viewers are every player in the level;
		// the client culls by its own particle range.
		for e := range tx.Players() {
			if pl, ok := e.(*player.Player); ok {
				pl.ViewNamedParticle(pos, name)
			}
		}
	}
	o.Printf("Spawned particle %s.", name)
}

// particleName returns the name /particle shows the particle of the name
// passed by: minecraft: and the name of a particle type of Java Edition the
// game knows, with or without its namespace; any other namespaced name as it
// is, for particles clients may know from their resources; and "" for a
// name of neither form.
func particleName(name string) string {
	if short := strings.TrimPrefix(name, "minecraft:"); slices.Contains(particleNameAliases, short) {
		return "minecraft:" + short
	}
	if strings.Contains(name, ":") {
		return name
	}
	return ""
}

// javaParticleNames are the particle types of Java Edition, without their
// namespace, that /particle takes by name.
var particleNameAliases = []string{
	"angry_villager", "cloud", "end_rod", "explosion", "explosion_emitter", "flame", "happy_villager",
	"heart", "large_smoke", "poof", "portal", "smoke", "soul_fire_flame", "totem_of_undying",
}
