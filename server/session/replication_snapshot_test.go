package session_test

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl64"
)

var updateSnapshots = flag.Bool("update-snapshots", false, "rewrite the snapshots of replication in testdata")

// TestReplicationSnapshot replays a scene to a viewer, tick by tick: an
// item dropped two blocks above the ground falls and comes to rest, a
// block is placed, a sound and a particle play, and the item goes. What
// the viewer is sent, packet by packet, is compared with the snapshot in
// testdata, so that a change of what replication sends shows as a change
// of the snapshot, to be looked at rather than slip by. Run with
// -update-snapshots to write it again.
func TestReplicationSnapshot(t *testing.T) {
	conn := &packetConn{packets: make(chan any, 4096)}
	viewer := session.Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 1}.New(conn)
	t.Cleanup(viewer.CloseConnection)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	do := func(f func(tx *world.Tx)) {
		t.Helper()
		if err := w.Do(f).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	names := map[uint64]string{}
	// step writes what the viewer was sent since the last step under a
	// heading, the session marking its end with a message.
	step := func(heading string, keep bool) {
		t.Helper()
		marker := "end of " + heading
		viewer.SendMessage(marker)
		if keep {
			fmt.Fprintf(&out, "== %s\n", heading)
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case raw := <-conn.packets:
				pk := decoded(t, raw.(proto.Packet))
				if text, ok := pk.(*v2193.TextPacket); ok && text.Message == marker {
					return
				}
				if keep {
					out.WriteString(describe(pk, names) + "\n")
				}
			case <-timer.C:
				t.Fatalf("%s: the session did not send its marker", heading)
			}
		}
	}

	var loader *world.Loader
	do(func(tx *world.Tx) {
		for x := 7; x <= 9; x++ {
			for z := 7; z <= 9; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		loader = world.NewLoader(1, w, viewer)
		loader.Move(tx, mgl64.Vec3{8.5, 1, 8.5})
		loader.Load(tx, 9)
	})
	t.Cleanup(func() { w.Do(func(tx *world.Tx) { loader.Close(tx) }) })
	// The chunks are not part of the snapshot.
	step("load", false)

	var drop *world.EntityHandle
	do(func(tx *world.Tx) {
		drop = entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 3, 8.5}}, item.NewStack(item.Apple{}, 1))
		names[drop.RuntimeID()] = "item"
		tx.AddEntity(drop)
	})
	step("spawn", true)
	for tick := 1; tick <= 25; tick++ {
		w.AdvanceTick()
		step(fmt.Sprintf("tick %d", tick), true)
	}
	do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{9, 1, 8}, block.Stone{}, nil) })
	w.AdvanceTick()
	step("place a block", true)
	do(func(tx *world.Tx) {
		tx.PlaySound(mgl64.Vec3{8.5, 1, 8.5}, sound.Click{})
		tx.AddParticle(mgl64.Vec3{8.5, 1, 8.5}, particle.HugeExplosion{})
	})
	step("sound and particle", true)
	do(func(tx *world.Tx) {
		if e, ok := drop.Entity(tx); ok {
			tx.RemoveEntity(e)
		}
	})
	step("remove", true)

	path := filepath.Join("testdata", "replication_item.snapshot")
	if *updateSnapshots {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run with -update-snapshots to write it", err)
	}
	if got := out.String(); got != string(want) {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := range max(len(gotLines), len(wantLines)) {
			var g, w string
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Fatalf("replication differs from %s at line %d:\n got: %s\nwant: %s\n(run with -update-snapshots if the change is meant)", path, i+1, g, w)
			}
		}
	}
}

// decoded returns a packet as a client reads it: encoded, which a packet
// shared by sessions is in a wrapper of the session, then decoded.
func decoded(t *testing.T, pk proto.Packet) proto.Packet {
	t.Helper()
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	back, err := netconn.DecodePacket(w.B)
	if err != nil {
		t.Fatalf("packet %d does not decode: %v", pk.ID(), err)
	}
	return back
}

// describe writes a packet on one line. IDs that differ from run to run are
// named: entities by the names passed, or by the order they first appear
// in, and item stacks not at all.
func describe(pk proto.Packet, names map[uint64]string) string {
	v := reflect.ValueOf(pk).Elem()
	normalise(v, names)
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(v.Type().Name(), "Packet"))
	for i := range v.NumField() {
		fmt.Fprintf(&b, " %s=%+v", v.Type().Field(i).Name, v.Field(i).Interface())
	}
	return b.String()
}

// entityIDFields are the fields that name an entity by its runtime or
// unique ID.
var entityIDFields = map[string]bool{"EntityRuntimeID": true, "EntityUniqueID": true, "RiddenEntityUniqueID": true, "RiderEntityUniqueID": true, "TargetActorID": true}

// normalise sets the IDs in v that differ from run to run to ones that do
// not: an entity ID to the index of its name, a stack network ID to zero.
func normalise(v reflect.Value, names map[uint64]string) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			f, field := v.Type().Field(i), v.Field(i)
			if !field.CanSet() {
				continue
			}
			switch {
			case f.Name == "StackNetworkID" && field.CanInt():
				field.SetInt(0)
			case entityIDFields[f.Name] && (field.CanUint() || field.CanInt()):
				var id uint64
				if field.CanUint() {
					id = field.Uint()
				} else {
					id = uint64(field.Int())
				}
				name, ok := names[id]
				if !ok {
					name = fmt.Sprintf("entity%d", len(names)+1)
					names[id] = name
				}
				index := uint64(0)
				for i, n := range sortedNames(names) {
					if n == name {
						index = uint64(i + 1)
					}
				}
				if field.CanUint() {
					field.SetUint(index)
				} else {
					field.SetInt(int64(index))
				}
			default:
				normalise(field, names)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			normalise(v.Index(i), names)
		}
	case reflect.Pointer:
		if !v.IsNil() {
			normalise(v.Elem(), names)
		}
	}
}

// sortedNames returns the names of entities in order.
func sortedNames(names map[uint64]string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
