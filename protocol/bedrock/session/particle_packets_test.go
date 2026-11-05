package session_test

import (
	"flag"
	"fmt"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/go-gl/mathgl/mgl64"
)

var updateParticles = flag.Bool("update-particles", false, "rewrite testdata/particles.golden, the packets of every particle")

// particleSample is a particle to show in the tests of the mapping of
// particles, by a label that names it in testdata/particles.golden.
type particleSample struct {
	label    string
	particle world.Particle
}

// particleSamples returns the particles to show: the zero value of every type
// of server/world/particle that is a world.Particle, from the generated list,
// so that none is forgotten, and for the types whose packets depend on their
// fields the values that lead to each of their packets.
func particleSamples(t testing.TB) []particleSample {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	var samples []particleSample
	for _, p := range particleTypes {
		samples = append(samples, particleSample{fmt.Sprintf("%T{}", p), p})
	}
	add := func(p world.Particle, detail string) {
		samples = append(samples, particleSample{fmt.Sprintf("%T{%s}", p, detail), p})
	}

	colours := []struct {
		name   string
		colour color.RGBA
	}{
		{"zero", color.RGBA{}},
		{"black", color.RGBA{A: 255}},
		{"red", color.RGBA{R: 255, A: 255}},
		{"mixed", color.RGBA{R: 12, G: 200, B: 99, A: 128}},
		{"transparent", color.RGBA{R: 1}},
	}
	for _, c := range colours {
		add(particle.Flame{Colour: c.colour}, "colour "+c.name)
		add(particle.Dust{Colour: c.colour}, "colour "+c.name)
		add(particle.Splash{Colour: c.colour}, "colour "+c.name)
		add(particle.Effect{Colour: c.colour}, "colour "+c.name)
		for kind := particle.CauldronExplode; kind <= particle.CauldronTakePowderSnow; kind++ {
			add(particle.CauldronEffect{Kind: kind, Colour: c.colour}, fmt.Sprint("kind ", kind, " colour ", c.name))
		}
	}
	// A kind beyond the last has no event.
	add(particle.CauldronEffect{Kind: particle.CauldronTakePowderSnow + 1}, "kind beyond the last")

	for _, b := range []world.Block{block.Stone{}, block.Glass{}, nil} {
		name := fmt.Sprintf("%T", b)
		add(particle.BlockBreak{Block: b}, "block "+name)
		for face := cube.Face(0); face < 6; face++ {
			add(particle.PunchBlock{Block: b, Face: face}, fmt.Sprint("block ", name, " face ", face))
		}
	}
	for face := cube.Face(0); face < 6; face++ {
		add(particle.DispenserSmoke{Face: face}, fmt.Sprint("face ", face))
	}
	for _, b := range []bool{false, true} {
		add(particle.BoneMeal{Area: b}, fmt.Sprint("area ", b))
		add(particle.MobGrowth{Paused: b}, fmt.Sprint("paused ", b))
		add(particle.EyeblossomTransform{Open: b}, fmt.Sprint("open ", b))
		add(particle.TrialSpawnerSpawning{Ominous: b}, fmt.Sprint("ominous ", b))
		add(particle.VaultActivate{Ominous: b}, fmt.Sprint("ominous ", b))
		add(particle.VaultDeactivate{Ominous: b}, fmt.Sprint("ominous ", b))
		for _, players := range []int{0, 1, 4} {
			add(particle.TrialSpawnerDetection{Players: players, Ominous: b}, fmt.Sprint("players ", players, " ominous ", b))
		}
	}
	for _, data := range []int32{0, 1, -1, 1 << 20} {
		add(particle.ElectricSpark{Data: data}, fmt.Sprint("data ", data))
	}

	instruments := []sound.Instrument{
		sound.Piano(), sound.BassDrum(), sound.Snare(), sound.ClicksAndSticks(), sound.Bass(), sound.Flute(),
		sound.Bell(), sound.Guitar(), sound.Chimes(), sound.Xylophone(), sound.IronXylophone(), sound.CowBell(),
		sound.Didgeridoo(), sound.Bit(), sound.Banjo(), sound.Pling(),
	}
	for i, in := range instruments {
		for _, pitch := range []int{0, 7, 24} {
			add(particle.Note{Instrument: in, Pitch: pitch}, fmt.Sprintf("instrument %d pitch %d", i, pitch))
		}
	}

	for _, diff := range []cube.Pos{{}, {1, 2, 3}, {-1, 2, 3}, {1, -2, 3}, {1, 2, -3}, {-4, -5, -6}, {15, 15, 15}} {
		add(particle.DragonEggTeleport{Diff: diff}, fmt.Sprint("diff ", diff))
	}
	for _, charge := range []int{0, 1, 9} {
		for _, faces := range []uint8{0, 1, 0b101010, 0b111111} {
			add(particle.SculkCharge{Charge: charge, Faces: faces}, fmt.Sprint("charge ", charge, " faces ", faces))
		}
	}
	add(particle.CreakingTrail{
		Heart: mgl64.Vec3{1.5, 64, -2.5}, Creaking: mgl64.Vec3{4, 65.25, 7},
		HeartAmount: 3, CreakingAmount: 5,
	}, "trail")
	for _, v := range []particle.VibrationSignal{
		{},
		{Destination: mgl64.Vec3{5, 70, 5}, TravelTicks: 20},
		{Destination: mgl64.Vec3{-5.5, 70.25, 5}, TravelTicks: -3},
		{Destination: mgl64.Vec3{0, 0, 0}, TravelTicks: 1000},
	} {
		add(v, fmt.Sprintf("destination %v ticks %d", v.Destination, v.TravelTicks))
	}
	return samples
}

// TestParticlePacketsMatch shows every particle of particleSamples, which
// holds every type of the particles of the game, and compares the packets as
// each protocol is sent them with testdata/particles.golden, which was
// recorded from the switch of Session.particlePackets before the particles
// were encoded by a table: the table and the cases that remain give the
// packets the switch gave. Run with -update-particles to write it again, and
// read the diff.
func TestParticlePacketsMatch(t *testing.T) {
	var got []string
	for _, c := range particleSamples(t) {
		got = append(got, tablePacketLines(c.label, func() []proto.Packet { return session.ParticlePackets(eventSoundPos, c.particle) })...)
	}
	checkTableGolden(t, filepath.Join("testdata", "particles.golden"), *updateParticles, "particles", got)
}
