package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// The expectations below are what BDS 1.26.52.3 saves for blocks placed with
// /setblock, or reads from earlier Adamant saves.

func TestMobSpawnerNBTMatchesBDS(t *testing.T) {
	m := MobSpawner{}.DecodeNBT(map[string]any{}).(MobSpawner).EncodeNBT()
	for key, want := range map[string]any{
		"MinSpawnDelay": int16(200), "MaxSpawnDelay": int16(800), "SpawnCount": int16(4), "SpawnRange": int16(4),
		"RequiredPlayerRange": int16(16), "MaxNearbyEntities": int16(6), "Delay": int16(20),
		"DisplayEntityWidth": float32(0.8), "DisplayEntityHeight": float32(1.8), "DisplayEntityScale": float32(1),
	} {
		if m[key] != want {
			t.Errorf("%s = %#v, want %#v", key, m[key], want)
		}
	}
	// BDS saves shorts; earlier versions ints.
	for _, width := range []any{int16(9), int32(9)} {
		decoded := MobSpawner{}.DecodeNBT(map[string]any{"SpawnCount": width}).(MobSpawner)
		if decoded.spawnCount != 9 {
			t.Errorf("SpawnCount %T decoded to %d, want 9", width, decoded.spawnCount)
		}
	}
}

func TestCauldronWithoutPotionNBT(t *testing.T) {
	for _, data := range []map[string]any{{}, {"PotionId": int16(-1), "PotionType": int16(-1)}} {
		c := Cauldron{FillLevel: 3, Liquid: CauldronWater}.DecodeNBT(data).(Cauldron)
		if c.PotionKind != CauldronNoPotion {
			t.Fatalf("cauldron decoded from %v holds a potion", data)
		}
		if m := c.EncodeNBT(); m["PotionId"] != int16(-1) || m["PotionType"] != int16(-1) {
			t.Fatalf("water cauldron saved as potion %v/%v", m["PotionId"], m["PotionType"])
		}
	}
}

func TestCommandBlockModeIsTheBlock(t *testing.T) {
	// A new chain block's last performed mode is impulse and unconditional.
	data := map[string]any{"LPCommandMode": int32(0), "LPCondionalMode": uint8(0)}
	chain := CommandBlock{Mode: CommandBlockChain, Conditional: true}.DecodeNBT(data).(CommandBlock)
	if chain.Mode != CommandBlockChain || !chain.Conditional {
		t.Fatalf("chain block decoded as mode %v conditional %v", chain.Mode, chain.Conditional)
	}
	if fresh := (CommandBlock{Mode: CommandBlockChain}).DecodeNBT(map[string]any{}).(CommandBlock); fresh.NeedsRedstone {
		t.Fatal("a new chain block needs redstone; BDS places it always active")
	}
	if fresh := (CommandBlock{Mode: CommandBlockRepeat}).DecodeNBT(map[string]any{}).(CommandBlock); !fresh.ExecuteOnFirstTick {
		t.Fatal("a new repeating block skips its first tick; BDS runs it")
	}
}

func TestBeehiveKeepsHoneyLevelState(t *testing.T) {
	// BDS keeps the honey level only in the block state.
	h := Beehive{HoneyLevel: 5}.DecodeNBT(map[string]any{"id": "Beehive"}).(Beehive)
	if h.HoneyLevel != 5 {
		t.Fatalf("honey level = %d, want the state's 5", h.HoneyLevel)
	}
	if m := h.EncodeNBT(); m["ShouldSpawnBees"] != uint8(0) || m["HoneyLevel"] != nil {
		t.Fatalf("beehive saved as %v", m)
	}
}

func TestBeaconEffectKeys(t *testing.T) {
	m := Beacon{}.EncodeNBT()
	if m["primary"] != int32(0) || m["secondary"] != int32(0) {
		t.Fatalf("beacon saved as %v, want lowercase primary and secondary", m)
	}
}

func TestShulkerBoxDefaultsMatchBDS(t *testing.T) {
	m := NewShulkerBox().DecodeNBT(map[string]any{}).(ShulkerBox).EncodeNBT()
	if m["facing"] != uint8(cube.FaceUp) || m["Findable"] != uint8(0) {
		t.Fatalf("shulker box saved as %v", m)
	}
}

func TestBrushableNBTMatchesBDS(t *testing.T) {
	m := SuspiciousGravel{}.DecodeNBT(map[string]any{}).(SuspiciousGravel).EncodeNBT()
	if m["type"] != "minecraft:suspicious_gravel" || m["brush_count"] != int32(0) || m["brush_direction"] != uint8(noBrushDirection) {
		t.Fatalf("suspicious gravel saved as %v", m)
	}
}
