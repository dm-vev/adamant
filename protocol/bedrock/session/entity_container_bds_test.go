package session_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// bdsEntityContainers is the capture of testdata/bedrock_1_26_52_entity_containers.json:
// what BDS 1.26.52.3 sent a client that opened and closed the inventories
// of mounts, chest boats and container minecarts.
type bdsEntityContainers struct {
	Version   string `json:"version"`
	Build     int    `json:"build"`
	Scenarios []struct {
		Name      string `json:"name"`
		Entity    string `json:"entity"`
		ActorData *struct {
			ContainerType    *uint8 `json:"container_type"`
			ContainerSize    *int32 `json:"container_size"`
			StrengthModifier *int32 `json:"strength_modifier"`
			Types            string `json:"types"`
		} `json:"actor_data"`
		Open  []bdsPacket `json:"open"`
		Close []bdsPacket `json:"close"`
	} `json:"scenarios"`
}

type bdsPacket struct {
	Type   string   `json:"type"`
	SHA256 string   `json:"sha256"`
	Raw    string   `json:"raw"`
	Window *uint32  `json:"window"`
	Size   *int     `json:"size"`
	Items  []string `json:"items"`
}

// decode decodes the packet of a capture, whose hash it checks first.
func (p bdsPacket) decode(t *testing.T) proto.Packet {
	t.Helper()
	raw, err := hex.DecodeString(p.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != p.SHA256 {
		t.Fatalf("captured %s does not hash to %s", p.Type, p.SHA256)
	}
	h, body, err := bedrock.ReadHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	return decodeAs(t, h.ID, body)
}

func loadBDSEntityContainers(t *testing.T) map[string]int {
	t.Helper()
	b, err := os.ReadFile("testdata/bedrock_1_26_52_entity_containers.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &bdsContainers); err != nil {
		t.Fatal(err)
	}
	if bdsContainers.Version != "1.26.52.3" || bdsContainers.Build != 51798919 {
		t.Fatalf("capture of BDS %s build %d", bdsContainers.Version, bdsContainers.Build)
	}
	index := map[string]int{}
	for i, sc := range bdsContainers.Scenarios {
		index[sc.Name] = i
	}
	return index
}

var bdsContainers bdsEntityContainers

// TestEntityContainersAgainstBDS opens the inventories of mounts, chest
// boats and container minecarts as a sneaking client clicking them with an
// empty hand, as the capture of BDS did, and compares what the client is
// sent: the packet that opens the window and its type, the window of the
// contents, the contents where Adamant lays them out as BDS does, and the
// container data of the entity.
func TestEntityContainersAgainstBDS(t *testing.T) {
	index := loadBDSEntityContainers(t)
	s := newScenario(t, "entity containers", scenarioConfig{})

	diamonds := func(n int) map[int]item.Stack { return map[int]item.Stack{0: item.NewStack(item.Diamond{}, n)} }
	cases := []struct {
		capture string
		nbt     map[string]any
		spawn   func(opts world.EntitySpawnOpts) *world.EntityHandle
		items   map[int]item.Stack
		// exactEquip compares the inventory data of UpdateEquip byte for
		// byte: BDS writes it differently once the mount was equipped.
		exactEquip bool
		// contents compares the contents of the window.
		contents bool
	}{
		{capture: "horse_empty", nbt: map[string]any{"id": "minecraft:horse", "Tame": byte(1)}, exactEquip: true, contents: true},
		{capture: "horse_saddle_armor", nbt: map[string]any{"id": "minecraft:horse", "Tame": byte(1), "Saddled": byte(1)}, exactEquip: true},
		{capture: "horse_container_slot1_armor", nbt: map[string]any{"id": "minecraft:horse", "Tame": byte(1)}},
		{capture: "donkey_nochest", nbt: map[string]any{"id": "minecraft:donkey", "Tame": byte(1), "Saddled": byte(1)}, exactEquip: true},
		{capture: "donkey_chest", nbt: map[string]any{"id": "minecraft:donkey", "Tame": byte(1), "Saddled": byte(1), "ChestedHorse": byte(1)}, exactEquip: true},
		{capture: "mule_chest", nbt: map[string]any{"id": "minecraft:mule", "Tame": byte(1), "ChestedHorse": byte(1)}, exactEquip: true},
		{capture: "llama_nochest", nbt: map[string]any{"id": "minecraft:llama", "Tame": byte(1)}, exactEquip: true},
		{capture: "llama_chest", nbt: map[string]any{"id": "minecraft:llama", "Tame": byte(1), "ChestedHorse": byte(1), "Strength": int32(3)}},
		{capture: "trader_llama_chest", nbt: map[string]any{"id": "minecraft:trader_llama", "Tame": byte(1), "ChestedHorse": byte(1), "Strength": int32(2)}, exactEquip: true},
		{capture: "camel", nbt: map[string]any{"id": "minecraft:camel", "Saddled": byte(1)}, exactEquip: true},
		{capture: "camel_husk", nbt: map[string]any{"id": "minecraft:camel_husk"}, exactEquip: true},
		{capture: "chest_boat", spawn: func(opts world.EntitySpawnOpts) *world.EntityHandle { return entity.NewChestBoat(opts, 0) }, items: diamonds(4), contents: true},
		{capture: "chest_minecart", nbt: map[string]any{"id": "minecraft:chest_minecart"}, items: diamonds(5), contents: true},
		{capture: "hopper_minecart", nbt: map[string]any{"id": "minecraft:hopper_minecart"}, items: diamonds(6), contents: true},
	}
	for _, c := range cases {
		i, ok := index[c.capture]
		if !ok {
			t.Fatalf("no capture %s", c.capture)
		}
		want := bdsContainers.Scenarios[i]

		// The entity is spawned two blocks from the player, as in the
		// capture, and the client is sent it.
		var h *world.EntityHandle
		var pos mgl64.Vec3
		spawned := s.gather(func() {
			s.run(func(tx *world.Tx, p *player.Player) {
				pos = p.Position().Add(mgl64.Vec3{2, 0, 0})
				opts := world.EntitySpawnOpts{Position: pos, SpawnFinalized: true}
				if c.spawn != nil {
					h = c.spawn(opts)
				} else {
					var err error
					if h, err = entity.NewFromStructureNBT(c.nbt, opts); err != nil {
						t.Fatal(err)
					}
				}
				e := tx.AddEntity(h)
				if len(c.items) > 0 {
					inv := entityInventory(t, e)
					for slot, it := range c.items {
						if err := inv.SetItem(slot, it); err != nil {
							t.Fatal(err)
						}
					}
				}
				p.StartSneaking()
			})
		})
		if want.ActorData != nil {
			checkContainerData(t, c.capture, spawned, h.RuntimeID(), want.ActorData.ContainerType, want.ActorData.ContainerSize, want.ActorData.StrengthModifier, want.ActorData.Types)
		}

		// The client clicks the entity.
		got := s.gather(func() {
			s.send(&mc.InventoryTransactionPacket{TransactionData: &mc.UseItemOnEntityTransactionData{
				TargetEntityRuntimeID: h.RuntimeID(),
				ActionType:            mc.UseItemOnEntityActionInteract,
				HotBarSlot:            emptyHand,
				Position:              mgl32.Vec3{float32(pos[0] - 2), float32(pos[1]), float32(pos[2])},
				ClickedPosition:       mgl32.Vec3{0, 0.8, 0},
			}})
		})
		window, windowType := checkOpen(t, c.capture, got, want.Open, h, pos, c.exactEquip)
		if c.contents {
			checkContents(t, c.capture, got, window, want.Open)
		}

		// The client closes the window; BDS answers with the window it
		// closed.
		closed := s.gather(func() {
			s.send(&mc.ContainerClosePacket{WindowID: window, ContainerType: windowType})
		})
		var echo *mc.ContainerClosePacket
		for _, pk := range closed {
			if cl, ok := pk.(*mc.ContainerClosePacket); ok {
				echo = cl
			}
		}
		if echo == nil || echo.WindowID != window || echo.ServerSide {
			t.Errorf("%s: closed with %+v, want window %d closed for the client", c.capture, echo, window)
		}

		s.run(func(tx *world.Tx, p *player.Player) {
			p.StopSneaking()
			if e, ok := h.Entity(tx); ok {
				tx.RemoveEntity(e)
			}
		})
		s.gather(func() {})
	}
}

// gather runs act, advances the world a tick and returns what the client was
// sent meanwhile, as it decodes it.
func (s *scenario) gather(act func()) []proto.Packet {
	s.t.Helper()
	act()
	s.advance()
	s.markers++
	marker := fmt.Sprintf("golden marker %d", s.markers)
	s.run(func(_ *world.Tx, p *player.Player) { p.Message(marker) })
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var out []proto.Packet
	for {
		select {
		case pk := <-s.conn.written:
			if text, ok := pk.(*mc.TextPacket); ok && text.Message == marker {
				return out
			}
			out = append(out, decodeAs(s.t, pk.ID(), encode(pk)))
		case <-timer.C:
			s.t.Fatalf("%s: the session did not send its marker", s.name)
		}
	}
}

func entityInventory(t *testing.T, e world.Entity) *inventory.Inventory {
	t.Helper()
	if c, ok := e.(interface{ Inventory() *inventory.Inventory }); ok {
		return c.Inventory()
	}
	if b, ok := e.(*entity.ChestBoat); ok {
		return b.Behaviour().(*entity.ChestBoatBehaviour).Inventory()
	}
	t.Fatalf("%T has no inventory", e)
	return nil
}

// checkContainerData compares the container type, size and strength
// modifier of the entity the client was sent, and their types, with BDS's.
func checkContainerData(t *testing.T, name string, pks []proto.Packet, rid uint64, typ *uint8, size, strength *int32, types string) {
	t.Helper()
	var meta mc.Metadata
	for _, pk := range pks {
		if add, ok := pk.(*mc.AddActorPacket); ok && add.EntityRuntimeID == rid {
			meta = add.EntityMetadata
		}
	}
	if meta == nil {
		t.Errorf("%s: the client was not sent the entity", name)
		return
	}
	var gotTypes []string
	for _, k := range []uint32{mc.EntityDataKeyContainerType, mc.EntityDataKeyContainerSize, mc.EntityDataKeyContainerStrengthModifier} {
		if v, ok := meta[k]; ok {
			gotTypes = append(gotTypes, fmt.Sprintf("%d:%T", k, v))
		}
	}
	sort.Strings(gotTypes)
	if strings.Join(gotTypes, " ") != types {
		t.Errorf("%s: container data of types %v, BDS sends %s", name, gotTypes, types)
	}
	if typ != nil && meta[mc.EntityDataKeyContainerType] != *typ {
		t.Errorf("%s: container type %v in the actor data, BDS sends %d", name, meta[mc.EntityDataKeyContainerType], *typ)
	}
	if size != nil && meta[mc.EntityDataKeyContainerSize] != *size {
		t.Errorf("%s: container size %v in the actor data, BDS sends %d", name, meta[mc.EntityDataKeyContainerSize], *size)
	}
	if strength != nil && meta[mc.EntityDataKeyContainerStrengthModifier] != *strength {
		t.Errorf("%s: strength modifier %v in the actor data, BDS sends %d", name, meta[mc.EntityDataKeyContainerStrengthModifier], *strength)
	}
}

// checkOpen compares the packet that opened the window with BDS's and
// returns the window and its type.
func checkOpen(t *testing.T, name string, got []proto.Packet, want []bdsPacket, h *world.EntityHandle, pos mgl64.Vec3, exactEquip bool) (byte, byte) {
	t.Helper()
	var opener proto.Packet
	for _, pk := range got {
		switch pk.(type) {
		case *mc.ContainerOpenPacket, *mc.UpdateEquipPacket:
			if opener != nil {
				t.Errorf("%s: two windows opened: %T and %T", name, opener, pk)
			}
			opener = pk
		}
	}
	var bds proto.Packet
	for _, p := range want {
		if p.Type == "ContainerOpen" || p.Type == "UpdateEquip" {
			bds = p.decode(t)
		}
	}
	if reflect.TypeOf(opener) != reflect.TypeOf(bds) {
		t.Errorf("%s: opened with %T, BDS opens with %T", name, opener, bds)
		return 0, 0
	}
	switch o := opener.(type) {
	case *mc.UpdateEquipPacket:
		b := bds.(*mc.UpdateEquipPacket)
		if o.WindowType != b.WindowType || o.Size != b.Size || o.EntityUniqueID != h.UniqueID() {
			t.Errorf("%s: UpdateEquip of window type %d, size %d, entity %d; BDS: type %d, size %d, entity %d", name,
				o.WindowType, o.Size, o.EntityUniqueID, b.WindowType, b.Size, h.UniqueID())
		}
		if exactEquip && !bytes.Equal(o.SerialisedInventoryData, b.SerialisedInventoryData) {
			t.Errorf("%s: equip slots\n%x\nBDS sends\n%x", name, o.SerialisedInventoryData, b.SerialisedInventoryData)
		}
		if g, w := equipSlots(t, o.SerialisedInventoryData), equipSlots(t, b.SerialisedInventoryData); !reflect.DeepEqual(g, w) {
			t.Errorf("%s: equip slots %v, BDS: %v", name, g, w)
		}
		return o.WindowID, o.WindowType
	case *mc.ContainerOpenPacket:
		b := bds.(*mc.ContainerOpenPacket)
		block := [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])}
		if o.ContainerType != b.ContainerType || o.ContainerEntityUniqueID != h.UniqueID() || o.ContainerPosition != block {
			t.Errorf("%s: ContainerOpen of type %d at %v, entity %d; BDS: type %d at the entity's block %v, entity %d", name,
				o.ContainerType, o.ContainerPosition, o.ContainerEntityUniqueID, b.ContainerType, block, h.UniqueID())
		}
		return o.WindowID, o.ContainerType
	}
	return 0, 0
}

