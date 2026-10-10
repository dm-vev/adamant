package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

type minecartLootSlot struct {
	Slot         int            `json:"slot"`
	Item         string         `json:"item"`
	Count        int            `json:"count"`
	Enchantments map[string]int `json:"enchantments"`
}
type minecartLootFixture struct {
	Seed   string             `json:"seed"`
	Biome  string             `json:"biome"`
	Origin [3]float64         `json:"origin"`
	Slots  []minecartLootSlot `json:"slots"`
}

type minecartLootLoadConfig struct{ data map[string]any }

func (conf minecartLootLoadConfig) Apply(data *world.EntityData) {
	data.NativeNBT = conf.data
	world.DecodeEntityNBT(ChestMinecartType, conf.data, data)
}

func minecartLootFixtures(t *testing.T) []minecartLootFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/java263_minecart_loot.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []minecartLootFixture
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func minecartLootStackSlot(slot int, stack item.Stack) minecartLootSlot {
	name, _ := stack.Item().EncodeItem()
	if name == "minecraft:golden_rail" {
		name = "minecraft:powered_rail"
	}
	enchantments := make(map[string]int)
	for _, enchantment := range stack.Enchantments() {
		enchantments["minecraft:"+strings.ToLower(strings.ReplaceAll(enchantment.Type().Name(), " ", "_"))] = enchantment.Level()
	}
	return minecartLootSlot{slot, name, stack.Count(), enchantments}
}

func minecartLootInventorySlots(cart *MinecartContainer) []minecartLootSlot {
	var slots []minecartLootSlot
	for slot, stack := range cart.Inventory().Slots() {
		if !stack.Empty() {
			slots = append(slots, minecartLootStackSlot(slot, stack))
		}
	}
	return slots
}

func spawnPendingMinecartLoot(t *testing.T, tx *world.Tx, fixture minecartLootFixture) *MinecartContainer {
	t.Helper()
	seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	position := mgl64.Vec3(fixture.Origin)
	var selected world.Biome = biome.Forest{}
	if fixture.Biome == "sulfur_caves" {
		selected = biome.SulfurCaves{}
	}
	tx.SetBiome(cube.PosFromVec3(position), selected)
	handle := world.EntitySpawnOpts{Position: position}.New(ChestMinecartType, minecartLootLoadConfig{map[string]any{"LootTable": "minecraft:chests/abandoned_mineshaft", "LootTableSeed": seed}})
	tx.AddEntity(handle)
	opened, ok := handle.Entity(tx)
	if !ok {
		t.Fatal("pending minecart handle unavailable")
	}
	return opened.(*MinecartContainer)
}

