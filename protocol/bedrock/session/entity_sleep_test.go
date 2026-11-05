package session

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// bedVillager is a villager that lies in a bed or not, as a test sets it,
// where a real one lies once its brain sends it to sleep.
type bedVillager struct {
	*entity.Villager
	bed    cube.Pos
	facing cube.Direction
	asleep bool
}

func (b bedVillager) Sleeping() (cube.Pos, bool)  { return b.bed, b.asleep }
func (b bedVillager) SleepFacing() cube.Direction { return b.facing }

// actorFlag reports whether a flag of the actor is set, in whichever of the
// two flag fields holds it.
func actorFlag(m mc.Metadata, flag uint8) bool {
	if flag < 64 {
		return m.Flag(mc.EntityDataKeyFlags, flag)
	}
	return m.Flag(mc.EntityDataKeyFlagsTwo, flag-64)
}

// for2193 returns the one packet a client of 2193 is sent for a packet of
// 2225, which the converter makes of it.
func for2193(t *testing.T, pk proto.Packet) proto.Packet {
	t.Helper()
	out := c2225.Down(pk)
	if len(out) != 1 {
		t.Fatalf("%T converts to %d packets for clients of 2193, want 1", pk, len(out))
	}
	return out[0]
}

// TestSleepingVillagerIsViewedAsBDSViews records what BDS 1.26.61.1 sends of
// a villager lying in a bed (a bot beside four beds, one facing each way,
// and villagers that went to sleep at midnight), and compares it. BDS sets
// flag 76 (Sleeping) in place of the bed position and player flag, shrinks
// its box to 1 by 0.3, lays it 0.375 blocks from the middle of the head of
// the bed towards the foot and 0.6 over the block, and turns its body and
// head to face the way the bed points; a viewer that arrives later is sent
// the same in AddActor.
func TestSleepingVillagerIsViewedAsBDSViews(t *testing.T) {
	t.Parallel()
	if mc.EntityDataFlagSleeping != 76 || v2193.EntityDataFlagSleeping != 76 {
		t.Fatalf("Sleeping flag = %d of 2225 and %d of 2193, want 76", mc.EntityDataFlagSleeping, v2193.EntityDataFlagSleeping)
	}
	// The head blocks of the beds BDS had villagers sleep in, and what it
	// sent for each: the position of the villager and its yaw. The beds face
	// from the foot to the head.
	cases := []struct {
		name   string
		facing cube.Direction
		head   cube.Pos
		pos    mgl64.Vec3
		yaw    float64
	}{
		{"north", cube.North, cube.Pos{3, 100, 2}, mgl64.Vec3{3.5, 100.6, 2.875}, 180},
		{"east", cube.East, cube.Pos{11, 100, 3}, mgl64.Vec3{11.125, 100.6, 3.5}, 270},
		{"south", cube.South, cube.Pos{17, 100, 4}, mgl64.Vec3{17.5, 100.6, 4.125}, 0},
		{"west", cube.West, cube.Pos{23, 100, 3}, mgl64.Vec3{23.875, 100.6, 3.5}, 90},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The villager of the game lies in the middle of the head
			// block, 0.6875 blocks up.
			at := c.head.Vec3Middle().Add(mgl64.Vec3{0, 0.6875})
			h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:villager"}, world.EntitySpawnOpts{Position: at, SpawnFinalized: true})
			if err != nil {
				t.Fatal(err)
			}
			w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
			defer w.Close()
			viewer := testSession("sleeper")
			viewer.br = world.DefaultBlockRegistry
			<-w.Exec(func(tx *world.Tx) {
				v := bedVillager{Villager: tx.AddEntity(h).(*entity.Villager), bed: c.head, facing: c.facing, asleep: true}

				// A viewer that walks up to the villager asleep.
				viewer.ViewEntity(v)
				added, ok := viewer.testPacket().(*mc.AddActorPacket)
				if !ok {
					t.Fatal("no AddActor")
				}
				checkSleeping(t, "AddActor", added.EntityMetadata, true)
				checkSleeping(t, "AddActor of 2193", mc.Metadata(for2193(t, added).(*v2193.AddActorPacket).EntityMetadata), true)
				if got := added.Position; !near(vec32(got), c.pos) {
					t.Errorf("AddActor position = %v, want %v", got, c.pos)
				}
				for name, got := range map[string]float32{"Yaw": added.Yaw, "HeadYaw": added.HeadYaw, "BodyYaw": added.BodyYaw} {
					if !sameAngle(float64(got), c.yaw) {
						t.Errorf("AddActor %s = %v, want %v", name, got, c.yaw)
					}
				}
				if added.Pitch != 0 {
					t.Errorf("AddActor pitch = %v, want 0", added.Pitch)
				}

				// A viewer that sees it lie down.
				viewer.ViewEntityState(v)
				state, ok := viewer.testPacket().(*mc.SetActorDataPacket)
				if !ok {
					t.Fatal("no SetActorData")
				}
				checkSleeping(t, "SetActorData", state.EntityMetadata, true)
				checkSleeping(t, "SetActorData of 2193", mc.Metadata(for2193(t, state).(*v2193.SetActorDataPacket).EntityMetadata), true)
				viewer.ViewEntityMovement(v, v.Position(), v.Rotation(), true)
				moved, ok := viewer.testPacket().(*mc.MoveActorAbsolutePacket)
				if !ok {
					t.Fatal("no MoveActorAbsolute")
				}
				if down := for2193(t, moved).(*v2193.MoveActorAbsolutePacket); down.Position != moved.Position || down.Rotation != moved.Rotation {
					t.Errorf("MoveActorAbsolute of 2193 = %v %v, want %v %v", down.Position, down.Rotation, moved.Position, moved.Rotation)
				}
				if !near(vec32(moved.Position), c.pos) {
					t.Errorf("MoveActorAbsolute position = %v, want %v", moved.Position, c.pos)
				}
				if moved.Rotation[0] != 0 || !sameAngle(float64(moved.Rotation[1]), c.yaw) || !sameAngle(float64(moved.Rotation[2]), c.yaw) {
					t.Errorf("MoveActorAbsolute rotation = %v, want pitch 0, yaw and head %v", moved.Rotation, c.yaw)
				}

				// It gets up: BDS sends the box back and clears the flag.
				v.asleep = false
				viewer.ViewEntityState(v)
				state = viewer.testPacket().(*mc.SetActorDataPacket)
				checkSleeping(t, "SetActorData after waking", state.EntityMetadata, false)
			})
		})
	}
}