// equipSlots describes the slots of serialised equip data, each as its
// number, the item drawn in it and the items it takes, under their current
// names: BDS names them by their legacy names until a mount was equipped,
// and by their current ones after, and leaves out an Aux of 32767 at times.
func equipSlots(t *testing.T, data []byte) []string {
	t.Helper()
	var v struct {
		Slots []struct {
			AcceptedItems []struct {
				SlotItem map[string]any `nbt:"slotItem"`
			} `nbt:"acceptedItems"`
			Item       map[string]any `nbt:"item"`
			SlotNumber int32          `nbt:"slotNumber"`
		} `nbt:"slots"`
	}
	if err := nbt.UnmarshalEncoding(data, &v, nbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	current := map[string]string{
		"minecraft:horsearmorleather": "minecraft:leather_horse_armor", "minecraft:horsearmoriron": "minecraft:iron_horse_armor",
		"minecraft:horsearmorgold": "minecraft:golden_horse_armor", "minecraft:horsearmordiamond": "minecraft:diamond_horse_armor",
	}
	name := func(m map[string]any) string {
		n, _ := m["Name"].(string)
		if c, ok := current[n]; ok {
			n = c
		}
		if aux, ok := m["Aux"]; ok && aux != int16(32767) {
			n += fmt.Sprintf(":%v", aux)
		}
		return n
	}
	var out []string
	for _, s := range v.Slots {
		var accepted []string
		for _, a := range s.AcceptedItems {
			accepted = append(accepted, name(a.SlotItem))
		}
		sort.Strings(accepted)
		out = append(out, fmt.Sprintf("%d %s %v", s.SlotNumber, name(s.Item), accepted))
	}
	return out
}

// checkContents compares the first contents of the window the client was
// sent with BDS's.
func checkContents(t *testing.T, name string, got []proto.Packet, window byte, want []bdsPacket) {
	t.Helper()
	var bds *bdsPacket
	for i := range want {
		if want[i].Type == "InventoryContent" {
			bds = &want[i]
			break
		}
	}
	for _, pk := range got {
		c, ok := pk.(*mc.InventoryContentPacket)
		if !ok || c.WindowID != uint32(window) {
			continue
		}
		var items []string
		for i, it := range c.Content {
			if it.Stack.NetworkID == 0 {
				continue
			}
			stack, ok := bedrockdata.ItemByRuntimeID(it.Stack.NetworkID, int16(it.Stack.MetadataValue))
			if !ok {
				t.Fatalf("%s: unknown item %d", name, it.Stack.NetworkID)
			}
			id, _ := stack.EncodeItem()
			items = append(items, fmt.Sprintf("%d:%s*%d", i, id, it.Stack.Count))
		}
		if len(c.Content) != *bds.Size || !reflect.DeepEqual(items, bds.Items) {
			t.Errorf("%s: window of %d slots holding %v, BDS: %d slots holding %v", name, len(c.Content), items, *bds.Size, bds.Items)
		}
		return
	}
	t.Errorf("%s: the client was not sent the contents of window %d", name, window)
}
