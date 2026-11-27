package builtin

import (
	"testing"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTagCommandsForMobAndPlayer(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	tagCmd := cmd.New("tag", "", nil, tagAddCommand{}, tagRemoveCommand{}, tagListCommand{})
	job := w.Do(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 65, 0.5}
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos})).(*entity.Cow)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "TagPlayer", Position: pos, GameMode: world.GameModeSurvival})).(*player.Player)
		for _, tc := range []struct {
			sel    string
			target tagTarget
		}{
			{"@e[type=cow,limit=1]", cow},
			{"TagPlayer", p},
		} {
			out := runTestCommand(tagCmd, tc.sel+" add tracked", tx)
			if out.ErrorCount() != 0 {
				t.Fatalf("tag add failed: %v", out.Errors())
			}
			list := runTestCommand(tagCmd, tc.sel+" list", tx)
			if list.MessageCount() != 1 || tc.target.CommandTags()[0] != "tracked" {
				t.Fatal("tag list did not report the added tag")
			}
			runTestCommand(tagCmd, tc.sel+" remove tracked", tx)
			if got := tc.target.CommandTags(); len(got) != 0 {
				t.Fatalf("tags after removal = %v", got)
			}
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestAttributeBaseSetMaxHealthUpdatesHealthCap(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	attrCmd := cmd.New("attribute", "", nil,
		attributeGetCommand{}, attributeBaseGetCommand{}, attributeBaseSetCommand{},
		attributeBaseResetCommand{}, attributeModifierAddCommand{}, attributeModifierRemoveCommand{},
		attributeModifierValueCommand{},
	)
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}.New(entity.CowType, entity.CowConfig{Health: 8, MaxHealth: 10})).(*entity.Cow)
		out := runTestCommand(attrCmd, "@e[type=cow,limit=1] max_health base set 30", tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("attribute base set failed: %v", out.Errors())
		}
		if got := cow.MaxHealth(); got != 30 {
			t.Fatalf("max health = %g, want 30", got)
		}
		if got := cow.Health(); got != 8 {
			t.Fatalf("health = %g after raising cap, want 8", got)
		}
		if !setAttributeBase(cow, "minecraft:max_health", 5) {
			t.Fatal("setting namespaced max_health base failed")
		}
		if got := cow.MaxHealth(); got != 5 {
			t.Fatalf("max health after lowering cap = %g, want 5", got)
		}
		if got := cow.Health(); got != 5 {
			t.Fatalf("health after lowering cap = %g, want 5", got)
		}
		add := runTestCommand(attrCmd, "@e[type=cow,limit=1] max_health modifier add test:boost 0.25 add_multiplied_total", tx)
		if add.ErrorCount() != 0 {
			t.Fatalf("modifier add failed: %v", add.Errors())
		}
		if got, ok := modifierValue(cow, "max_health", "test:boost"); !ok || got != .25 {
			t.Fatalf("modifier value = %g/%v, want .25/true", got, ok)
		}
		remove := runTestCommand(attrCmd, "@e[type=cow,limit=1] max_health modifier remove test:boost", tx)
		if remove.ErrorCount() != 0 {
			t.Fatalf("modifier remove failed: %v", remove.Errors())
		}
	})
}
