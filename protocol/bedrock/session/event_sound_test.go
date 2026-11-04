package session_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// eventSoundCase is a sound of an event as a site of the game plays it. The
// sites are those that played a sound.NativeEvent.
type eventSoundCase struct {
	// site is the file and line of the game that plays the sound.
	site  string
	sound world.Sound
}

// eventSoundPos is where every case is played. Where a sound is played is
// the game's: the session only copies the position.
var eventSoundPos = mgl64.Vec3{1.5, 64.5, -2.5}

// eventSoundSources are the entities the cases with a source are played by,
// of fixed UUIDs, so that their unique IDs are the same in every run.
var eventSoundSources = struct{ trader, creaking *world.EntityHandle }{
	trader:   entity.NewWanderingTrader(world.EntitySpawnOpts{ID: uuid.MustParse("00000000-0000-0000-0102-030405060708")}),
	creaking: entity.NewCreaking(world.EntitySpawnOpts{ID: uuid.MustParse("00000000-0000-0000-1112-131415161718")}),
}

// lightWeightedPressurePlate returns a light weighted pressure plate of the
// signal passed.
func lightWeightedPressurePlate(t testing.TB, signal int) block.PressurePlate {
	t.Helper()
	for _, typ := range block.PressurePlateTypes() {
		p := block.PressurePlate{Type: typ, Signal: signal}
		if name, _ := p.EncodeBlock(); name == "minecraft:light_weighted_pressure_plate" {
			return p
		}
	}
	t.Fatal("no light weighted pressure plate")
	return block.PressurePlate{}
}

