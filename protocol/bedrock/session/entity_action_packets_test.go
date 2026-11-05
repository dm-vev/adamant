package session_test

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

var updateActions = flag.Bool("update-actions", false, "rewrite testdata/entity_actions.golden, the packets of every entity action")

// actionSample is an action to view in the tests of the mapping of entity
// actions: performed by a target, in a scene, by a label that names it in
// testdata/entity_actions.golden.
type actionSample struct {
	label  string
	scene  session.ActionScene
	target world.Entity
	action world.EntityAction
}

// actionWorld holds the entities the samples of the actions are performed by.
type actionWorld struct {
	self, other, cow, hiddenCow, zombie, dragon world.Entity
	eaters                                      map[string]world.Entity
	// ids number the entities by a number of the tests' own, as the runtime
	// IDs of the handles come from a counter the tests of the package share.
	ids map[uint64]uint64
}

// newActionWorld adds the entities of the samples to the transaction passed.
func newActionWorld(tx *world.Tx) *actionWorld {
	pos := mgl64.Vec3{8.5, 64, 8.5}
	addPlayer := func() *player.Player {
		return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos})).(*player.Player)
	}
	w := &actionWorld{eaters: map[string]world.Entity{}, ids: map[uint64]uint64{}}
	w.self, w.other = addPlayer(), addPlayer()
	w.cow = tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos}))
	w.hiddenCow = tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos}))
	w.zombie = tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: pos}))
	w.dragon = tx.AddEntity(entity.NewEnderDragon(world.EntitySpawnOpts{Position: pos}))
	for _, held := range []struct {
		name  string
		stack item.Stack
	}{
		{"empty", item.Stack{}}, {"bread", item.NewStack(item.Bread{}, 1)}, {"apple", item.NewStack(item.Apple{}, 3)},
		{"sword", item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)},
	} {
		p := addPlayer()
		p.SetHeldItems(held.stack, item.Stack{})
		w.eaters[held.name] = p
	}
	w.ids[w.self.H().RuntimeID()] = 1
	for i, e := range []world.Entity{w.other, w.cow, w.hiddenCow, w.zombie, w.dragon, w.eaters["empty"], w.eaters["bread"], w.eaters["apple"], w.eaters["sword"]} {
		w.ids[e.H().RuntimeID()] = uint64(100 + i)
	}
	return w
}

// scene returns the scene in which the entities passed are shown, along with
// the entities of the world that are always shown.
func (w *actionWorld) scene(shown ...world.Entity) session.ActionScene {
	return session.ActionScene{Self: w.self, Shown: append([]world.Entity{w.other, w.cow, w.zombie, w.dragon}, shown...)}
}

// actionSamples returns the actions to view: the zero value of every type of
// server/entity that is a world.EntityAction, from the generated list, so that
// none is forgotten, performed by each kind of entity, and for the types whose
// packets depend on their fields the values that lead to each of their
// packets.
func actionSamples(w *actionWorld) []actionSample {
	var samples []actionSample
	targets := []struct {
		name string
		e    world.Entity
	}{
		{"self", w.self}, {"other player", w.other}, {"cow", w.cow}, {"hidden cow", w.hiddenCow},
		{"zombie", w.zombie}, {"ender dragon", w.dragon},
	}
	for _, a := range entityActionTypes {
		for _, t := range targets {
			samples = append(samples, actionSample{fmt.Sprintf("%T{} by %s", a, t.name), w.scene(), t.e, a})
		}
	}
	add := func(a world.EntityAction, detail string, target world.Entity, name string) {
		samples = append(samples, actionSample{fmt.Sprintf("%T{%s} by %s", a, detail, name), w.scene(), target, a})
	}
	for _, count := range []int{-1, 0, 1, 7, 100} {
		for _, t := range targets[:3] {
			add(entity.CriticalHitAction{Count: count}, fmt.Sprint("count ", count), t.e, t.name)
			add(entity.EnchantedHitAction{Count: count}, fmt.Sprint("count ", count), t.e, t.name)
		}
	}
	for _, count := range []int{-1, 0, 1, 64, 1 << 20} {
		add(entity.ItemStackSizeAction{Count: count}, fmt.Sprint("count ", count), w.cow, "cow")
	}
	for _, d := range []time.Duration{0, time.Millisecond, 49 * time.Millisecond, 50 * time.Millisecond, 125 * time.Millisecond, time.Second, -time.Second, 3 * time.Hour} {
		add(entity.ArrowShakeAction{Duration: d}, fmt.Sprint("duration ", d), w.cow, "cow")
	}
	for _, b := range []bool{false, true} {
		add(entity.TameAction{Succeeded: b}, fmt.Sprint("succeeded ", b), w.cow, "cow")
		add(entity.DrinkAction{Milk: b}, fmt.Sprint("milk ", b), w.cow, "cow")
		add(entity.DrinkAction{Milk: b}, fmt.Sprint("milk ", b), w.self, "self")
	}
	for _, collector := range []struct {
		name string
		e    world.Entity
	}{{"self", w.self}, {"other player", w.other}, {"cow", w.cow}, {"hidden cow", w.hiddenCow}, {"nil", nil}} {
		for _, t := range targets[2:4] {
			add(entity.PickedUpAction{Collector: collector.e}, "collector "+collector.name, t.e, t.name)
		}
	}
	for _, name := range []string{"empty", "bread", "apple", "sword"} {
		samples = append(samples, actionSample{"entity.EatAction{} by player holding " + name, w.scene(w.eaters[name]), w.eaters[name], entity.EatAction{}})
	}
	for _, swinging := range []bool{false, true} {
		for _, t := range targets[:3] {
			sc := w.scene()
			sc.SwingingArm = swinging
			samples = append(samples, actionSample{fmt.Sprintf("entity.SwingArmAction{} by %s while swinging %v", t.name, swinging), sc, t.e, entity.SwingArmAction{}})
		}
	}
	for _, congested := range []bool{false, true} {
		sc := w.scene()
		sc.Congested = congested
		samples = append(samples, actionSample{fmt.Sprintf("entity.OfferFlowerAction{} by cow while congested %v", congested), sc, w.cow, entity.OfferFlowerAction{}})
	}
	return samples
}

