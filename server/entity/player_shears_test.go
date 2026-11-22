package entity_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// soundNameRecorder records the names of custom and event sounds a world plays.
type soundNameRecorder struct {
	world.NopHandler
	names      []string
	eventNames []string
}

func (r *soundNameRecorder) HandleSound(_ *world.Context, s world.Sound, _ mgl64.Vec3) {
	switch s := s.(type) {
	case sound.Custom:
		r.names = append(r.names, s.Name)
	case sound.Event:
		r.eventNames = append(r.eventNames, s.Name)
	}
}

// playerWithShears adds a survival player holding shears next to the origin.
func playerWithShears(tx *world.Tx) interface {
	UseItemOnEntity(world.Entity) bool
	HeldItems() (item.Stack, item.Stack)
} {
	p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
	p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})
	return p
}

// TestPlayerShearsWhatJavaShears checks the mobInteract of each Shearable in
// 26.3: a player with shears shears a mooshroom, a snow golem, a copper golem,
// a sulfur cube and a sheep, each with its own sound and drops, and the shears
// wear down by one. A mob that is not ready is left alone and the shears keep
// their durability.
func TestPlayerShearsWhatJavaShears(t *testing.T) {
	t.Parallel()
	at := world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}
	for _, c := range []struct {
		name  string
		spawn func() *world.EntityHandle
		sound string
		drop  string
		// done reports whether the mob was sheared.
		done func(tx *world.Tx, e world.Entity) bool
	}{
		{"sheep", func() *world.EntityHandle { return entity.NewSheep(at) }, "entity.sheep.shear", "minecraft:white_wool",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.Sheep).Sheared() }},
		{"mooshroom", func() *world.EntityHandle { return entity.NewMooshroom(at) }, "entity.mooshroom.shear", "minecraft:red_mushroom",
			func(tx *world.Tx, _ world.Entity) bool {
				return len(entitiesOfType(tx, entity.MooshroomType)) == 0 && len(entitiesOfType(tx, entity.CowType)) == 1
			}},
		{"snow golem", func() *world.EntityHandle { return entity.NewSnowGolem(at) }, "entity.snow_golem.shear", "minecraft:carved_pumpkin",
			func(_ *world.Tx, e world.Entity) bool { return !e.(*entity.SnowGolem).HasPumpkin() }},
		{"copper golem", func() *world.EntityHandle {
			return at.New(entity.CopperGolemType, entity.CopperGolemBehaviourConfig{NextWeatheringTick: -1, Antenna: item.NewStack(block.Flower{Type: block.Poppy()}, 1)})
		}, "entity.copper_golem.shear", "minecraft:poppy",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.CopperGolem).Antenna().Empty() }},
		{"sulfur cube", func() *world.EntityHandle {
			return at.New(entity.SulfurCubeType, entity.SulfurCubeBehaviourConfig{Size: 2, BodyItem: item.NewStack(block.Stone{}, 1)})
		}, "entity.sulfur_cube.eject", "minecraft:stone",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.SulfurCube).BodyItem().Empty() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := playerWithShears(tx)
				mob := tx.AddEntity(c.spawn())
				if !p.UseItemOnEntity(mob) {
					t.Fatal("the player could not use the shears on the mob")
				}
				if !c.done(tx, mob) {
					t.Fatal("the mob was not sheared")
				}
				if held, _ := p.HeldItems(); !worn(held) {
					t.Errorf("shears are %v, want them worn down by one", held)
				}
				if len(r.names) != 1 || r.names[0] != c.sound {
					t.Errorf("played %v, want only %s", r.names, c.sound)
				}
				var dropped []string
				for _, e := range entitiesOfType(tx, entity.ItemType) {
					name, _ := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Item().EncodeItem()
					dropped = append(dropped, name)
				}
				if len(dropped) == 0 || dropped[0] != c.drop {
					t.Errorf("dropped %v, want %s", dropped, c.drop)
				}
			})
		})
	}

	t.Run("things that are not ready", func(t *testing.T) {
		t.Parallel()
		for name, spawn := range map[string]func() *world.EntityHandle{
			"copper golem without an antenna": func() *world.EntityHandle { return entity.NewCopperGolem(at) },
			"sulfur cube without a block":     func() *world.EntityHandle { return entity.NewSulfurCube(at) },
			"mooshroom calf": func() *world.EntityHandle {
				return at.New(entity.MooshroomType, entity.MooshroomConfig{Age: -24000})
			},
			"lamb": func() *world.EntityHandle { return at.New(entity.SheepType, entity.SheepConfig{AgeTicks: -24000}) },
		} {
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := playerWithShears(tx)
				p.UseItemOnEntity(tx.AddEntity(spawn()))
				held, _ := p.HeldItems()
				if held.Durability() != held.MaxDurability() || len(r.names) != 0 || len(entitiesOfType(tx, entity.ItemType)) != 0 {
					t.Errorf("%s: shears are %v, played %v and %d items dropped, want nothing done",
						name, held, r.names, len(entitiesOfType(tx, entity.ItemType)))
				}
			})
		}
	})
}