// eventSoundCases returns the sounds the sites of the game play of events:
// each site once, those of buttons and the trail of a creaking heart in each
// of their variants.
func eventSoundCases(t testing.TB) []eventSoundCase {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	trader, creaking := eventSoundSources.trader, eventSoundSources.creaking

	stoneButton := func(pressed bool) block.Button {
		return block.Button{Type: block.StoneButton(), Facing: cube.FaceUp, Pressed: pressed}
	}
	crafter := block.NewCrafter()
	crafter.Triggered = true
	dispenser := block.NewDispenser()
	// creakingHeartTrail is the trail sound of creaking_heart.go when the
	// emitter counts emitter.
	creakingHeartTrail := func(emitter int) eventSoundCase {
		progress := float32(.2) + float32(.8)*float32(100-emitter)/100
		return eventSoundCase{fmt.Sprintf("block/creaking_heart.go:253 emitter %d", emitter), sound.CreakingHeartTrail{Progress: float64(progress)}}
	}

	return []eventSoundCase{
		{"item/bucket.go:301", sound.Event{Name: "bucket.empty.water"}},
		{"item/bucket.go:316", sound.Event{Name: "bucket.fill.lava"}},
		{"item/spawn_egg.go:162", sound.Event{Name: "ambient", Entity: "minecraft:villager"}},
		{"entity/sniffer.go:587", sound.Event{Name: "plop", Entity: "minecraft:sniffer"}},
		{"entity/shear_equipment.go:19", sound.Event{Name: "unsaddle", Entity: "minecraft:pig"}},
		{"entity/mount.go:411", sound.Event{Name: "mount", Entity: "minecraft:horse"}},
		{"entity/mount.go:617", sound.Event{Name: "saddle", Entity: "minecraft:donkey"}},
		{"entity/mount.go:629", sound.Event{Name: "armor", Entity: "minecraft:horse"}},
		{"entity/frog.go:508", sound.Event{Name: "lay_spawn", Entity: "minecraft:frog"}},
		{"entity/happy_ghast.go:578", sound.Event{Name: "attach", Entity: "minecraft:happy_ghast"}},
		{"entity/happy_ghast.go:612", sound.Event{Name: "detach", Entity: "minecraft:happy_ghast"}},
		{"entity/horse_ai.go:108", sound.Event{Name: "mad", Entity: "minecraft:horse"}},
		{"entity/creaking.go:811", sound.Event{Name: "activate", Entity: "minecraft:creaking", Source: creaking}},
		{"entity/wandering_trader.go:317", sound.Event{Name: "disappeared", Entity: "minecraft:wandering_trader", Source: trader}},
		{"entity/villager_ai.go:1956", sound.Event{Name: "celebrate", Entity: "minecraft:villager"}},
		{"entity/chicken.go:145", sound.Event{Name: "plop", Entity: "minecraft:chicken"}},
		{"entity/llama_ai.go:102", sound.Event{Name: "spit", Entity: "minecraft:llama"}},
		{"entity/effect_triggers.go:155", sound.Event{Name: "hurt", Entity: "minecraft:silverfish"}},
		{"entity/mount_ai.go:282", sound.Event{Name: "ambient", Entity: "minecraft:mule"}},
		{"entity/warden.go:149", sound.Event{Name: "agitated", Entity: "minecraft:warden"}},
		{"entity/warden.go:298", sound.Event{Name: "listening_angry", Entity: "minecraft:warden"}},
		{"entity/warden.go:347", sound.Event{Name: "attack", Entity: "minecraft:warden"}},
		{"entity/warden.go:546", sound.Event{Name: "agitated", Entity: "minecraft:warden"}},
		{"entity/warden.go:644", sound.Event{Name: "sonic_charge", Entity: "minecraft:warden"}},
		{"entity/warden.go:666", sound.Event{Name: "sonic_boom", Entity: "minecraft:warden"}},
		{"entity/hostile_mobs.go:857", sound.Event{Name: "convert_to_stray"}},
		{"entity/hostile_mobs.go:900", sound.Event{Name: "mob.husk.convert_to_zombie"}},
		{"entity/hostile_mobs.go:904", sound.Event{Name: "convert_to_drowned"}},
		{"block/redstone_switch_sound.go:20 on", sound.Event{Name: "button.click_on", Block: stoneButton(true)}},
		{"block/redstone_switch_sound.go:20 off", sound.Event{Name: "button.click_off", Block: stoneButton(false)}},
		{"block/redstone_switch_sound.go:32", sound.Event{Name: "pressure_plate.click_on", Block: lightWeightedPressurePlate(t, 3)}},
		{"block/redstone_tripwire_sound.go:34", sound.Event{Name: "attach", Block: block.Tripwire{Attached: true}}},
		{"block/crafter.go:261", sound.Event{Name: "crafter.craft", Block: crafter}},
		{"block/item_ejection_native.go:88", sound.Event{Name: "block.click", Block: dispenser}},
		{"block/cauldron.go:178", sound.Event{Name: "bucket.fill.powder_snow"}},
		{"block/cauldron.go:210", sound.Event{Name: "bucket.empty.powder_snow"}},
		{"block/bee_hive.go:564", sound.Event{Name: "block.beehive.exit"}},
		{"block/straw_bed.go:145", sound.Event{Name: "straw_bed.break_leave"}},
		creakingHeartTrail(100),
		creakingHeartTrail(40),
		creakingHeartTrail(0),
		{"block/creaking_heart.go:283", sound.Event{Name: "creaking_heart_spawn", Entity: "minecraft:creaking", Source: creaking}},
		{"block/creaking_heart.go:284", sound.Event{Name: "creaking_heart_spawn"}},
		{"block/sculk_spreader.go:544", sound.Event{Name: "block.sculk.spread"}},
		{"block/sculk_catalyst.go:85", sound.Event{Name: "block.sculk_catalyst.bloom"}},
	}
}

// TestEventSoundPackets compares the packets the session plays the sounds
// of eventSoundCases with with testdata/golden/sounds.snapshot, recorded
// when the game passed the fields of LevelSoundEvent itself, as
// sound.NativeEvent, rather than a sound.Event. No golden scenario plays
// them.
func TestEventSoundPackets(t *testing.T) {
	var b strings.Builder
	for _, tc := range eventSoundCases(t) {
		fmt.Fprintf(&b, "== %s\n", tc.site)
		for _, model := range session.SoundPackets(eventSoundPos, tc.sound) {
			for _, pk := range toWire(model) {
				raw := encode(pk)
				fmt.Fprintf(&b, "%s id=%d len=%d\n", packetName(pk), pk.ID(), len(raw))
				writeValue(&b, "    ", "", reflect.ValueOf(pk).Elem())
				for off := 0; off < len(raw); off += 32 {
					fmt.Fprintf(&b, "    | %x\n", raw[off:min(off+32, len(raw))])
				}
			}
		}
	}
	path := filepath.Join("testdata", "golden", "sounds.snapshot")
	got := b.String()
	if *updateSnapshots {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run with -update-snapshots to write it", err)
	}
	if got != string(want) {
		t.Errorf("the sounds of events play differently from %s (run with -update-snapshots if the change is meant):\n%s", path, snapshotDiff(string(want), got))
	}
}
