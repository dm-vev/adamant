package session_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/go-gl/mathgl/mgl64"
)

var updateSounds = flag.Bool("update-sounds", false, "rewrite testdata/sounds.golden, the packets of every sound")

// soundSample is a sound to play in the tests of the mapping of sounds, by a
// label that names it in testdata/sounds.golden.
type soundSample struct {
	label string
	sound world.Sound
}

// soundSamples returns the sounds to play: the zero value of every type of
// server/world/sound that is a world.Sound, from the generated list, so that
// none is forgotten, and for the types whose packets depend on their fields
// the values that lead to each of their packets.
func soundSamples(t testing.TB) []soundSample {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	var samples []soundSample
	for _, s := range soundTypes {
		samples = append(samples, soundSample{fmt.Sprintf("%T{}", s), s})
	}
	add := func(s world.Sound, detail string) {
		samples = append(samples, soundSample{fmt.Sprintf("%T{%s}", s, detail), s})
	}

	for _, c := range eventSoundCases(t) {
		samples = append(samples, soundSample{"sound.Event " + c.site, c.sound})
	}
	add(sound.Event{Name: "ambient", Entity: "minecraft:cow"}, "ambient cow")
	add(sound.Event{Name: "straw_bed.break_leave", Entity: "minecraft:cow"}, "straw bed with an entity")
	add(sound.Event{Name: "straw_bed.break_leave", Block: block.Stone{}}, "straw bed with a block")
	add(sound.Event{Name: "step", Block: block.Stone{}, Source: eventSoundSources.trader}, "all fields")
	add(sound.Event{}, "zero")

	blocks := map[string]world.Block{"stone": block.Stone{}, "glass": block.Glass{}}
	for _, name := range []string{"stone", "glass"} {
		b := blocks[name]
		for _, s := range []world.Sound{
			sound.BlockPlace{Block: b}, sound.BlockBreaking{Block: b}, sound.DoorOpen{Block: b},
			sound.DoorClose{Block: b}, sound.TrapdoorOpen{Block: b}, sound.TrapdoorClose{Block: b},
			sound.FenceGateOpen{Block: b}, sound.FenceGateClose{Block: b}, sound.ItemUseOn{Block: b},
			sound.BlockBounce{Block: b},
		} {
			add(s, "block "+name)
		}
	}

	for charge := 0; charge <= 5; charge++ {
		add(sound.RespawnAnchorAmbient{Charge: charge}, fmt.Sprint("charge ", charge))
		add(sound.RespawnAnchorCharge{Charge: charge}, fmt.Sprint("charge ", charge))
		add(sound.RespawnAnchorDeplete{Charge: charge}, fmt.Sprint("charge ", charge))
	}

	instruments := []sound.Instrument{
		sound.Piano(), sound.BassDrum(), sound.Snare(), sound.ClicksAndSticks(), sound.Bass(), sound.Flute(),
		sound.Bell(), sound.Guitar(), sound.Chimes(), sound.Xylophone(), sound.IronXylophone(), sound.CowBell(),
		sound.Didgeridoo(), sound.Bit(), sound.Banjo(), sound.Pling(),
	}
	for i, in := range instruments {
		for _, pitch := range []int{0, 1, 12, 24} {
			add(sound.Note{Instrument: in, Pitch: pitch}, fmt.Sprintf("instrument %d pitch %d", i, pitch))
		}
	}

	for _, name := range []string{"", "entity.allay.item_given", "block.growing_plant.crop", "entity.bee.sting", "my_pack.sound", "ambient.cave"} {
		for _, vp := range [][2]float64{{0, 0}, {1, 1}, {0.5, 1.5}} {
			add(sound.Custom{Name: name, Volume: vp[0], Pitch: vp[1]}, fmt.Sprintf("%q volume %v pitch %v", name, vp[0], vp[1]))
		}
	}

	tiers := map[string]item.ArmourTier{
		"leather": item.ArmourTierLeather{}, "copper": item.ArmourTierCopper{}, "gold": item.ArmourTierGold{},
		"chain": item.ArmourTierChain{}, "iron": item.ArmourTierIron{}, "diamond": item.ArmourTierDiamond{},
		"netherite": item.ArmourTierNetherite{}, "nil": nil,
	}
	for _, name := range []string{"leather", "copper", "gold", "chain", "iron", "diamond", "netherite", "nil"} {
		tier := tiers[name]
		add(sound.EquipItem{Item: item.Helmet{Tier: tier}}, "helmet "+name)
		add(sound.EquipItem{Item: item.Chestplate{Tier: tier}}, "chestplate "+name)
		add(sound.EquipItem{Item: item.Leggings{Tier: tier}}, "leggings "+name)
		add(sound.EquipItem{Item: item.Boots{Tier: tier}}, "boots "+name)
	}
	add(sound.EquipItem{Item: item.Elytra{}}, "elytra")
	add(sound.EquipItem{Item: item.Stick{}}, "stick")
	add(sound.EquipItem{}, "nil item")

	for i, h := range sound.GoatHorns() {
		add(sound.GoatHorn{Horn: h}, fmt.Sprint("horn ", i))
	}
	for i, d := range sound.MusicDiscs() {
		add(sound.MusicDiscPlay{DiscType: d}, fmt.Sprint("disc ", i))
	}

	for _, d := range []float64{-1, 0, 3.9, 4, 4.01, 20} {
		add(sound.Fall{Distance: d}, fmt.Sprint("distance ", d))
	}
	for _, b := range []bool{false, true} {
		add(sound.Attack{Damage: b}, fmt.Sprint("damage ", b))
		add(sound.GeyserEruptionStart{Continuous: b}, fmt.Sprint("continuous ", b))
		add(sound.MobGrowth{Paused: b}, fmt.Sprint("paused ", b))
		add(sound.PointedDripstoneCauldronDrip{Lava: b}, fmt.Sprint("lava ", b))
		add(sound.BigDripleafTilt{Up: b}, fmt.Sprint("up ", b))
	}
	for _, c := range []struct {
		name   string
		liquid world.Liquid
	}{{"water", block.Water{}}, {"lava", block.Lava{}}, {"nil", nil}} {
		add(sound.BucketFill{Liquid: c.liquid}, c.name)
		add(sound.BucketEmpty{Liquid: c.liquid}, c.name)
	}
	for stage := -1; stage <= 4; stage++ {
		for _, quick := range []bool{false, true} {
			add(sound.CrossbowLoad{Stage: stage, QuickCharge: quick}, fmt.Sprint("stage ", stage, " quick ", quick))
		}
	}
	for level := -1; level <= 4; level++ {
		add(sound.TridentRiptide{Level: level}, fmt.Sprint("level ", level))
	}
	for _, p := range []float64{0, 0.25, 1} {
		add(sound.DecoratedPotInserted{Progress: p}, fmt.Sprint("progress ", p))
	}
	for _, p := range []float64{0, 0.2, 0.6, 1} {
		add(sound.CreakingHeartTrail{Progress: p}, fmt.Sprint("progress ", p))
	}
	return samples
}