// checkSleeping checks the metadata of a villager that lies in a bed or
// stands.
func checkSleeping(t *testing.T, what string, m mc.Metadata, asleep bool) {
	t.Helper()
	if got := actorFlag(m, mc.EntityDataFlagSleeping); got != asleep {
		t.Errorf("%s: Sleeping flag = %t, want %t", what, got, asleep)
	}
	width, height := float32(0.6), float32(1.9)
	if asleep {
		width, height = 1, 0.3
	}
	if m[mc.EntityDataKeyWidth] != width || m[mc.EntityDataKeyHeight] != height {
		t.Errorf("%s: box = %v by %v, want %v by %v", what, m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight], width, height)
	}
	// BDS sends neither of the data that players lie by.
	if _, ok := m[mc.EntityDataKeyBedPosition]; ok {
		t.Errorf("%s: a villager has a bed position, which BDS does not send", what)
	}
	if m.Flag(mc.EntityDataKeyPlayerFlags, 1) {
		t.Errorf("%s: a villager has the player flag of sleeping, which BDS does not send", what)
	}
}

func vec32(v [3]float32) mgl64.Vec3 { return mgl64.Vec3{float64(v[0]), float64(v[1]), float64(v[2])} }

func near(a, b mgl64.Vec3) bool { return a.Sub(b).Len() < 1e-4 }

// sameAngle reports whether two yaws in degrees turn the same way.
func sameAngle(a, b float64) bool {
	d := math.Mod(a-b, 360)
	return math.Abs(d) < 1e-3 || math.Abs(math.Abs(d)-360) < 1e-3
}
