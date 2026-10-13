package player

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"golang.org/x/text/language"
)

func TestDeathMessageMatchesBedrockKeys(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	job := w.Do(func(tx *world.Tx) {
		killer := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 0}}.New(Type, Config{Name: "Killer"})).(*Player)
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 0}}))
		arrow := tx.AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{3, 64, 0}}, killer))
		burning, inFire := cube.Pos{0, 64, 0}, cube.Pos{5, 64, 0}
		tx.SetBlock(inFire, block.Fire{}, nil)

		cases := []struct {
			name   string
			pos    cube.Pos
			src    world.DamageSource
			key    string
			params []string
		}{
			{"player melee", burning, entity.AttackDamageSource{Attacker: killer}, "death.attack.player", []string{"Victim", "Killer"}},
			{"mob melee uses entity name key", burning, entity.AttackDamageSource{Attacker: cow}, "death.attack.mob", []string{"Victim", "%entity.cow.name"}},
			{"arrow names the shooter", burning, entity.ProjectileDamageSource{Projectile: arrow, Owner: killer}, "death.attack.arrow", []string{"Victim", "Killer"}},
			{"ownerless projectile", burning, entity.ProjectileDamageSource{Projectile: arrow}, "death.attack.generic", []string{"Victim"}},
			{"thorns", burning, enchantment.ThornsDamageSource{Owner: killer}, "death.attack.thorns", []string{"Victim", "Killer"}},
			{"standing in fire", inFire, block.FireDamageSource{}, "death.attack.inFire", []string{"Victim"}},
			{"burning away from fire", burning, block.FireDamageSource{}, "death.attack.onFire", []string{"Victim"}},
			{"magma", burning, block.MagmaDamageSource{}, "death.attack.magma", []string{"Victim"}},
			{"starvation", burning, StarvationDamageSource{}, "death.attack.starve", []string{"Victim"}},
			{"ownerless explosion", burning, entity.ExplosionDamageSource{Source: world.BlockExplosionSource{}}, "death.attack.explosion", []string{"Victim"}},
			{"explosion caused by player", burning, entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: killer}}, "death.attack.explosion.player", []string{"Victim", "Killer"}},
		}
		for _, c := range cases {
			tr, args := deathMessage("Victim", c.pos, c.src, tx)
			got := tr.F(args...)
			// Translations are sent with chat.Translate's leading colour reset.
			if key := strings.TrimPrefix(got.Resolve(language.English), "§r"); key != "%"+c.key {
				t.Errorf("%s: key %q, want %q", c.name, key, "%"+c.key)
			}
			if params := got.Params(language.English); !slices.Equal(params, c.params) {
				t.Errorf("%s: params %v, want %v", c.name, params, c.params)
			}
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}