// TestPlayerShearsCutLeads checks Entity.interact: a player with shears cuts the
// lead of a mob before anything else, the lead drops, shears snip and wear down
// by one, and the sheep on the lead keeps its wool.
func TestPlayerShearsCutLeads(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := playerWithShears(tx)
		sheep := tx.AddEntity(entity.NewSheep(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}))
		leash := sheep.(interface{ LeashEnt() *entity.Ent }).LeashEnt()
		holder := addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, .5})
		leash.AttachLeash(holder, tx)
		if !p.UseItemOnEntity(sheep) {
			t.Fatal("the player could not use the shears on the sheep")
		}
		if leash.HolderLeashID() != 0 || sheep.(*entity.Sheep).Sheared() {
			t.Error("want the lead cut and the wool kept")
		}
		if held, _ := p.HeldItems(); !worn(held) {
			t.Errorf("shears are %v, want them worn down by one", held)
		}
		if len(r.names) != 1 || r.names[0] != "item.shears.snip" {
			t.Errorf("played %v, want only item.shears.snip", r.names)
		}
		items := entitiesOfType(tx, entity.ItemType)
		if len(items) != 1 {
			t.Fatalf("%d items dropped, want the lead", len(items))
		}
		if _, ok := items[0].(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Item().(item.Lead); !ok {
			t.Error("the item dropped is not a lead")
		}
	})
}

// TestPlayerShearsRefusesPrimedSulfurCube checks Java 26.3 SulfurCube.mobInteract:
// a player cannot shear a primed sulfur cube, though leashes are still cut first
// by Entity.interact.
func TestPlayerShearsRefusesPrimedSulfurCube(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := playerWithShears(tx)
		at := world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}
		cubeMob := tx.AddEntity(at.New(entity.SulfurCubeType, entity.SulfurCubeBehaviourConfig{
			Size:     2,
			BodyItem: item.NewStack(block.TNT{}, 1),
		})).(*entity.SulfurCube)
		if !cubeMob.Prime() {
			t.Fatal("failed to prime sulfur cube")
		}
		r.names = nil
		p.UseItemOnEntity(cubeMob)
		if cubeMob.BodyItem().Empty() {
			t.Fatal("primed sulfur cube was sheared by player")
		}
		if held, _ := p.HeldItems(); held.Durability() != held.MaxDurability() || len(r.names) != 0 {
			t.Fatalf("shears = %v, sounds = %v, want untouched", held, r.names)
		}
	})
}

