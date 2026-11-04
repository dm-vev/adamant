package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/dm-vev/adamant/net/nethernet"
)

// fakeServer serves one client what BDS sends a client of protocol 2193:
// an item registry and block definitions in its spawn, then biomes,
// entity identifiers, the creative inventory and recipes, and a message.
func fakeServer(t *testing.T) string {
	t.Helper()
	l, err := netconn.Config{AuthenticationDisabled: true}.Listen(nethernet.ListenConfig{}, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var ids bytes.Buffer
	if err := nbt.NewEncoderWithEncoding(&ids, nbt.NetworkLittleEndian).Encode(map[string]any{
		"idlist": []any{map[string]any{"id": "minecraft:pig"}, map[string]any{"id": "minecraft:zombie"}},
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err = c.StartGameContext(ctx, netconn.GameData{
			WorldName: "probe", EntityUniqueID: 1, EntityRuntimeID: 1, BaseGameVersion: "1.26.50",
			Items:  []mc.ItemEntry{{Name: "minecraft:stick", RuntimeID: 1}, {Name: "minecraft:planks", RuntimeID: 2}},
			Blocks: []mc.BlockEntry{{Name: "adamant:probe", Properties: map[string]any{"menu_category": map[string]any{"category": "none"}}}},
		})
		if err != nil {
			t.Errorf("start game: %v", err)
			return
		}
		for _, pk := range []proto.Packet{
			&mc.BiomeDefinitionListPacket{BiomeDefinitions: []mc.BiomeDefinition{{NameIndex: 0, BiomeID: 1, Temperature: 0.8}}, StringList: []string{"minecraft:plains"}},
			&mc.AvailableActorIdentifiersPacket{SerialisedEntityIdentifiers: ids.Bytes()},
			&mc.CreativeContentPacket{
				Groups: []mc.CreativeGroup{{Category: 1, Name: "itemGroup.name.planks"}},
				Items:  []mc.CreativeItem{{CreativeItemNetworkID: 1, Item: mc.ItemStack{NetworkID: 2, Count: 1}}},
			},
			&mc.CraftingDataPacket{ShapedRecipes: []mc.ShapedRecipe{{
				RecipeID: "minecraft:stick", Width: 1, Height: 2, Block: "crafting_table", RecipeNetworkID: 1,
				Input:  []mc.ItemDescriptorCount{{Descriptor: &mc.ItemTagItemDescriptor{Tag: "minecraft:planks"}, Count: 1}, {Descriptor: &mc.DefaultItemDescriptor{Name: "minecraft:planks"}, Count: 1}},
				Output: []mc.ItemStack{{NetworkID: 1, Count: 4}},
			}}},
			&mc.TextPacket{TextType: mc.TextTypeRaw, Message: "probe done"},
		} {
			_ = c.WritePacket(pk)
		}
		_ = c.Flush()
	}()
	return l.Addr().String()
}

// TestProbeFakeServer joins a server of the test as a client of 2193 and
// writes every dump it takes.
func TestProbeFakeServer(t *testing.T) {
	dir := t.TempDir()
	path := func(name string) string { return filepath.Join(dir, name) }
	var out bytes.Buffer
	err := run(context.Background(), []string{"-address", fakeServer(t), "-protocol", "2193", "-timeout", "30s",
		"-items-nbt", path("items.nbt"), "-blocks-nbt", path("blocks.nbt"), "-entities", path("entities.json"),
		"-creative", path("creative.json"), "-crafting", path("crafting.json"), "-wait-message", "probe done"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(out.Bytes(), &snap); err != nil || snap.ProtocolVersion != 2193 || snap.LibraryVersion != "1.26.50" ||
		len(snap.Items) != 2 || len(snap.Biomes) != 1 || snap.Biomes[0].Identifier != "minecraft:plains" {
		t.Fatalf("snapshot %+v: %v", snap, err)
	}
	read := func(name string, v any) {
		t.Helper()
		b, err := os.ReadFile(path(name))
		if err != nil {
			t.Fatal(err)
		}
		if v != nil {
			if err := json.Unmarshal(b, v); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
	read("items.nbt", nil)
	read("blocks.nbt", nil)
	var entities []string
	read("entities.json", &entities)
	if strings.Join(entities, " ") != "minecraft:pig minecraft:zombie" {
		t.Errorf("entities %q", entities)
	}
	var creative []creativeItem
	read("creative.json", &creative)
	if len(creative) != 1 || creative[0].Item != "minecraft:planks" || creative[0].Name != "itemGroup.name.planks" || creative[0].Category != 1 {
		t.Errorf("creative %+v", creative)
	}
	var recipes []recipe
	read("crafting.json", &recipes)
	if len(recipes) != 1 || recipes[0].Kind != "shaped 1x2" || strings.Join(recipes[0].Input, ",") != "1 #minecraft:planks,1 minecraft:planks:0" ||
		strings.Join(recipes[0].Output, ",") != "4 minecraft:stick:0" {
		t.Errorf("recipes %+v", recipes)
	}
}

// TestProbeRefusesUnknownProtocols checks -protocol names one the codecs
// know.
func TestProbeRefusesUnknownProtocols(t *testing.T) {
	for _, n := range []string{"2192", "2250", "x"} {
		if _, err := parseOptions([]string{"-address", "127.0.0.1:1", "-protocol", n}); err == nil {
			t.Errorf("-protocol %s is taken", n)
		}
	}
	if o, err := parseOptions([]string{"-address", "127.0.0.1:1", "-protocol", "2225"}); err != nil || o.protocol.Number != 2225 {
		t.Errorf("-protocol 2225: %v", err)
	}
}
