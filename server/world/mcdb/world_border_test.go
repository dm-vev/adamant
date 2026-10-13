package mcdb

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWorldBorderPersistsTransitionAndDimensionIsolation(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	overworld := world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	nether := world.Config{Provider: db, Dim: world.Nether, Synchronous: true, RandomTickSpeed: -1}.New()
	saved := world.BorderSettings{CenterX: 12.5, CenterZ: -3.25, DamagePerBlock: .2, SafeZone: 5, WarningBlocks: 5, WarningTime: 15, Size: 40, LerpTime: 20, LerpTarget: 8}
	overworld.SetBorder(saved)
	nether.SetBorderCenter(-100, 200)
	nether.SetBorderSize(6, 0)
	overworld.AdvanceTick()
	overworld.AdvanceTick()
	saved.Size, saved.LerpTime = 36.8, 18
	if got := overworld.Border(); got != saved {
		t.Fatalf("before save=%+v Source two-tick settings=%+v", got, saved)
	}
	if err = nether.Close(); err != nil {
		t.Fatal(err)
	}
	if err = overworld.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := db.LoadWorldData("world-border:0")
	if err != nil {
		t.Fatal(err)
	}
	var stored world.BorderSettings
	if err = json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored != saved {
		t.Fatalf("native MCDB border=%+v Source saved payload=%+v", stored, saved)
	}
	overworld = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	nether = world.Config{Provider: db, Dim: world.Nether, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = nether.Close(); _ = overworld.Close() })
	if got := overworld.Border(); got.CenterX != saved.CenterX || got.CenterZ != saved.CenterZ || got.LerpTime != 17 || got.LerpTarget != 8 || got.DamagePerBlock != .2 || got.SafeZone != 5 || got.WarningBlocks != 5 || got.WarningTime != 15 {
		t.Fatalf("restored transition after startup tick=%+v", got)
	}
	if got := nether.Border(); got.CenterX != -100 || got.CenterZ != 200 || got.Size != 6 || got.LerpTime != 0 {
		t.Fatalf("Nether border leaked shared level.dat settings=%+v", got)
	}
	candidate := cube.Pos{28, 64, -3}
	if !overworld.WithinBorder(candidate) {
		t.Fatal("restored moving border rejected initially inside candidate")
	}
	for range 17 {
		overworld.AdvanceTick()
	}
	if overworld.WithinBorder(candidate) {
		t.Fatal("persisted shrinking border never excluded candidate")
	}
	if got := overworld.Border(); got.Size != 8 || got.LerpTime != 0 {
		t.Fatalf("persisted transition did not finish=%+v", got)
	}
	if !nether.WithinBorder(cube.Pos{-100, 64, 200}) || nether.WithinBorder(cube.Pos{12, 64, -3}) {
		t.Fatal("Overworld ticks/mutations changed independent Nether border")
	}
	t.Log("actual MCDB reopen preserved Source border payload; transition resumed and completed; dimensions retained independent bounds")
}
