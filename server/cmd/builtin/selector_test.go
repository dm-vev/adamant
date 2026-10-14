package builtin

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSelectorJavaOptions checks the EntitySelectorOptions that filter on
// Java state: Java entity type IDs and tags, rotation, scores, team and nbt.
func TestSelectorJavaOptions(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		spawn := func(id string, pos mgl64.Vec3, yaw float64) world.Entity {
			opts := world.EntitySpawnOpts{Position: pos, Rotation: cube.Rotation{yaw, 0}}
			return tx.AddEntity(tx.World().EntityRegistry().Config().SpawnMob(id, opts))
		}
		cow := spawn("minecraft:cow", mgl64.Vec3{0.5, 64, 0.5}, 90)
		zombie := spawn("minecraft:zombie", mgl64.Vec3{3.5, 70, 0.5}, -90)
		piglin := spawn("minecraft:zombified_piglin", mgl64.Vec3{6.5, 70, 0.5}, 180)
		boat := tx.AddEntity(entity.NewBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{9.5, 70, 0.5}}, 1))

		board := tx.World().Scoreboard()
		if err := board.AddObjective("kills", "kills", scoreboard.Dummy); err != nil {
			t.Fatal(err)
		}
		_ = board.SetScore("kills", cmd.ScoreHolderName(cow), 5)
		_ = board.SetScore("kills", cmd.ScoreHolderName(zombie), 1)
		if err := board.AddTeam("red", "red"); err != nil {
			t.Fatal(err)
		}
		if err := board.JoinTeam("red", cmd.ScoreHolderName(cow)); err != nil {
			t.Fatal(err)
		}

		src := consoleTestSource{pos: mgl64.Vec3{0.5, 64, 0.5}, out: &cmd.Output{}}
		check := func(selector string, want ...world.Entity) {
			t.Helper()
			targets, err := cmd.ResolveTargets(selector, src, tx)
			if err != nil {
				t.Fatalf("%s: %v", selector, err)
			}
			var got []world.Entity
			for _, target := range targets {
				got = append(got, target.(world.Entity))
			}
			if len(got) != len(want) {
				t.Fatalf("%s selected %d entities, want %d", selector, len(got), len(want))
			}
			for _, e := range want {
				if !slices.ContainsFunc(got, func(g world.Entity) bool { return g.H() == e.H() }) {
					t.Fatalf("%s did not select %s", selector, e.H().Type().EncodeEntity())
				}
			}
		}
		// Java type IDs match entities whose Bedrock actor ID differs.
		check("@e[type=zombified_piglin]", piglin)
		check("@e[type=minecraft:zombie_pigman]", piglin)
		check("@e[type=spruce_boat]", boat)
		check("@e[type=#minecraft:undead]", zombie, piglin)
		check("@e[type=!#undead,type=!#boat]", cow)
		// y_rotation wraps around when its wrapped minimum exceeds the maximum.
		check("@e[y_rotation=45..135]", cow)
		check("@e[y_rotation=170..-80]", zombie, piglin)
		check("@e[scores={kills=3..}]", cow)
		check("@e[scores={kills=..5}]", cow, zombie)
		check("@e[scores={missing=0..}]")
		check("@e[team=red]", cow)
		check("@e[team=!red]", zombie, piglin, boat)
		check("@e[team=]", zombie, piglin, boat)
		check("@e[team=!]", cow)
		check("@e[nbt={Pos:[64.0f]}]", cow)
		check("@e[nbt=!{Pos:[70.0f]}]", cow)
		// NBT types must match exactly, as Java compares tag classes.
		check("@e[nbt={Pos:[64.0d]}]")
		check("@e[distance=..1]", cow)

		for _, invalid := range []string{"@e[distance=5..1]", "@e[distance=-1..]", "@e[level=1.5]", "@e[limit=!1]", "@e[scores={kills=5..1}]", "@e[distance=..]", "@e[nbt={Pos:]"} {
			if _, err := cmd.ResolveTargets(invalid, src, tx); err == nil {
				t.Fatalf("%s parsed, want an error", invalid)
			}
		}
	})
}