// TestPlayerShearsHappyGhastHarnessParity checks Java 26.3 HappyGhast harness
// equip/shearing: equipping plays entity.happy_ghast.equip; shearing refuses
// while ridden or sneaking, and otherwise drops the harness, plays
// entity.happy_ghast.unequip, emits GameEventShear, and wears shears by one.
func TestPlayerShearsHappyGhastHarnessParity(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
		rider := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		ghast := tx.AddEntity(entity.NewHappyGhast(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}})).(*entity.HappyGhast)

		if !ghast.EquipHarness(tx, p, item.Harness{Colour: item.ColourWhite()}) {
			t.Fatal("failed to equip harness")
		}
		if !slices.Contains(r.names, "entity.happy_ghast.equip") {
			t.Fatalf("equip sounds = %v, want entity.happy_ghast.equip", r.names)
		}
		p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})

		// Ridden happy ghast cannot be sheared; interacting falls through to mounting an open seat.
		if !ghast.Mount(tx, rider) {
			t.Fatal("rider failed to mount happy ghast")
		}
		r.names = nil
		p.UseItemOnEntity(ghast)
		if !ghast.Harnessed() || rider.Riding() != ghast.H() {
			t.Fatal("ridden happy ghast was sheared or rider was dismounted")
		}
		if held, _ := p.HeldItems(); held.Durability() != held.MaxDurability() {
			t.Fatalf("shears damaged on ridden ghast: %v", held)
		}
		ghast.Dismount(tx, p)
		ghast.Dismount(tx, rider)

		// Sneaking player cannot shear harness.
		p.StartSneaking()
		r.names = nil
		p.UseItemOnEntity(ghast)
		if !ghast.Harnessed() {
			t.Fatal("sneaking player sheared happy ghast harness")
		}
		p.StopSneaking()

		// Standing player shears unmounted happy ghast.
		sensor := cube.Pos{2, 64, 3}
		tx.SetBlock(sensor, block.NewSculkSensor(), nil)
		r.names = nil
		p.UseItemOnEntity(ghast)
		if ghast.Harnessed() {
			t.Fatal("happy ghast still harnessed after shearing")
		}
		if held, _ := p.HeldItems(); !worn(held) {
			t.Fatalf("shears = %v, want worn by one", held)
		}
		if !slices.Contains(r.names, "entity.happy_ghast.unequip") {
			t.Fatalf("unequip sounds = %v, want entity.happy_ghast.unequip", r.names)
		}
		data := tx.Block(sensor).(world.VibrationListener).VibrationData()
		if data == nil || !data.Selector.HasCandidate || data.Selector.Candidate.Event != world.GameEventShear {
			t.Fatalf("sculk sensor candidate = %+v, want GameEventShear", data)
		}
	})
}

