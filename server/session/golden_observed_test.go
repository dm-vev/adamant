package session_test

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
)

// TestGoldenObserved records what a player is sent of what another player
// in its world does with menus and its inventory, and as it quits: Golden
// opens and closes a chest, holds an item and puts on armour by item stack
// requests, drops a stack out of the chest, closes a crafting table with
// items left in its grid and no room for them, and quits with the chest
// open; Golden then joins again, dies and quits while dead. Visitor sees it
// all. Both are sessions of the harness in one world, with the join and
// quit messages of the server; each step records what each client was sent.
func TestGoldenObserved(t *testing.T) {
	if isolated(t) {
		return
	}
	o := newObserved(t)
	g := o.join("Golden joins", nil)
	o.join("Visitor joins", &visitorIdentity)
	const inv = v2193.ContainerCombinedHotBarAndInventory

	chest := cube.Pos{2, -60, 0}
	o.step("a chest is placed with apples in it", func(tx *world.Tx) {
		tx.SetBlock(chest, block.NewChest(), nil)
		c := tx.Block(chest).(block.Container)
		if err := c.Inventory(tx, chest).SetItem(0, item.NewStack(item.Apple{}, 5)); err != nil {
			t.Fatal(err)
		}
	})
	o.client(g, "Golden opens the chest", clickPacket(chest))
	o.client(g, "Golden closes the chest", &v2193.ContainerClosePacket{WindowID: byte(g.window), ContainerType: g.windowType})

	o.step("Golden is given a sword and a helmet", func(tx *world.Tx) {
		p := g.player(tx)
		_ = p.Inventory().SetItem(0, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
		_ = p.Inventory().SetItem(1, item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
	})
	o.request(g, "Golden puts the sword in the slot it holds",
		&v2193.PlaceStackRequestAction{Count: 1, Source: g.slot(inv, 0), Destination: g.slot(inv, emptyHand)})
	o.request(g, "Golden puts the helmet on",
		&v2193.PlaceStackRequestAction{Count: 1, Source: g.slot(inv, 1), Destination: g.slot(v2193.ContainerArmor, 0)})
	o.request(g, "Golden takes the sword out of the slot it holds",
		&v2193.PlaceStackRequestAction{Count: 1, Source: g.slot(inv, emptyHand), Destination: g.slot(inv, 0)})

	o.client(g, "Golden opens the chest again", clickPacket(chest))
	o.request(g, "Golden drops the apples out of the chest",
		&v2193.DropStackRequestAction{Count: 5, Source: g.slot(v2193.ContainerLevelEntity, 0)})
	o.client(g, "Golden closes the chest", &v2193.ContainerClosePacket{WindowID: byte(g.window), ContainerType: g.windowType})
	o.tick("the tick after", 1)
	o.step("the apples taken away", removeItems)

	table := cube.Pos{-2, -60, 0}
	o.step("a crafting table is placed and Golden is given planks", func(tx *world.Tx) {
		tx.SetBlock(table, block.CraftingTable{}, nil)
		_ = g.player(tx).Inventory().SetItem(2, item.NewStack(block.Planks{Wood: block.OakWood()}, 9))
	})
	o.client(g, "Golden opens the crafting table", clickPacket(table))
	var fill []v2193.StackRequestAction
	for i := range 9 {
		source := g.slot(inv, 2)
		if i > 0 {
			source = g.again(inv, 2)
		}
		fill = append(fill, &v2193.PlaceStackRequestAction{Count: 1, Source: source, Destination: g.slot(v2193.ContainerCraftingInput, uiCraftingGrid+i)})
	}
	o.request(g, "Golden fills the grid with planks", fill...)
	o.step("the inventory of Golden is filled with stone", func(tx *world.Tx) {
		p := g.player(tx)
		for slot, it := range p.Inventory().Slots() {
			if it.Empty() {
				_ = p.Inventory().SetItem(slot, item.NewStack(block.Stone{}, 64))
			}
		}
	})
	o.client(g, "Golden closes the crafting table: the planks of the grid fall out", &v2193.ContainerClosePacket{WindowID: byte(g.window), ContainerType: g.windowType})
	o.tick("the tick after", 1)
	o.step("the planks taken away and the inventory of Golden emptied", func(tx *world.Tx) {
		removeItems(tx)
		p := g.player(tx)
		for slot, it := range p.Inventory().Slots() {
			if !it.Empty() {
				_ = p.Inventory().SetItem(slot, item.Stack{})
			}
		}
	})

	o.client(g, "Golden opens the chest once more", clickPacket(chest))
	o.quit(g, "the client of Golden disconnects with the chest open")

	g = o.join("Golden joins again", nil)
	o.step("Golden dies", func(tx *world.Tx) {
		g.player(tx).Hurt(1000, entity.VoidDamageSource{})
	})
	o.tick("the ticks after Golden died", 2)
	o.quit(g, "the client of Golden disconnects while Golden is dead")
	o.snapshot()
}

// observed records the clients of the sessions of several players of the
// harness in one world, step by step: what each was sent in a step, under
// its name.
type observed struct {
	t   *testing.T
	w   *world.World
	out strings.Builder
	// norm names the IDs of what every client is sent.
	norm *normaliser
	// roster lists the players of every client, as the roster of a server
	// does, so that the clients list each other.
	roster *client.Roster
	// clients are the clients online, in the order they joined, and names
	// their names.
	clients []*scenario
	names   map[*scenario]string
}

func newObserved(t *testing.T) *observed {
	return &observed{t: t, norm: newNormaliser(), roster: new(client.Roster), names: map[*scenario]string{}}
}

// join has a player join the world of the scenario, the first making it,
// and records what the clients online were sent meanwhile; what the client
// joining is sent is left out.
func (o *observed) join(heading string, who *serverIdentity) *scenario {
	o.t.Helper()
	name := goldenName
	if who != nil {
		name = who.name
	}
	c := newScenario(o.t, "observed", scenarioConfig{world: o.w, who: who, norm: o.norm, session: func(conf *session.Config) {
		// As the server does by default.
		conf.JoinMessage, conf.QuitMessage = chat.MessageJoin, chat.MessageQuit
		conf.Roster = o.roster
	}})
	if o.w == nil {
		o.w = c.w
	}
	o.clients = append(o.clients, c)
	o.names[c] = name
	fmt.Fprintf(&o.out, "== %s\n", heading)
	o.collect()
	return c
}

// step runs f in a transaction of the world and records what each client
// was sent under the heading.
func (o *observed) step(heading string, f func(tx *world.Tx)) {
	o.t.Helper()
	fmt.Fprintf(&o.out, "== %s\n", heading)
	o.sync()
	<-o.w.Exec(f)
	o.collect()
}

// tick advances the world n ticks, recording what each client was sent
// after each.
func (o *observed) tick(heading string, n int) {
	o.t.Helper()
	for i := 1; i <= n; i++ {
		h := heading
		if n > 1 {
			h = fmt.Sprintf("%s, tick %d", heading, i)
		}
		fmt.Fprintf(&o.out, "== %s\n", h)
		o.advance()
		o.collect()
	}
}

// client sends packets as the client c does, which its session handles in
// the next tick of the world, and records them and what each client was
// sent in that tick.
func (o *observed) client(c *scenario, heading string, pks ...proto.Packet) {
	o.t.Helper()
	fmt.Fprintf(&o.out, "== %s\n", heading)
	fmt.Fprintf(&o.out, "-- %s sends\n", o.names[c])
	for _, pk := range pks {
		raw := encode(pk)
		o.out.WriteString(c.describe(decodeAs(o.t, pk.ID(), raw), raw, "<<"))
		c.send(pk)
	}
	o.advance()
	o.collect()
}

// request sends an item stack request of the client c and fails the test
// unless the server accepted it.
func (o *observed) request(c *scenario, heading string, actions ...v2193.StackRequestAction) {
	o.t.Helper()
	id := c.nextRequest
	c.nextRequest -= 2
	before := len(c.responses)
	o.client(c, heading, &v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{RequestID: id, Actions: actions}}})
	if len(c.responses) == before {
		o.t.Errorf("%s: the request was not answered", heading)
	} else if status := c.responses[len(c.responses)-1]; status != v2193.ItemStackResponseStatusOK {
		o.t.Errorf("%s: the request was refused with status %d", heading, status)
	}
}

