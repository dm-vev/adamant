package client

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// TestBDSActorsProbe joins the BDS at BDS_ADDR as a 2225 client, runs the
// steps of the file BDS_STEPS (see testdata/bds_actors) and logs to the file
// BDS_OUT the actor packets BDS sends: AddActor, SetActorData, MobEquipment,
// ActorEvent, Animate and, with BDS_MOVES, the moves of actors; BDS_ALL adds
// the names of the other packets. Run it with scripts/bds-run.sh, with BDS_RUN
// set to a folder of your own when other runs share the machine.
//
// A line of BDS_STEPS is a command, run as the player; "wait N" waits N
// seconds; "hit TYPE" attacks every actor of TYPE seen so far; "use TYPE
// ITEM" selects ITEM in the hotbar and interacts with every actor of TYPE;
// "mark TEXT" writes a marker into the log. Interactions with actors were
// ignored by BDS in every try, so "use" and "hit" record nothing of use yet.
func TestBDSActorsProbe(t *testing.T) {
	addr := os.Getenv("BDS_ADDR")
	if addr == "" {
		t.Skip("BDS_ADDR is not set")
	}
	steps, err := os.ReadFile(os.Getenv("BDS_STEPS"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(os.Getenv("BDS_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	var c *Conn
	for try := 0; try < 8; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		c, err = Dial(ctx, addr, Config{Identity: login.Identity{DisplayName: "Probe"}, Protocol: codec.V2225})
		cancel()
		if err == nil {
			break
		}
		t.Logf("dial: %v", err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var mu sync.Mutex
	pos := c.game.PlayerPosition
	actors := map[uint64]string{}
	slots := map[uint32]mc.ItemInstance{}
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	}
	wheat := int32(0)
	for _, it := range c.Items() {
		if it.Name == "minecraft:wheat" {
			wheat = int32(it.RuntimeID)
		}
	}
	logf("start game position %v, wheat id %d", c.game.PlayerPosition, wheat)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				logf("read: %v", err)
				return
			}
			switch p := pk.(type) {
			case *mc.AddActorPacket:
				mu.Lock()
				actors[p.EntityRuntimeID] = p.EntityType
				mu.Unlock()
				logf("AddActor %s rid=%d uid=%d pos=%v vel=%v pitch=%v yaw=%v head=%v body=%v attrs=%d links=%v\n    meta=%s props=%+v", p.EntityType, p.EntityRuntimeID, p.EntityUniqueID,
					p.Position, p.Velocity, p.Pitch, p.Yaw, p.HeadYaw, p.BodyYaw, len(p.Attributes), p.EntityLinks, metadata(p.EntityMetadata), p.EntityProperties)
			case *mc.SetActorDataPacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				if ty != "" {
					logf("SetActorData %s rid=%d tick=%d\n    meta=%s", ty, p.EntityRuntimeID, p.Tick, metadata(p.EntityMetadata))
				}
			case *mc.MobEquipmentPacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				logf("MobEquipment %s rid=%d item=%+v slot=%d hotbar=%d window=%d", ty, p.EntityRuntimeID, p.NewItem, p.InventorySlot, p.HotBarSlot, p.WindowID)
			case *mc.ActorEventPacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				logf("ActorEvent %s rid=%d event=%d data=%d", ty, p.EntityRuntimeID, p.EventType, p.EventData)
			case *mc.MoveActorAbsolutePacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				if ty != "" && os.Getenv("BDS_MOVES") != "" {
					logf("MoveActorAbsolute %s rid=%d pos=%v rot=%v flags=%d", ty, p.EntityRuntimeID, p.Position, p.Rotation, p.Flags)
				}
			case *mc.MoveActorDeltaPacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				if ty != "" && os.Getenv("BDS_MOVES") != "" {
					logf("MoveActorDelta %s %+v", ty, p)
				}
			case *mc.RemoveActorPacket:
				logf("RemoveActor uid=%d", p.EntityUniqueID)
			case *mc.InventorySlotPacket:
				if p.WindowID == 0 {
					mu.Lock()
					slots[p.Slot] = p.NewItem
					mu.Unlock()
					logf("slot %d: %+v", p.Slot, p.NewItem.Stack)
				}
			case *mc.InventoryContentPacket:
				if p.WindowID == 0 {
					for i, it := range p.Content {
						mu.Lock()
						slots[uint32(i)] = it
						mu.Unlock()
					}
				}
			case *mc.MovePlayerPacket:
				if p.EntityRuntimeID == c.game.EntityRuntimeID {
					mu.Lock()
					pos = p.Position
					mu.Unlock()
					logf("MovePlayer self %v mode=%d", p.Position, p.Mode)
				}
			case *mc.CommandOutputPacket:
				logf("CommandOutput ok=%d %+v", p.SuccessCount, p.OutputMessages)
			default:
				if os.Getenv("BDS_ALL") != "" {
					switch pk.(type) {
					case *mc.LevelChunkPacket, *mc.SubChunkPacket, *mc.NetworkChunkPublisherUpdatePacket, *mc.UpdateAttributesPacket, *mc.SetTimePacket:
					default:
						logf("pkt %T %+v", pk, pk)
					}
				}
			case *mc.AnimatePacket:
				mu.Lock()
				ty := actors[p.EntityRuntimeID]
				mu.Unlock()
				logf("Animate %s rid=%d %+v", ty, p.EntityRuntimeID, p)
			case *mc.TextPacket:
				logf("Text %+v", p)
			}
		}
	}()

	// Without this BDS treats the player as one still loading, and ignores
	// what it does to actors.
	_ = c.WritePacket(&mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: c.game.EntityRuntimeID})
	_ = c.Flush()
	var tick uint64
	step := func(d time.Duration) {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			tick++
			mu.Lock()
			here := pos
			mu.Unlock()
			_ = c.WritePacket(&mc.PlayerAuthInputPacket{Position: here, Pitch: 15, InputMode: 1, Tick: tick})
			if c.Flush() != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	step(2 * time.Second)
	for _, line := range strings.Split(string(steps), "\n") {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "wait "):
			var n float64
			fmt.Sscan(strings.TrimPrefix(line, "wait "), &n)
			step(time.Duration(n * float64(time.Second)))
		case strings.HasPrefix(line, "mark "):
			logf("---- %s", strings.TrimPrefix(line, "mark "))
		case strings.HasPrefix(line, "hit "):
			ty := "minecraft:" + strings.TrimPrefix(line, "hit ")
			mu.Lock()
			var ids []uint64
			for rid, t := range actors {
				if t == ty {
					ids = append(ids, rid)
				}
			}
			mu.Unlock()
			for _, rid := range ids {
				logf("attack %s rid=%d", ty, rid)
				_ = c.WritePacket(&mc.InventoryTransactionPacket{TransactionData: &mc.UseItemOnEntityTransactionData{
					TargetEntityRuntimeID: rid,
					ActionType:            mc.UseItemOnEntityActionAttack,
					Position:              pos,
					ClickedPosition:       [3]float32{0, 0.7, 0},
				}})
				_ = c.Flush()
				step(500 * time.Millisecond)
			}
		case strings.HasPrefix(line, "use "):
			f := strings.Fields(line)
			ty, name := "minecraft:"+f[1], "minecraft:"+f[2]
			var id int32
			for _, it := range c.Items() {
				if it.Name == name {
					id = int32(it.RuntimeID)
				}
			}
			mu.Lock()
			var held mc.ItemInstance
			var heldSlot uint32 = 99
			for i, it := range slots {
				if it.Stack.NetworkID == id && id != 0 && i < heldSlot {
					held, heldSlot = it, i
				}
			}
			var ids []uint64
			for rid, t := range actors {
				if t == ty {
					ids = append(ids, rid)
				}
			}
			mu.Unlock()
			logf("using %s from slot %d: %+v", name, heldSlot, held.Stack)
			_ = c.WritePacket(&mc.MobEquipmentPacket{EntityRuntimeID: c.game.EntityRuntimeID, NewItem: held, InventorySlot: uint8(heldSlot), HotBarSlot: uint8(heldSlot)})
			_ = c.Flush()
			step(300 * time.Millisecond)
			for _, rid := range ids {
				logf("interact with %s rid=%d", ty, rid)
				_ = c.WritePacket(&mc.InteractPacket{ActionType: mc.InteractActionMouseOverEntity, TargetEntityRuntimeID: rid, Position: proto.Some([3]float32{})})
				_ = c.WritePacket(&mc.InventoryTransactionPacket{TransactionData: &mc.UseItemOnEntityTransactionData{
					TargetEntityRuntimeID: rid,
					ActionType:            mc.UseItemOnEntityActionInteract,
					HotBarSlot:            int32(heldSlot),
					HeldItem:              held,
					Position:              pos,
					ClickedPosition:       [3]float32{0, 0.7, 0},
				}})
				_ = c.Flush()
				step(500 * time.Millisecond)
			}
		default:
			logf("run %s", line)
			_ = c.WritePacket(&mc.CommandRequestPacket{
				CommandLine:   "/" + line,
				CommandOrigin: mc.CommandOrigin{PlayerUniqueID: c.game.EntityUniqueID},
				Version:       "latest",
			})
			_ = c.Flush()
			step(700 * time.Millisecond)
		}
	}
	step(time.Second)
	_ = c.nn.SetReadDeadline(time.Now())
	<-done
}

// metadata formats metadata with its keys in order and flags decoded.
func metadata(m mc.Metadata) string {
	var keys []uint32
	for k := range m {
		keys = append(keys, k)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var b strings.Builder
	for _, k := range keys {
		v := m[k]
		if k == mc.EntityDataKeyFlags || k == mc.EntityDataKeyFlagsTwo {
			n, _ := v.(int64)
			var set []int
			for i := 0; i < 64; i++ {
				if n&(1<<i) != 0 {
					set = append(set, i)
				}
			}
			base := 0
			if k == mc.EntityDataKeyFlagsTwo {
				base = 64
			}
			for i := range set {
				set[i] += base
			}
			fmt.Fprintf(&b, "[%d flags=%v] ", k, set)
			continue
		}
		fmt.Fprintf(&b, "[%d %v] ", k, v)
	}
	return b.String()
}
