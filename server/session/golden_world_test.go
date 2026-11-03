package session_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// otherUUID is the UUID of the other player of the world scenario.
var otherUUID = uuid.MustParse("00000000-0000-4000-8000-0000000007e2")

// TestGoldenWorld records the world as a client sees it: another player
// that spawns, moves, changes metadata, armour and held items, swings its
// arm, is hurt and sleeps; a chest the player opens and closes, sounds,
// particles, an item cooldown, the sleeping indicator and a map.
func TestGoldenWorld(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "world", scenarioConfig{})

	var other *world.EntityHandle
	s.step("another player spawns", func(tx *world.Tx, p *player.Player) {
		pos := p.Position().Add(mgl64.Vec3{3, 0, 0})
		other = world.EntitySpawnOpts{Position: pos, ID: otherUUID}.New(player.Type, player.Config{
			Name: "Other", UUID: otherUUID, XUID: "2535400000000043", Position: pos, GameMode: world.GameModeSurvival,
		})
		tx.AddEntity(other)
	})
	withOther := func(f func(tx *world.Tx, o *player.Player)) func(tx *world.Tx, p *player.Player) {
		return func(tx *world.Tx, _ *player.Player) {
			e, ok := other.Entity(tx)
			if !ok {
				t.Fatal("the other player left")
			}
			f(tx, e.(*player.Player))
		}
	}
	s.step("it moves and turns", withOther(func(_ *world.Tx, o *player.Player) {
		o.Move(mgl64.Vec3{0.5, 0, 0.25}, 45, 10)
	}))
	s.tick("movement in the tick", 1)
	s.step("it teleports", withOther(func(_ *world.Tx, o *player.Player) {
		o.Teleport(o.Position().Add(mgl64.Vec3{1, 0, 1}))
	}))
	s.tick("teleport in the tick", 1)
	s.step("its metadata: sneaking, name tag, on fire", withOther(func(_ *world.Tx, o *player.Player) {
		o.StartSneaking()
		o.SetNameTag("§nOther")
		o.SetOnFire(time.Second)
	}))
	s.step("its armour", withOther(func(_ *world.Tx, o *player.Player) {
		o.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1))
		o.Armour().SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1))
	}))
	s.step("its held items", withOther(func(_ *world.Tx, o *player.Player) {
		o.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Shield{}, 1))
	}))
	s.step("it swings its arm", withOther(func(_ *world.Tx, o *player.Player) {
		o.SwingArm()
	}))
	s.step("it is hurt", withOther(func(_ *world.Tx, o *player.Player) {
		o.Hurt(2, entity.FreezeDamageSource{})
	}))
	s.tick("hurt in the tick", 1)

	bed := cube.Pos{3, -60, 3}
	s.step("a bed is placed and it sleeps in it", func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(bed, block.Bed{Facing: cube.South, Head: false}, nil)
		tx.SetBlock(bed.Side(cube.FaceSouth), block.Bed{Facing: cube.South, Head: true}, nil)
		e, _ := other.Entity(tx)
		e.(*player.Player).Sleep(bed)
	})
	s.step("the sleeping indicator", func(_ *world.Tx, p *player.Player) {
		p.SendSleepingIndicator(1, 2)
	})
	s.step("it wakes", withOther(func(_ *world.Tx, o *player.Player) {
		o.Wake()
	}))
	s.step("it leaves", func(tx *world.Tx, _ *player.Player) {
		e, _ := other.Entity(tx)
		_ = e.(*player.Player).Close()
	})

	// The client walks a few steps east, a PlayerAuthInput a tick, at the
	// position of its eyes.
	for i := 1; i <= 3; i++ {
		x := 0.5 + 0.2*float32(i)
		s.client(fmt.Sprintf("the client walks east, input %d", i), &v2193.PlayerAuthInputPacket{
			Yaw: -90, HeadYaw: -90,
			Position:   [3]float32{x, -58.38, 0.5},
			MoveVector: [2]float32{0, 1},
			Delta:      [3]float32{0.2, 0, 0},
			Tick:       uint64(i),
			InputMode:  v2193.InputModeMouse,
			PlayMode:   v2193.PlayModeNormal,
		})
	}

	chest := cube.Pos{-2, -60, 0}
	s.step("a chest is placed", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(chest, block.NewChest(), nil)
	})
	s.step("the player opens it", func(tx *world.Tx, p *player.Player) {
		p.OpenBlockContainer(chest, tx)
	})
	s.client("the client closes it", &v2193.ContainerClosePacket{WindowID: 1, ContainerType: v2193.ContainerTypeContainer, ServerSide: false})
	s.tick("after it closed", 1)

	s.step("sounds", func(tx *world.Tx, p *player.Player) {
		tx.PlaySound(mgl64.Vec3{1.5, -60, 1.5}, sound.Click{})
		tx.PlaySound(mgl64.Vec3{1.5, -60, 1.5}, sound.Explosion{})
		tx.PlaySound(mgl64.Vec3{1.5, -60, 1.5}, sound.BlockPlace{Block: block.Stone{}})
		p.PlaySound(sound.LevelUp{})
		p.PlaySound(sound.Note{Instrument: sound.Piano(), Pitch: 12})
	})
	s.step("particles", func(tx *world.Tx, p *player.Player) {
		tx.AddParticle(mgl64.Vec3{1.5, -59, 1.5}, particle.HugeExplosion{})
		tx.AddParticle(mgl64.Vec3{1.5, -59, 1.5}, particle.BlockBreak{Block: block.Dirt{}})
		tx.AddParticle(mgl64.Vec3{1.5, -59, 1.5}, particle.Flame{})
		p.ShowParticle(mgl64.Vec3{1.5, -59, 1.5}, particle.Note{Instrument: sound.Piano(), Pitch: 3})
	})
	s.step("item cooldown", func(_ *world.Tx, p *player.Player) {
		p.SetCooldown(item.EnderPearl{}, time.Second)
	})

	var mapID int64
	s.step("a map is made and held", func(tx *world.Tx, p *player.Player) {
		data, err := tx.CreateMap(cube.Pos{0, -60, 0}, 0)
		if err != nil {
			t.Fatal(err)
		}
		mapID = data.ID
		_ = p.Inventory().SetItem(0, item.NewStack(item.FilledMap{NativeID: data.ID, HasNativeID: true}, 1))
	})
	s.client("the client asks for the map", &v2193.MapInfoRequestPacket{MapID: mapID})
	s.snapshot()
}
