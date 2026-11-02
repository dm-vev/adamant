package player

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Exercise the actual player's empty-hand path and session background sender.
// The independent session corpus regression covers both flushing modes and
// every raw native frame; this check ensures the input path invokes correction.
func TestPlayerRejectedIronUseMatchesNativePackets(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_barrier_use.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Feed  string
				Initial, Top bool
				RequestPhase int `json:"request_phase"`
			}
			Base, Target cube.Pos
			Effects      []struct {
				Update *packet.UpdateBlock `json:"block_update"`
			} `json:"effect_packets"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 136 {
		t.Fatal("incomplete native player-use controls")
	}
	cases := 0
	for _, c := range native.Scenarios {
		if c.Scenario.Feed != "none" || c.Scenario.Initial || c.Scenario.RequestPhase != 0 || (c.Scenario.Block != "iron_door" && c.Scenario.Block != "iron_trapdoor") {
			continue
		}
		cases++
		t.Run(c.Scenario.Block+map[bool]string{false: "/lower", true: "/upper"}[c.Scenario.Top], func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			w := world.Config{Log: log, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			conn := newPacketPanicConn()
			sess := session.Config{Log: log, MaxChunkRadius: 1, HandleStop: func(*world.Tx, session.Controllable) {}}.New(conn)
			t.Cleanup(sess.CloseConnection)
			feet := c.Base.Vec3().Add(mgl64.Vec3{.5, 0, 2.5})
			cfg := Config{Session: sess, Position: feet, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName, GameMode: world.GameModeCreative}
			h := world.EntitySpawnOpts{Position: feet, ID: cfg.UUID}.New(Type, cfg)
			sess.SetHandle(h, cfg.Skin)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.Do(func(tx *world.Tx) {
				tx.SetBlock(c.Base.Side(cube.FaceDown), block.Stone{}, nil)
				if c.Scenario.Block == "iron_door" {
					tx.SetBlock(c.Base, block.IronDoor{Facing: cube.West}, nil)
					tx.SetBlock(c.Base.Side(cube.FaceUp), block.IronDoor{Facing: cube.West, Top: true}, nil)
				} else {
					tx.SetBlock(c.Base, block.IronTrapdoor{Facing: cube.East, Top: c.Scenario.Top}, nil)
				}
				p := tx.AddEntity(h).(*Player)
				sess.Spawn(p, tx)
			}).Wait(ctx); err != nil {
				t.Fatal(err)
			}
			conn.writesMu.Lock()
			start := len(conn.writes)
			conn.writesMu.Unlock()
			if err := h.Do(func(tx *world.Tx, e world.Entity) {
				e.(*Player).UseItemOnBlock(c.Target, cube.FaceSouth, mgl64.Vec3{.5, .5, .5})
			}).Wait(ctx); err != nil {
				t.Fatal(err)
			}
			count := 1
			if c.Scenario.Block == "iron_door" {
				count = 2
			}
			var want []*packet.UpdateBlock
			for _, p := range c.Effects[:count] {
				if p.Update == nil {
					t.Fatal("native rejected-use control emitted a sound")
				}
				want = append(want, p.Update)
			}
			for {
				var got []*packet.UpdateBlock
				conn.writesMu.Lock()
				for _, p := range conn.writes[start:] {
					if update, ok := p.(*packet.UpdateBlock); ok && update.Layer == 0 && int(update.Position[0]) == c.Base[0] && int(update.Position[2]) == c.Base[2] && (int(update.Position[1]) == c.Base[1] || count == 2 && int(update.Position[1]) == c.Base[1]+1) {
						got = append(got, update)
					}
				}
				conn.writesMu.Unlock()
				if len(got) >= count {
					g, _ := json.Marshal(got)
					v, _ := json.Marshal(want)
					if !bytes.Equal(g, v) {
						t.Fatalf("player rejection packets=%s, native=%s", g, v)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("actual player did not publish native rejection corrections")
				case <-time.After(5 * time.Millisecond):
				}
			}
		})
	}
	if cases != 4 {
		t.Fatal("native iron input-height controls missing")
	}
}
