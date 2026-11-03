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

// eventSoundCase is a sound of an event as a site of the game plays it.
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

// eventBlockHash returns the network hash of a block, as the sites of the
// game passed it.
func eventBlockHash(b world.Block) int32 {
	br := world.DefaultBlockRegistry
	hash, _ := br.RuntimeIDToHash(br.BlockRuntimeID(b))
	return int32(hash)
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
		return eventSoundCase{fmt.Sprintf("block/creaking_heart.go:253 emitter %d", emitter), sound.NativeEvent{SoundType: "block.creaking_heart.trail", ExtraData: int32((progress*2 - 1) * 1000)}}
	}

	return []eventSoundCase{
		{"item/bucket.go:301", sound.NativeEvent{SoundType: "bucket.empty.water", ExtraData: -1}},
		{"item/bucket.go:316", sound.NativeEvent{SoundType: "bucket.fill.lava", ExtraData: -1}},
		{"item/spawn_egg.go:162", sound.NativeEvent{SoundType: "ambient", EntityType: world.BedrockEntityIdentifier("minecraft:villager"), ExtraData: -1}},
		{"entity/sniffer.go:587", sound.NativeEvent{SoundType: "plop", EntityType: "minecraft:sniffer", ExtraData: -1}},
		{"entity/shear_equipment.go:19", sound.NativeEvent{SoundType: "unsaddle", EntityType: "minecraft:pig", ExtraData: -1}},
		{"entity/mount.go:411", sound.NativeEvent{SoundType: "mount", EntityType: "minecraft:horse", ExtraData: -1}},
		{"entity/mount.go:617", sound.NativeEvent{SoundType: "saddle", EntityType: "minecraft:donkey", ExtraData: -1}},
		{"entity/mount.go:629", sound.NativeEvent{SoundType: "armor", EntityType: "minecraft:horse", ExtraData: -1}},
		{"entity/frog.go:508", sound.NativeEvent{SoundType: "lay_spawn", EntityType: "minecraft:frog", ExtraData: -1}},
		{"entity/happy_ghast.go:578", sound.NativeEvent{SoundType: "attach", EntityType: "minecraft:happy_ghast", ExtraData: -1}},
		{"entity/happy_ghast.go:612", sound.NativeEvent{SoundType: "detach", EntityType: "minecraft:happy_ghast", ExtraData: -1}},
		{"entity/horse_ai.go:108", sound.NativeEvent{SoundType: "mad", EntityType: "minecraft:horse", ExtraData: -1}},
		{"entity/creaking.go:811", sound.NativeEvent{SoundType: "activate", EntityType: "minecraft:creaking", ExtraData: -1, EntityUniqueID: creaking.UniqueID()}},
		{"entity/wandering_trader.go:317", sound.NativeEvent{SoundType: "disappeared", EntityType: "minecraft:wandering_trader", EntityUniqueID: trader.UniqueID(), ExtraData: -1}},
		{"entity/villager_ai.go:1956", sound.NativeEvent{SoundType: "celebrate", EntityType: "minecraft:villager_v2", ExtraData: -1}},
		{"entity/chicken.go:145", sound.NativeEvent{SoundType: "plop", EntityType: "minecraft:chicken", ExtraData: -1}},
		{"entity/llama_ai.go:102", sound.NativeEvent{SoundType: "spit", EntityType: "minecraft:llama", ExtraData: -1}},
		{"entity/effect_triggers.go:155", sound.NativeEvent{SoundType: "hurt", EntityType: "minecraft:silverfish", ExtraData: -1}},
		{"entity/mount_ai.go:282", sound.NativeEvent{SoundType: "ambient", EntityType: "minecraft:mule", ExtraData: -1}},
		{"entity/warden.go:149", sound.NativeEvent{SoundType: "agitated", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/warden.go:298", sound.NativeEvent{SoundType: "listening_angry", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/warden.go:347", sound.NativeEvent{SoundType: "attack", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/warden.go:546", sound.NativeEvent{SoundType: "agitated", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/warden.go:644", sound.NativeEvent{SoundType: "sonic_charge", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/warden.go:666", sound.NativeEvent{SoundType: "sonic_boom", EntityType: "minecraft:warden", ExtraData: -1}},
		{"entity/hostile_mobs.go:857", sound.NativeEvent{SoundType: "convert_to_stray", ExtraData: -1}},
		{"entity/hostile_mobs.go:900", sound.NativeEvent{SoundType: "mob.husk.convert_to_zombie", ExtraData: -1}},
		{"entity/hostile_mobs.go:904", sound.NativeEvent{SoundType: "convert_to_drowned", ExtraData: -1}},
		{"block/redstone_switch_sound.go:20 on", sound.NativeEvent{SoundType: "button.click_on", ExtraData: eventBlockHash(stoneButton(true))}},
		{"block/redstone_switch_sound.go:20 off", sound.NativeEvent{SoundType: "button.click_off", ExtraData: eventBlockHash(stoneButton(false))}},
		{"block/redstone_switch_sound.go:32", sound.NativeEvent{SoundType: "pressure_plate.click_on", ExtraData: eventBlockHash(lightWeightedPressurePlate(t, 3))}},
		{"block/redstone_tripwire_sound.go:34", sound.NativeEvent{SoundType: "attach", ExtraData: eventBlockHash(block.Tripwire{Attached: true})}},
		{"block/crafter.go:261", sound.NativeEvent{SoundType: "crafter.craft", ExtraData: eventBlockHash(crafter), EntityUniqueID: -1}},
		{"block/item_ejection_native.go:88", sound.NativeEvent{SoundType: "block.click", ExtraData: eventBlockHash(dispenser), EntityUniqueID: -1}},
		{"block/cauldron.go:178", sound.NativeEvent{SoundType: "bucket.fill.powder_snow", ExtraData: -1}},
		{"block/cauldron.go:210", sound.NativeEvent{SoundType: "bucket.empty.powder_snow", ExtraData: -1}},
		{"block/bee_hive.go:564", sound.NativeEvent{SoundType: "block.beehive.exit", ExtraData: -1}},
		{"block/straw_bed.go:145", sound.NativeEvent{SoundType: "straw_bed.break_leave", EntityType: ":", ExtraData: -1}},
		creakingHeartTrail(100),
		creakingHeartTrail(40),
		creakingHeartTrail(0),
		{"block/creaking_heart.go:283", sound.NativeEvent{SoundType: "creaking_heart_spawn", EntityType: "minecraft:creaking", EntityUniqueID: creaking.UniqueID(), ExtraData: -1}},
		{"block/creaking_heart.go:284", sound.NativeEvent{SoundType: "creaking_heart_spawn", ExtraData: -1}},
		{"block/sculk_spreader.go:544", sound.NativeEvent{SoundType: "block.sculk.spread", ExtraData: -1}},
		{"block/sculk_catalyst.go:85", sound.NativeEvent{SoundType: "block.sculk_catalyst.bloom", ExtraData: -1}},
	}
}

// TestEventSoundPackets compares the packets the session plays the sounds
// of eventSoundCases with with testdata/golden/sounds.snapshot, recorded
// when the game passed the fields of LevelSoundEvent itself. No golden
// scenario plays them.
func TestEventSoundPackets(t *testing.T) {
	var b strings.Builder
	for _, tc := range eventSoundCases(t) {
		fmt.Fprintf(&b, "== %s\n", tc.site)
		for _, pk := range session.SoundPackets(eventSoundPos, tc.sound) {
			raw := encode(pk)
			fmt.Fprintf(&b, "%s id=%d len=%d\n", packetName(pk), pk.ID(), len(raw))
			writeValue(&b, "    ", "", reflect.ValueOf(pk).Elem())
			for off := 0; off < len(raw); off += 32 {
				fmt.Fprintf(&b, "    | %x\n", raw[off:min(off+32, len(raw))])
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