// soundProtocols are the protocols the packets of a sound are encoded for:
// that of the model the session is compiled against, and 2193, whose clients
// are sent what protocol/bedrock/internal/versions converts them to.
func soundProtocols() []*versions.Protocol {
	return []*versions.Protocol{versions.Model(), oldProtocol()}
}

// encodeSoundPackets returns the lines that tell the packets passed as each
// of the protocols is sent them: the ID and the bytes of each, or none.
func encodeSoundPackets(pks []proto.Packet) []string {
	var lines []string
	for _, p := range soundProtocols() {
		var out []proto.Packet
		for _, pk := range pks {
			if p.Converts() {
				out = append(out, p.Out(pk)...)
			} else {
				out = append(out, pk)
			}
		}
		if len(out) == 0 {
			lines = append(lines, fmt.Sprintf("  v%d none", p.Number))
		}
		for _, pk := range out {
			lines = append(lines, fmt.Sprintf("  v%d id=%d %x", p.Number, pk.ID(), encode(pk)))
		}
	}
	return lines
}

// soundPacketLines returns the lines of testdata/sounds.golden of one sound
// played with play, a panic of the play written as one.
func soundPacketLines(label string, play func(pos mgl64.Vec3, t world.Sound) []proto.Packet, s world.Sound) (lines []string) {
	lines = []string{label}
	defer func() {
		if r := recover(); r != nil {
			lines = append(lines, fmt.Sprint("  panic: ", r))
		}
	}()
	lines = append(lines, encodeSoundPackets(play(eventSoundPos, s))...)
	return lines
}

// TestSoundPacketsMatch plays every sound of soundSamples, which holds every
// type of the sounds of the game, and compares the packets as each protocol
// is sent them with testdata/sounds.golden, which was recorded from the
// switch of Session.soundPackets before the sounds were encoded by a table.
// Run with -update-sounds to write it again.
func TestSoundPacketsMatch(t *testing.T) {
	var got []string
	for _, c := range soundSamples(t) {
		lines := soundPacketLines(c.label, session.SoundPackets, c.sound)
		if tabled := soundPacketLines(c.label, session.TabledSoundPackets, c.sound); strings.Join(tabled, "\n") != strings.Join(lines, "\n") {
			t.Errorf("%s: the table gives\n%s\nthe switch gave\n%s", c.label, strings.Join(tabled, "\n"), strings.Join(lines, "\n"))
		}
		got = append(got, lines...)
	}
	file := filepath.Join("testdata", "sounds.golden")
	out := strings.Join(got, "\n") + "\n"
	if *updateSounds {
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != out {
		t.Errorf("the packets of the sounds differ from %s (run with -update-sounds to see the change as a diff)", file)
		wl, gl := strings.Split(string(want), "\n"), strings.Split(out, "\n")
		for i := 0; i < min(len(wl), len(gl)); i++ {
			if wl[i] != gl[i] {
				t.Fatalf("first difference at line %d:\ngolden: %s\ngot:    %s", i+1, wl[i], gl[i])
			}
		}
		t.Fatalf("golden has %d lines, got %d", len(wl), len(gl))
	}
}