func runMinecartLootTransaction(t *testing.T, w *world.World, fn func(*world.Tx)) {
	t.Helper()
	task := w.Do(fn)
	<-task.Done()
	if err := task.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMinecartLootInventoryMatchesJava263(t *testing.T) {
	for _, fixture := range minecartLootFixtures(t) {
		t.Run(fixture.Biome+"/"+fixture.Seed, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			runMinecartLootTransaction(t, w, func(tx *world.Tx) {
				cart := spawnPendingMinecartLoot(t, tx, fixture)
				if got := minecartLootInventorySlots(cart); !reflect.DeepEqual(got, fixture.Slots) {
					t.Fatalf("materialized slots = %+v, Java %+v", got, fixture.Slots)
				}
				encoded := ChestMinecartType.EncodeNBT(cart.Ent.data)
				if encoded["LootTable"] != nil || encoded["LootTableSeed"] != nil {
					t.Fatalf("consumed loot retained pending fields: %v", encoded)
				}
				slot := fixture.Slots[0].Slot
				if err := cart.Inventory().SetItem(slot, item.Stack{}); err != nil {
					t.Fatal(err)
				}
				if stack, _ := cart.Inventory().Item(slot); !stack.Empty() {
					t.Fatalf("repeated access regenerated depleted slot %d", slot)
				}
				if err := cart.CloseIn(tx); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestMinecartPendingAndConsumedLootSurviveMCDBRestart(t *testing.T) {
	var fixture minecartLootFixture
	for _, candidate := range minecartLootFixtures(t) {
		if candidate.Biome == "forest" && candidate.Seed == "-6891946900181190969" {
			fixture = candidate
			break
		}
	}
	if fixture.Seed == "" {
		t.Fatal("source-generated mineshaft minecart loot fixture missing")
	}
	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	closeWorld := func(w *world.World) {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	find := func(tx *world.Tx) *MinecartContainer {
		t.Helper()
		tx.Block(cube.PosFromVec3(mgl64.Vec3(fixture.Origin)))
		for e := range tx.Entities() {
			if cart, ok := e.(*MinecartContainer); ok {
				return cart
			}
		}
		t.Fatal("saved minecart missing")
		return nil
	}
	w := open()
	runMinecartLootTransaction(t, w, func(tx *world.Tx) { spawnPendingMinecartLoot(t, tx, fixture) })
	w.Save()
	closeWorld(w)
	w = open()
	runMinecartLootTransaction(t, w, func(tx *world.Tx) {
		cart := find(tx)
		encoded := ChestMinecartType.EncodeNBT(cart.Ent.data)
		if encoded["LootTable"] != "minecraft:chests/abandoned_mineshaft" || fmt.Sprint(encoded["LootTableSeed"]) != fixture.Seed {
			t.Fatalf("pending loot lost on restart: %v", encoded)
		}
		if got := minecartLootInventorySlots(cart); !reflect.DeepEqual(got, fixture.Slots) {
			t.Fatalf("pending restart slots = %+v, Java %+v", got, fixture.Slots)
		}
		if err := cart.Inventory().SetItem(fixture.Slots[0].Slot, item.Stack{}); err != nil {
			t.Fatal(err)
		}
	})
	w.Save()
	closeWorld(w)
	w = open()
	defer w.Close()
	runMinecartLootTransaction(t, w, func(tx *world.Tx) {
		cart := find(tx)
		if got, want := minecartLootInventorySlots(cart), fixture.Slots[1:]; !reflect.DeepEqual(got, want) {
			t.Fatalf("consumed/depleted restart slots = %+v, want %+v", got, want)
		}
		if encoded := ChestMinecartType.EncodeNBT(cart.Ent.data); encoded["LootTable"] != nil || encoded["LootTableSeed"] != nil {
			t.Fatalf("consumed saved actor restored stale pending fields: %v", encoded)
		}
	})
}

func TestPendingMinecartLootMaterializesOnceWhenDestroyed(t *testing.T) {
	fixture := minecartLootFixtures(t)[0]
	key := func(slot minecartLootSlot) string {
		data, err := json.Marshal(struct {
			Item         string
			Enchantments map[string]int
		}{slot.Item, slot.Enchantments})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	expected := make(map[string]int)
	for _, slot := range fixture.Slots {
		expected[key(slot)] += slot.Count
	}
	expected[key(minecartLootSlot{Item: "minecraft:chest_minecart", Enchantments: map[string]int{}})]++
	w := world.Config{Entities: DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	runMinecartLootTransaction(t, w, func(tx *world.Tx) {
		cart := spawnPendingMinecartLoot(t, tx, fixture)
		if !cart.Destroy(tx, ExplosionDamageSource{}, nil) {
			t.Fatal("pending minecart destruction failed")
		}
		actual := make(map[string]int)
		for e := range tx.Entities() {
			if e.H().Type() == ItemType {
				stack := e.(*Ent).Behaviour().(*ItemBehaviour).Item()
				actual[key(minecartLootStackSlot(0, stack))] += stack.Count()
			} else if e.H().Type() == ChestMinecartType {
				t.Fatal("destroyed minecart remained in world")
			}
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("unopened destruction drops = %v, Java %v", actual, expected)
		}
	})
}
