package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// recipeTestPlayer is an entity with unlocked Java recipes.
type recipeTestPlayer struct {
	world.Entity
	handle   *world.EntityHandle
	unlocked []string
}

func (p *recipeTestPlayer) Close() error            { return nil }
func (p *recipeTestPlayer) H() *world.EntityHandle  { return p.handle }
func (p *recipeTestPlayer) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (p *recipeTestPlayer) Rotation() cube.Rotation { return cube.Rotation{} }

func (p *recipeTestPlayer) UnlockedRecipeIDs() []string { return p.unlocked }

type recipeTestType struct{ p *recipeTestPlayer }

func (t recipeTestType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return t.p
}
func (recipeTestType) EncodeEntity() string                        { return "test:recipes" }
func (recipeTestType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (recipeTestType) DecodeNBT(map[string]any, *world.EntityData) {}
func (recipeTestType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// TestLimitedCraftingLocksRecipes checks crafting as
// RecipeCraftingHolder.setRecipeUsed: with limited_crafting on, a crafting
// recipe needs to be unlocked, and with it off any recipe crafts.
func TestLimitedCraftingLocksRecipes(t *testing.T) {
	windCharge := recipe.NewShapeless([]recipe.Item{item.NewStack(item.BreezeRod{}, 1)}, item.NewStack(item.WindCharge{}, 4), "crafting_table")
	ids := recipe.JavaRecipeIDs(windCharge)
	if len(ids) != 1 || ids[0] != "minecraft:wind_charge" {
		t.Fatalf("wind charge recipe Java IDs = %v, want [minecraft:wind_charge]", ids)
	}
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	s := craftingTestSession()
	player := &recipeTestPlayer{}
	handle := world.EntitySpawnOpts{}.New(recipeTestType{p: player}, abilityTestConfig{})
	player.handle = handle
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(handle)
		s.ent = handle
		if s.recipeLocked(windCharge, tx) {
			t.Fatal("recipe locked with limited_crafting off")
		}
		tx.World().SetGameRule("limited_crafting", true)
		if !s.recipeLocked(windCharge, tx) {
			t.Fatal("locked recipe crafts with limited_crafting on")
		}
		player.unlocked = ids[:1]
		if s.recipeLocked(windCharge, tx) {
			t.Fatal("unlocked recipe locked with limited_crafting on")
		}
	})
}
