package block_test

import (
	"io"
	"log/slog"
	"testing"

	sdkblock "github.com/df-mc/dragonfly/sdk/block"
	coreblock "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCustomBlockDefineRegisterAndWorldOps(t *testing.T) {
	var placed, broken, interacted bool
	b := sdkblock.Define("sdk_test:glow_tile",
		sdkblock.FullCube(),
		sdkblock.Light(12),
		sdkblock.LightFilter(2),
		sdkblock.Hardness(1.5),
		sdkblock.Drops(item.NewStack(item.Diamond{}, 2)),
		sdkblock.XP(1, 3),
		sdkblock.OnPlace(func(cube.Pos, *world.Tx, world.Block) { placed = true }),
		sdkblock.OnBreak(func(cube.Pos, *world.Tx, item.User) { broken = true }),
		sdkblock.OnInteract(func(cube.Pos, cube.Face, *world.Tx, item.User, *item.UseContext) bool {
			interacted = true
			return true
		}),
	)

	name, _ := b.EncodeBlock()
	if name != "sdk_test:glow_tile" {
		t.Fatalf("EncodeBlock = %q", name)
	}

	reg := world.NewBlockRegistry()
	sdkblock.RegisterIn(reg, b)
	reg.Finalize()

	rid := sdkblock.RuntimeIDIn(reg, b)

	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          reg,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{2, 64, 2}
		sdkblock.SetRID(tx, pos, rid, nil)
		if got := tx.Block(pos); got != b {
			t.Fatalf("SetRID block = %T, want custom block", got)
		}
		sdkblock.FillRID(tx, cube.Pos{0, 64, 0}, cube.Pos{1, 64, 1}, rid)
		if tx.Block(cube.Pos{1, 64, 1}) != b {
			t.Fatal("FillRID did not populate corner")
		}
		if pl, ok := b.(interface {
			OnPlace(cube.Pos, *world.Tx, world.Block)
		}); ok {
			pl.OnPlace(pos, tx, nil)
		}
		if br, ok := b.(coreblock.Breakable); ok {
			info := br.BreakInfo()
			if info.Hardness != 1.5 || info.XPDrops != (coreblock.XPDropRange{1, 3}) {
				t.Fatalf("BreakInfo = %+v", info)
			}
			if info.BreakHandler != nil {
				info.BreakHandler(pos, tx, nil)
			}
		}
		if ac, ok := b.(interface {
			Activate(cube.Pos, cube.Face, *world.Tx, item.User, *item.UseContext) bool
		}); ok {
			ac.Activate(pos, cube.FaceUp, tx, nil, &item.UseContext{})
		}
	})

	if !placed || !broken || !interacted {
		t.Fatalf("callbacks: placed=%v broken=%v interacted=%v", placed, broken, interacted)
	}
}
