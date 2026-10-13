package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTagCommandsForMobAndPlayer(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	job := w.Do(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 65, 0.5}
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos})).(*entity.Cow)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos, GameMode: world.GameModeSurvival})).(*player.Player)
		for _, target := range []cmd.Target{cow, p} {
			out := &cmd.Output{}
			(tagAddCommand{Targets: []cmd.Target{target}, Name: "tracked"}).Run(nil, out, tx)
			if out.ErrorCount() != 0 {
				t.Fatalf("tag add failed: %v", out.Errors())
			}
			list := &cmd.Output{}
			(tagListCommand{Targets: []cmd.Target{target}}).Run(nil, list, tx)
			if list.MessageCount() != 1 || target.(tagTarget).CommandTags()[0] != "tracked" {
				t.Fatal("tag list did not report the added tag")
			}
			(tagRemoveCommand{Targets: []cmd.Target{target}, Name: "tracked"}).Run(nil, &cmd.Output{}, tx)
			if got := target.(tagTarget).CommandTags(); len(got) != 0 {
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
	data := &world.EntityData{Data: (entity.CowConfig{Health: 8, MaxHealth: 10}).New()}
	target := entity.CowType.Open(nil, nil, data)
	cow := target.(*entity.Cow)
	out := &cmd.Output{}
	(attributeBaseSetCommand{Target: []cmd.Target{cow}, Attribute: "max_health", Value: 30}).Run(nil, out, nil)
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
	add := &cmd.Output{}
	(attributeModifierAddCommand{Target: []cmd.Target{cow}, Attribute: "max_health", ID: "test:boost", Amount: .25, Operation: "add_multiplied_total"}).Run(nil, add, nil)
	if add.ErrorCount() != 0 {
		t.Fatalf("modifier add failed: %v", add.Errors())
	}
	if got, ok := modifierValue(cow, "max_health", "test:boost"); !ok || got != .25 {
		t.Fatalf("modifier value = %g/%v, want .25/true", got, ok)
	}
	remove := &cmd.Output{}
	(attributeModifierRemoveCommand{Target: []cmd.Target{cow}, Attribute: "max_health", ID: "test:boost"}).Run(nil, remove, nil)
	if remove.ErrorCount() != 0 {
		t.Fatalf("modifier remove failed: %v", remove.Errors())
	}
}
