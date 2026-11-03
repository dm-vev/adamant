package player

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/text"
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
			// console is the fallback the console is shown.
			console string
		}{
			{"player melee", burning, entity.AttackDamageSource{Attacker: killer}, "death.attack.player", []string{"Victim", "Killer"}, "§rVictim was slain by Killer"},
			{"mob melee uses entity name key", burning, entity.AttackDamageSource{Attacker: cow}, "death.attack.mob", []string{"Victim", "%entity.cow.name"}, "§rVictim was slain by Cow"},
			{"arrow names the shooter", burning, entity.ProjectileDamageSource{Projectile: arrow, Owner: killer}, "death.attack.arrow", []string{"Victim", "Killer"}, "§rVictim was shot by Killer"},
			{"ownerless projectile", burning, entity.ProjectileDamageSource{Projectile: arrow}, "death.attack.generic", []string{"Victim"}, "§rVictim died"},
			{"thorns", burning, enchantment.ThornsDamageSource{Owner: killer}, "death.attack.thorns", []string{"Victim", "Killer"}, "§rVictim was killed trying to hurt Killer"},
			{"standing in fire", inFire, block.FireDamageSource{}, "death.attack.inFire", []string{"Victim"}, "§rVictim went up in flames"},
			{"burning away from fire", burning, block.FireDamageSource{}, "death.attack.onFire", []string{"Victim"}, "§rVictim burned to death"},
			{"magma", burning, block.MagmaDamageSource{}, "death.attack.magma", []string{"Victim"}, "§rVictim discovered the floor was lava"},
			{"starvation", burning, StarvationDamageSource{}, "death.attack.starve", []string{"Victim"}, "§rVictim starved to death"},
			{"ownerless explosion", burning, entity.ExplosionDamageSource{Source: world.BlockExplosionSource{}}, "death.attack.explosion", []string{"Victim"}, "§rVictim blew up"},
			{"explosion caused by player", burning, entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: killer}}, "death.attack.explosion.player", []string{"Victim", "Killer"}, "§rVictim was blown up by Killer"},
		}
		for _, c := range cases {
			tr, args := deathMessage("Victim", c.pos, c.src, tx)
			got := tr.F(args...)
			key, params, ok := translationOf(got.Text(language.English))
			if !ok || key != c.key {
				t.Errorf("%s: key %q, want %q", c.name, key, c.key)
			}
			if !slices.Equal(params, c.params) {
				t.Errorf("%s: params %v, want %v", c.name, params, c.params)
			}
			if console := got.String(); console != c.console {
				t.Errorf("%s: console %q, want %q", c.name, console, c.console)
			}
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

// translationOf returns the key of the first translation in t and its
// arguments as a client is sent them: a translation as its key after a %,
// other text as its plain text.
func translationOf(t text.Text) (key string, args []string, ok bool) {
	if key, _, ok := t.Translation(); ok {
		for a := range t.Args() {
			if k, _, ok := a.Translation(); ok {
				args = append(args, "%"+k)
				continue
			}
			args = append(args, a.Plain())
		}
		return key, args, true
	}
	for c := range t.Children() {
		if key, args, ok := translationOf(c); ok {
			return key, args, true
		}
	}
	return "", nil, false
}

func TestMobNameInWords(t *testing.T) {
	for id, want := range map[string]string{"cow": "Cow", "cave_spider": "Cave Spider", "zombie_villager_v2": "Zombie Villager V2", "a__b": "A  B", "": ""} {
		if got := mobName(id); !text.Equal(got, text.Translate("entity."+id+".name", want)) {
			t.Errorf("the name of %q is %#v, want the fallback %q", id, got, want)
		}
	}
}