// quit closes the connection of the client c as a client that disconnects
// does: the session reads EOF and the next tick of the world closes it.
// What the other clients were sent until then is recorded.
func (o *observed) quit(c *scenario, heading string) {
	o.t.Helper()
	fmt.Fprintf(&o.out, "== %s\n", heading)
	_ = c.conn.Close()
	select {
	case <-c.conn.eof:
	case <-time.After(5 * time.Second):
		o.t.Fatalf("%s: the session of %s did not read the end of its connection", heading, o.names[c])
	}
	// The session marks its inbox closed right after it read the end.
	time.Sleep(timerSettle)
	o.advance()
	select {
	case <-c.stopped:
	case <-time.After(5 * time.Second):
		o.t.Fatalf("%s: the session of %s did not stop in the tick after its connection closed", heading, o.names[c])
	}
	for i, other := range o.clients {
		if other == c {
			o.clients = append(o.clients[:i:i], o.clients[i+1:]...)
			break
		}
	}
	o.collect()
}

// advance advances the world a tick.
func (o *observed) advance() {
	o.sync()
	o.w.AdvanceTick()
	o.sync()
}

// sync syncs with the timers of the session of each client, as
// scenario.sync does.
func (o *observed) sync() {
	for _, c := range o.clients {
		c.sync()
	}
}

// collect records what each client online was sent since the last step,
// under its name.
func (o *observed) collect() {
	o.t.Helper()
	for _, c := range o.clients {
		c.drain(true)
		fmt.Fprintf(&o.out, "-- %s\n", o.names[c])
		o.out.WriteString(c.out.String())
		c.out.Reset()
	}
}

// snapshot compares the record with the observed snapshot, or writes it
// with -update-snapshots.
func (o *observed) snapshot() {
	o.t.Helper()
	rec := &scenario{t: o.t, name: "observed"}
	rec.out.WriteString(o.out.String())
	rec.snapshot()
}

// clickPacket returns what a client sends when its player clicks the top of
// a block with the empty hand, as scenario.clickBlock sends it.
func clickPacket(pos cube.Pos) proto.Packet {
	return &v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemTransactionData{
		ActionType:       v2193.UseItemActionClickBlock,
		TriggerType:      v2193.TriggerTypePlayerInput,
		BlockPosition:    [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		BlockFace:        int32(cube.FaceUp),
		HotBarSlot:       emptyHand,
		Position:         mgl32.Vec3{0.5, -58.38, 0.5},
		ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
		ClientPrediction: v2193.ClientPredictionSuccess,
	}}
}

// removeItems removes the item entities of the world in the order they
// were made: the world holds its entities in no order.
func removeItems(tx *world.Tx) {
	var items []world.Entity
	for e := range tx.Entities() {
		if e.H().Type() == entity.ItemType {
			items = append(items, e)
		}
	}
	slices.SortFunc(items, func(a, b world.Entity) int { return cmp.Compare(a.H().RuntimeID(), b.H().RuntimeID()) })
	for _, e := range items {
		tx.RemoveEntity(e)
	}
}