// TestPlayerShearsNautilusEquipmentOrder checks Java 26.3 Mob.attemptToShearEquipment
// on a nautilus: sneaking refuses shearing, body armor shears before the saddle,
// a ridden nautilus refuses saddle shearing, and shearing the saddle drops it
// with the unsaddle event sound and GameEventShear.
func TestPlayerShearsNautilusEquipmentOrder(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})

		data := world.EntityData{Pos: mgl64.Vec3{2.5, 64, .5}}
		entity.NautilusType.DecodeNBT(map[string]any{
			"Tame":      byte(1),
			"Saddle":    item.WriteNBT(item.NewStack(item.Saddle{}, 1)),
			"BodyArmor": item.WriteNBT(item.NewStack(item.NautilusArmor{Material: item.NautilusArmorIron}, 1)),
		}, &data)
		naut := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}.New(entity.NautilusType, nautilusFromData{data.Data})).(*entity.Nautilus)

		// Sneaking refuses shearing armor and saddle, and does not mount.
		p.StartSneaking()
		p.UseItemOnEntity(naut)
		if naut.BodyArmor().Empty() || !naut.Saddled() || p.Riding() != nil {
			t.Fatal("sneaking player sheared or mounted nautilus")
		}
		p.StopSneaking()

		// First standing shear removes body armor first (BODY before SADDLE).
		r.names, r.eventNames = nil, nil
		p.UseItemOnEntity(naut)
		if !naut.BodyArmor().Empty() || !naut.Saddled() {
			t.Fatalf("after first shear: armor=%v saddled=%v, want armor empty and saddle kept", naut.BodyArmor(), naut.Saddled())
		}
		if !slices.Contains(r.names, "item.armor.unequip_nautilus") {
			t.Fatalf("armor shear sounds = %v, want item.armor.unequip_nautilus", r.names)
		}

		// Ridden nautilus refuses shearing saddle.
		rider := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		if !naut.Mount(tx, rider) {
			t.Fatal("rider failed to mount nautilus")
		}
		p.UseItemOnEntity(naut)
		if !naut.Saddled() {
			t.Fatal("ridden nautilus saddle was sheared")
		}
		naut.Dismount(tx, rider)

		// Second standing shear removes saddle, drops it, plays unsaddle, and emits GameEventShear.
		sensor := cube.Pos{2, 64, 3}
		tx.SetBlock(sensor, block.NewSculkSensor(), nil)
		r.names, r.eventNames = nil, nil
		p.UseItemOnEntity(naut)
		if naut.Saddled() {
			t.Fatal("nautilus still saddled after second shear")
		}
		if !slices.Contains(r.eventNames, "unsaddle") {
			t.Fatalf("saddle shear event sounds = %v, want unsaddle", r.eventNames)
		}
		var droppedSaddle bool
		for _, e := range entitiesOfType(tx, entity.ItemType) {
			if _, ok := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Item().(item.Saddle); ok {
				droppedSaddle = true
			}
		}
		if !droppedSaddle {
			t.Fatal("shearing nautilus saddle did not drop a saddle item")
		}
		vib := tx.Block(sensor).(world.VibrationListener).VibrationData()
		if vib == nil || !vib.Selector.HasCandidate || vib.Selector.Candidate.Event != world.GameEventShear {
			t.Fatalf("sculk sensor candidate = %+v, want GameEventShear", vib)
		}
	})
}

type nautilusFromData struct{ data any }

func (n nautilusFromData) Apply(d *world.EntityData) { d.Data = n.data }

// TestPlayerShearsWolfArmorRespectsSneaking checks Java 26.3 Entity.interact +
// Wolf.canShearEquipment: a sneaking owner cannot shear wolf armor, while a
// standing owner shears it with item.armor.unequip_wolf and GameEventShear.
func TestPlayerShearsWolfArmorRespectsSneaking(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})
		wolf := tx.AddEntity(entity.NewWolf(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}, entity.WolfBehaviourConfig{
			Tamed:     true,
			OwnerUUID: p.H().UUID().String(),
			BodyArmor: item.WolfArmor{},
		})).(*entity.Wolf)
		if wolf.BodyArmor().Empty() {
			// NewWolf checks BodyArmor != (item.WolfArmor{}); equip via Armor slot through EquipBodyArmor if empty.
			if !wolf.EquipBodyArmor(tx, p, item.WolfArmor{}) {
				t.Fatal("failed to equip wolf armor")
			}
		}

		p.StartSneaking()
		p.UseItemOnEntity(wolf)
		if wolf.BodyArmor().Empty() {
			t.Fatal("sneaking owner sheared wolf armor")
		}
		p.StopSneaking()

		sensor := cube.Pos{2, 64, 3}
		tx.SetBlock(sensor, block.NewSculkSensor(), nil)
		r.names = nil
		p.UseItemOnEntity(wolf)
		if !wolf.BodyArmor().Empty() {
			t.Fatal("standing owner failed to shear wolf armor")
		}
		if !slices.Contains(r.names, "item.armor.unequip_wolf") {
			t.Fatalf("wolf armor shear sounds = %v, want item.armor.unequip_wolf", r.names)
		}
		vib := tx.Block(sensor).(world.VibrationListener).VibrationData()
		if vib == nil || !vib.Selector.HasCandidate || vib.Selector.Candidate.Event != world.GameEventShear {
			t.Fatalf("sculk sensor candidate = %+v, want GameEventShear", vib)
		}
	})
}