// withActionWorld runs f in a transaction of a world with the entities of the
// samples.
func withActionWorld(t *testing.T, f func(w *actionWorld)) {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) { f(newActionWorld(tx)) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// numberActionIDs replaces the runtime IDs of the packets passed with the
// numbers of the world passed, and returns the numbers of the runtime IDs
// passed. It panics for a packet that carries an ID it does not know of.
func numberActionIDs(w *actionWorld, pks []proto.Packet, dying []uint64) []uint64 {
	number := func(id uint64) uint64 {
		if n, ok := w.ids[id]; ok {
			return n
		}
		return id
	}
	for _, pk := range pks {
		switch p := pk.(type) {
		case *mc.ActorEventPacket:
			p.EntityRuntimeID = number(p.EntityRuntimeID)
		case *mc.AnimatePacket:
			p.EntityRuntimeID = number(p.EntityRuntimeID)
		case *mc.TakeItemActorPacket:
			p.ItemEntityRuntimeID, p.TakerEntityRuntimeID = number(p.ItemEntityRuntimeID), number(p.TakerEntityRuntimeID)
		case *mc.UpdateAttributesPacket:
			p.EntityRuntimeID = number(p.EntityRuntimeID)
		case *mc.AnimateEntityPacket:
			for i := range p.EntityRuntimeIDs {
				p.EntityRuntimeIDs[i] = number(p.EntityRuntimeIDs[i])
			}
		default:
			panic(fmt.Sprintf("entity actions: no runtime ID to number in %T", pk))
		}
	}
	numbered := make([]uint64, len(dying))
	for i, id := range dying {
		numbered[i] = number(id)
	}
	return numbered
}

// actionLines returns the lines of testdata/entity_actions.golden for a
// sample: the packets as each protocol is sent them, and the runtime IDs of
// the dying mobs the session remembered.
func actionLines(w *actionWorld, c actionSample, show func(session.ActionScene, world.Entity, world.EntityAction) session.ActionResult) (lines []string) {
	lines = []string{c.label}
	defer func() {
		if r := recover(); r != nil {
			lines = append(lines, fmt.Sprint("  panic: ", r))
		}
	}()
	r := show(c.scene, c.target, c.action)
	dying := numberActionIDs(w, r.Packets, r.DeathAnimations)
	lines = append(lines, encodeTablePackets(r.Packets)...)
	if len(dying) > 0 {
		lines = append(lines, fmt.Sprint("  remembers dying mobs ", dying))
	}
	return lines
}

// TestEntityActionPacketsMatch views every action of actionSamples, which
// holds every type of the actions of the game, and compares the packets as
// each protocol is sent them with testdata/entity_actions.golden, which was
// recorded from the switch of Session.ViewEntityAction before the actions were
// shown by a table: the table and the cases that remain give the packets the
// switch gave. Run with -update-actions to write it again, and read the diff.
func TestEntityActionPacketsMatch(t *testing.T) {
	withActionWorld(t, func(w *actionWorld) {
		var got []string
		for _, c := range actionSamples(w) {
			got = append(got, actionLines(w, c, session.EntityActionPackets)...)
		}
		checkTableGolden(t, filepath.Join("testdata", "entity_actions.golden"), *updateActions, "actions", got)
	})
}

// TestEntityActionTableMatchesSwitch views every action of actionSamples with
// the switch of Session.ViewEntityAction and with the table and the cases that
// remain, and requires the same packets, field by field, and the same
// remembered dying mobs.
func TestEntityActionTableMatchesSwitch(t *testing.T) {
	withActionWorld(t, func(w *actionWorld) {
		for _, c := range actionSamples(w) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						// A panic is the same on both paths: the golden holds it.
						if !panicsAlike(w, c) {
							t.Errorf("%s: the paths panic differently", c.label)
						}
					}
				}()
				old := session.EntityActionPackets(c.scene, c.target, c.action)
				new := session.TabledEntityActionPackets(c.scene, c.target, c.action)
				if !reflect.DeepEqual(old, new) {
					t.Errorf("%s:\nswitch: %s\ntable:  %s", c.label, describeAction(old), describeAction(new))
				}
			}()
		}
	})
}

// panicsAlike reports whether both paths panic with the same value for the
// sample.
func panicsAlike(w *actionWorld, c actionSample) bool {
	try := func(show func(session.ActionScene, world.Entity, world.EntityAction) session.ActionResult) (v any) {
		defer func() { v = recover() }()
		show(c.scene, c.target, c.action)
		return nil
	}
	a, b := try(session.EntityActionPackets), try(session.TabledEntityActionPackets)
	return a != nil && fmt.Sprint(a) == fmt.Sprint(b)
}

// describeAction returns what a session did as text, the packets by the fields
// they point to.
func describeAction(r session.ActionResult) string {
	var s string
	for _, pk := range r.Packets {
		s += fmt.Sprintf("%T%+v ", pk, pk)
	}
	return fmt.Sprintf("%sdying %v", s, r.DeathAnimations)
}
