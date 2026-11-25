package api_test

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/api"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/nether"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

func TestCustomItemsAndRecipes(t *testing.T) {
	var used, consumed atomic.Int32
	wand := api.NewCustomItem(api.CustomItemSpec{
		ID:           "sdk_test:frost_wand",
		DisplayName:  "Frost Wand",
		Category:     category.Equipment(),
		MaxCount:     1,
		AttackDamage: 7.5,
		Cooldown:     time.Second,
		HandEquipped: true,
		Glinted:      true,
		FireProof:    true,
		OnUse: func(*world.Tx, item.User, *item.UseContext) bool {
			used.Add(1)
			return true
		},
	})
	api.RegisterItem(wand)
	got, ok := api.ItemByName("sdk_test:frost_wand", 0)
	if !ok || got != wand {
		t.Fatalf("ItemByName = (%v, %v), want wand", got, ok)
	}
	if u, ok := wand.(item.Usable); !ok || !u.Use(nil, nil, nil) || used.Load() != 1 {
		t.Fatalf("expected custom item to implement item.Usable and invoke OnUse")
	}
	if w, ok := wand.(item.Weapon); !ok || w.AttackDamage() != 7.5 {
		t.Fatalf("expected Weapon attack damage 7.5")
	}
	if _, ok := wand.(item.Consumable); ok {
		t.Fatal("non-consumable item should not implement item.Consumable")
	}

	elixir := api.NewCustomItem(api.CustomItemSpec{
		ID:               "sdk_test:elixir",
		DisplayName:      "Elixir",
		Drinkable:        true,
		AlwaysConsumable: true,
		OnConsume: func(*world.Tx, item.Consumer) item.Stack {
			consumed.Add(1)
			return item.NewStack(item.GlassBottle{}, 1)
		},
	})
	api.RegisterItem(elixir)
	c, ok := elixir.(item.Consumable)
	if !ok || !c.AlwaysConsumable() {
		t.Fatal("expected elixir to implement item.Consumable")
	}
	ret := c.Consume(nil, nil)
	if consumed.Load() != 1 || ret.Empty() {
		t.Fatalf("Consume returned %v (calls=%d)", ret, consumed.Load())
	}

	// Declarative recipes: shaped, shapeless, smithing, potion, dynamic.
	beforeCount := len(recipe.Recipes())
	shaped := api.RegisterShapedRecipe(api.ShapedRecipeSpec{
		Pattern: []string{" D ", " S ", " S "},
		Key: map[rune]recipe.Item{
			'D': item.NewStack(item.Diamond{}, 1),
			'S': item.NewStack(item.Stick{}, 1),
		},
		Output: item.NewStack(wand, 1),
	})
	if shaped.Shape().Width() != 3 || shaped.Shape().Height() != 3 || len(shaped.Input()) != 9 {
		t.Fatalf("unexpected shaped recipe layout: %+v", shaped.Shape())
	}
	api.RegisterShapelessRecipe(api.ShapelessRecipeSpec{
		Inputs: []recipe.Item{item.NewStack(item.Sugar{}, 1), item.NewStack(item.Apple{}, 1)},
		Output: item.NewStack(elixir, 1),
	})
	api.RegisterSmithingRecipe(api.SmithingRecipeSpec{
		Template: item.NewStack(item.Paper{}, 1),
		Base:     item.NewStack(item.Stick{}, 1),
		Addition: item.NewStack(item.Diamond{}, 1),
		Output:   item.NewStack(wand, 1),
	})
	api.RegisterPotionRecipe(api.PotionRecipeSpec{
		Input:   item.NewStack(item.GlassBottle{}, 1),
		Reagent: item.NewStack(item.Diamond{}, 1),
		Output:  item.NewStack(elixir, 1),
	})
	if len(recipe.Recipes()) != beforeCount+4 {
		t.Fatalf("recipe count grew by %d, want 4", len(recipe.Recipes())-beforeCount)
	}
	dynBefore := len(recipe.DynamicRecipes())
	dyn := api.RegisterDynamicRecipe(api.DynamicRecipeSpec{
		Match: func(in []recipe.Item) ([]item.Stack, bool) {
			return []item.Stack{item.NewStack(wand, 1)}, len(in) == 1
		},
	})
	if len(recipe.DynamicRecipes()) != dynBefore+1 {
		t.Fatal("dynamic recipe not registered")
	}
	if out, ok := dyn.Match([]recipe.Item{item.NewStack(item.Stick{}, 1)}); !ok || len(out) != 1 {
		t.Fatalf("dynamic recipe Match = (%v, %v)", out, ok)
	}
}

type typedCmdRunnable struct {
	Target string `cmd:"target"`
	Count  int    `cmd:"count"`
	onRun  func(target string, count int)
}

func (r typedCmdRunnable) Run(_ cmd.Source, _ *cmd.Output, _ *world.Tx) {
	if r.onRun != nil {
		r.onRun(r.Target, r.Count)
	}
}

func TestTypedCommandAndCommandGroup(t *testing.T) {
	w := newTestWorld(t, nil, world.EntityRegistry{})
	var gotTarget string
	var gotCount int
	typed := api.RegisterTypedCommand("sdktyped", "Typed SDK command", []string{"styp"}, typedCmdRunnable{
		onRun: func(target string, count int) {
			gotTarget, gotCount = target, count
		},
	})
	var buyArgs, listCalled []string
	group := api.RegisterCommandGroup(api.CommandGroupSpec{
		Name:        "sdkshop",
		Description: "Shop command group",
		SubCommands: []api.SubCommandSpec{
			{
				Name: "buy",
				Run: func(_ cmd.Source, args []string, _ *world.Tx) {
					buyArgs = args
				},
			},
			{
				Name: "list",
				Run: func(_ cmd.Source, args []string, _ *world.Tx) {
					listCalled = append(listCalled, "list")
				},
			},
		},
	})

	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "CmdTester", mgl64.Vec3{0, 64, 0}, nil)
		typed.Execute("diamond 5", p, tx)
		group.Execute("buy sword 2", p, tx)
		group.Execute("list", p, tx)
	})
	if gotTarget != "diamond" || gotCount != 5 {
		t.Fatalf("typed command args = (%q, %d), want (diamond, 5)", gotTarget, gotCount)
	}
	if len(buyArgs) != 2 || buyArgs[0] != "sword" || buyArgs[1] != "2" {
		t.Fatalf("subcommand buy args = %v, want [sword 2]", buyArgs)
	}
	if len(listCalled) != 1 {
		t.Fatalf("subcommand list calls = %d, want 1", len(listCalled))
	}
}

func TestVirtualMenusPagedMenuAndCustomForm(t *testing.T) {
	w := newTestWorld(t, nil, world.EntityRegistry{})
	rec := clienttest.New()
	var closedCount atomic.Int32
	shop := api.NewMenu(api.MenuSpec{
		Title:    "Server Shop",
		Kind:     api.MenuDoubleChest,
		ReadOnly: true,
		Slots: map[int]api.MenuSlot{
			0: {Item: item.NewStack(item.Diamond{}, 1), Locked: true},
		},
		OnClose: func(*player.Player, *world.Tx) {
			closedCount.Add(1)
		},
	})
	if shop.Inventory().Size() != 54 || shop.Title() != "Server Shop" || shop.Kind() != api.MenuDoubleChest {
		t.Fatalf("unexpected double chest menu config: size=%d title=%q kind=%v", shop.Inventory().Size(), shop.Title(), shop.Kind())
	}
	shop.SetSlot(1, api.MenuSlot{Item: item.NewStack(item.Emerald{}, 3), Locked: true})
	if it, _ := shop.Inventory().Item(1); it.Count() != 3 {
		t.Fatalf("SetSlot failed: got %v", it)
	}
	shop.ClearSlot(1)
	if it, _ := shop.Inventory().Item(1); !it.Empty() {
		t.Fatalf("ClearSlot failed: got %v", it)
	}

	var pageClicked atomic.Int32
	items := make([]api.PagedMenuItem, 25)
	for i := range items {
		items[i] = api.PagedMenuItem{
			Item: item.NewStack(item.GoldIngot{}, 1),
			OnClick: func(*player.Player, int, item.Stack) {
				pageClicked.Add(1)
			},
		}
	}
	paged := api.NewPagedMenu(api.PagedMenuSpec{Title: "Catalog", Items: items})
	if paged.PageSize() != 18 || paged.PageCount() != 2 {
		t.Fatalf("paged layout = (%d, %d), want (18, 2)", paged.PageSize(), paged.PageCount())
	}

	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "MenuTester", mgl64.Vec3{4, 64, 4}, rec)
		tx.SetBlock(cube.Pos{4, 67, 4}, block.Stone{}, nil)
		shop.Open(p, tx)
		m := p.Menu()
		if m == nil || m.Kind() != menu.Virtual || m.VirtualKind() != menu.VirtualDoubleChest || m.Title() != "Server Shop" {
			t.Fatalf("opened menu = %+v, want VirtualDoubleChest 'Server Shop'", m)
		}
		// Real world block must remain untouched.
		if _, isStone := tx.Block(cube.Pos{4, 67, 4}).(block.Stone); !isStone {
			t.Fatalf("virtual menu mutated real block: %T", tx.Block(cube.Pos{4, 67, 4}))
		}
		p.CloseMenu(false)
		if closedCount.Load() != 1 {
			t.Fatalf("OnClose calls = %d, want 1", closedCount.Load())
		}
		paged.OpenPage(p, 1, tx)
		if pm := p.Menu(); pm == nil || pm.Title() != "Catalog (2/2)" {
			t.Fatalf("paged menu title = %q, want 'Catalog (2/2)'", pm.Title())
		}
		p.CloseMenu(false)
	})

	// CustomForm serialization & submission validation.
	var gotName, gotMode string
	var gotEnabled bool
	var gotScale float64
	var gotStep int
	var formClosed atomic.Int32
	cf := api.NewCustomForm(api.CustomFormSpec{
		Title: "Settings",
		Fields: []api.FormField{
			api.HeaderField("General"),
			api.LabelField("Configure your profile"),
			api.DividerField(),
			api.InputField("name", "Display Name", "Steve", "Enter name"),
			api.ToggleField("enabled", "Notifications", true),
			api.SliderField("scale", "Scale", 1, 10, 0.5, 2.5),
			api.DropdownField("mode", "Mode", []string{"Survival", "Creative"}, 0),
			api.StepSliderField("tier", "Tier", []string{"Low", "Med", "High"}, 1),
		},
		OnSubmit: func(_ *player.Player, v api.FormValues, _ *world.Tx) {
			gotName = v.String("name")
			gotEnabled = v.Bool("enabled")
			gotScale = v.Float("scale")
			gotMode = v.String("mode")
			gotStep = v.Index("tier")
		},
		OnClose: func(*player.Player, *world.Tx) {
			formClosed.Add(1)
		},
	})
	rawJSON, err := json.Marshal(cf)
	if err != nil || len(rawJSON) == 0 {
		t.Fatalf("Marshal CustomForm: %v", err)
	}
	payload := []byte(`[null, null, null, "Alex", false, 6.5, 1, 2]`)
	if err := cf.SubmitJSON(payload, nil, nil); err != nil {
		t.Fatalf("SubmitJSON: %v", err)
	}
	if gotName != "Alex" || gotEnabled || gotScale != 6.5 || gotMode != "Creative" || gotStep != 2 {
		t.Fatalf("CustomForm values = (%q, %v, %v, %q, %d)", gotName, gotEnabled, gotScale, gotMode, gotStep)
	}
	if err := cf.SubmitJSON([]byte(`[null,null,null,"Alex",false,99,1,2]`), nil, nil); err == nil {
		t.Fatal("expected out-of-range slider submission to fail")
	}
	if err := cf.SubmitJSON(nil, nil, nil); err != nil || formClosed.Load() != 1 {
		t.Fatalf("CustomForm close err=%v count=%d", err, formClosed.Load())
	}
}

func TestPlayerUIHelpers(t *testing.T) {
	w := newTestWorld(t, nil, world.EntityRegistry{})
	rec := clienttest.New()
	var clicked atomic.Int32

	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "UITester", mgl64.Vec3{0, 64, 0}, rec)
		api.SendScoreboard(p, api.ScoreboardSpec{Title: "Stats", Lines: []string{"Coins: 100", "Rank: VIP"}})
		api.RemoveScoreboard(p)
		api.SendBossBar(p, api.BossBarSpec{ID: "raid", Text: "Boss", Health: 0.75, Colour: bossbar.Red(), HasColour: true})
		api.RemoveBossBarID(p, "raid")
		api.RemoveBossBar(p)
		api.SendTitle(p, api.TitleSpec{Title: "Welcome", Subtitle: "Arena", Duration: time.Second})
		api.SendActionBar(p, "Action")
		api.SendPopup(p, "Popup")
		api.SendTip(p, "Tip")
		api.SendToast(p, "Notice", "Ready")
		api.PlaySound(p, sound.LevelUp{})
		api.PlaySoundAt(tx, p.Position(), sound.Click{})
		api.PlayCustomSound(p, "custom.ding", 1, 1)
		api.PlayCustomSoundAt(tx, p.Position(), "custom.bell", 1, 1)
		api.SpawnParticle(tx, p.Position(), particle.Flame{})
		d := api.NewDialogue(api.DialogueSpec{
			Title: "Guide",
			Body:  "Choose a path",
			Buttons: []api.DialogueButton{
				{Text: "Start", OnClick: func(*player.Player, *world.Tx) { clicked.Add(1) }},
			},
		})
		api.SendDialogue(p, p, api.DialogueSpec{Title: "Guide", Body: "Hi"})
		if err := d.Submit(0, p, tx); err != nil {
			t.Fatalf("Dialogue Submit: %v", err)
		}
	})
	if clicked.Load() != 1 {
		t.Fatalf("dialogue button clicks = %d, want 1", clicked.Load())
	}
}

func TestCustomEntityUpgradesAndBrain(t *testing.T) {
	var spawned, hurtCalled, died, sensorRuns, fightTicks atomic.Int32
	bossType := api.NewCustomEntityType(api.CustomEntitySpec{
		Name:        "sdk_test:boss",
		NetworkType: "minecraft:zombie",
		MaxHealth:   20,
		Equipment: api.CustomEquipment{
			MainHand: item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1),
			Helmet:   item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1),
		},
		Drops:   []item.Stack{item.NewStack(item.Emerald{}, 3)},
		XPDrops: 10,
		EncodeNBT: func(state any) map[string]any {
			if phase, ok := state.(int32); ok {
				return map[string]any{"phase": phase}
			}
			return nil
		},
		DecodeNBT: func(data map[string]any) any {
			return data["phase"]
		},
		Brain: &api.BrainSpec{
			DefaultActivity: "idle",
			Sensors: []api.BrainSensor{
				{
					Name:          "threat",
					IntervalTicks: 1,
					Sense: func(_ *api.CustomEntity, b *api.EntityBrain, _ *world.Tx) {
						sensorRuns.Add(1)
						b.SetMemoryWithTTL("alert", true, 3)
					},
				},
			},
			ActivitySelector: func(_ *api.CustomEntity, b *api.EntityBrain, _ *world.Tx) string {
				if _, ok := b.Memory("alert"); ok {
					return "fight"
				}
				return "idle"
			},
			Activities: map[string][]api.BrainBehaviour{
				"fight": {
					{
						Name: "strike",
						Tick: func(*api.CustomEntity, *api.EntityBrain, *world.Tx) {
							fightTicks.Add(1)
						},
					},
				},
			},
		},
		OnSpawn: func(*api.CustomEntity, *world.Tx) { spawned.Add(1) },
		OnHurt: func(_ *api.CustomEntity, dmg *float64, _ world.DamageSource, _ *world.Tx) bool {
			hurtCalled.Add(1)
			if *dmg < 2 {
				return false // cancel chip damage
			}
			return true
		},
		OnDeath: func(*api.CustomEntity, world.DamageSource, *world.Tx) { died.Add(1) },
	})
	reg := api.RegisterEntityTypes(entity.DefaultRegistry, bossType)
	w := newTestWorld(t, nil, reg)

	<-w.Exec(func(tx *world.Tx) {
		h := bossType.Spawn(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})
		ent := tx.AddEntity(h).(*api.LivingCustomEntity)
		if spawned.Load() != 1 {
			t.Fatalf("OnSpawn = %d, want 1", spawned.Load())
		}
		mainHand, _ := ent.HeldItems()
		helmet, _, _, _ := ent.EquipmentArmour()
		if mainHand.Empty() || helmet.Empty() {
			t.Fatal("expected CustomEquipment on HeldItems and EquipmentArmour")
		}
		ent.SetCustomState(int32(2))
		enc := bossType.EncodeNBT(&world.EntityData{Data: nil})
		if enc != nil {
			t.Fatal("EncodeNBT with nil data should return nil")
		}
		// Tick brain -> sensor fires, switches activity to "fight", ticks strike behaviour.
		ent.Tick(tx, 1)
		if ent.Brain().Activity() != "fight" || sensorRuns.Load() < 1 || fightTicks.Load() < 1 {
			t.Fatalf("brain state: act=%q sensors=%d fightTicks=%d", ent.Brain().Activity(), sensorRuns.Load(), fightTicks.Load())
		}
		// Cancelled damage via OnHurt.
		if _, ok := ent.Hurt(1, entity.VoidDamageSource{}); ok {
			t.Fatal("expected chip damage to be cancelled by OnHurt")
		}
		// Fatal damage spawns drops + XP orbs + fires OnDeath.
		if _, ok := ent.Hurt(25, entity.VoidDamageSource{}); !ok || !ent.Dead() {
			t.Fatal("expected fatal damage to kill boss")
		}
		if hurtCalled.Load() != 2 || died.Load() != 1 {
			t.Fatalf("OnHurt=%d OnDeath=%d, want 2 and 1", hurtCalled.Load(), died.Load())
		}
		foundDrop := false
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok && it.H().Type() == entity.ItemType {
				foundDrop = true
			}
		}
		if !foundDrop {
			t.Fatal("expected death drop item entity to spawn")
		}
	})

	// Round-trip EncodeNBT/DecodeNBT on EntityData directly.
	data := &world.EntityData{}
	bossType.DecodeNBT(map[string]any{"phase": int32(3)}, data)
	encoded := bossType.EncodeNBT(data)
	if encoded["phase"] != int32(3) {
		t.Fatalf("EncodeNBT round-trip = %#v, want phase=3", encoded)
	}
}

func TestNetherEndHooksCustomBiomeAndStructures(t *testing.T) {
	// Custom Biome registration.
	b := api.NewCustomBiome(api.CustomBiomeSpec{
		ID:          249,
		Name:        "sdk_test:crystal_caverns",
		Temperature: 0.4,
		Rainfall:    0.2,
		WaterColour: color.RGBA{R: 0x10, G: 0xaa, B: 0xff, A: 0xff},
		Tags:        []string{"underground", "crystal"},
	})
	api.RegisterBiome(b)
	if got, ok := api.BiomeByName("sdk_test:crystal_caverns"); !ok || got.EncodeBiome() != 249 {
		t.Fatalf("BiomeByName = (%v, %v)", got, ok)
	}
	if got, ok := api.BiomeByID(249); !ok || got.String() != "sdk_test:crystal_caverns" {
		t.Fatalf("BiomeByID = (%v, %v)", got, ok)
	}

	// Nether & End hooks + StructureTemplate placement.
	pillar := api.NewStructure("sdk_test:pillar",
		api.StructureBlock{Offset: cube.Pos{0, 0, 0}, Block: block.Gold{}},
		api.StructureBlock{Offset: cube.Pos{0, 1, 0}, Block: block.Diamond{}},
		api.StructureBlock{Offset: cube.Pos{1, 0, 0}, Block: block.Emerald{}},
	)
	goldRID := api.BlockRuntimeID(block.Gold{})
	emeraldRID := api.BlockRuntimeID(block.Emerald{})

	ng := nether.New(42)
	var netherTerrain, netherDecorate atomic.Int32
	api.AttachNetherHooks(ng, api.WorldGenHooks{
		OnTerrain: func(pos world.ChunkPos, c *chunk.Chunk) {
			netherTerrain.Add(1)
		},
		OnDecorate: func(pos world.ChunkPos, c *chunk.Chunk, rng api.WorldGenRandom) {
			netherDecorate.Add(1)
			_ = rng.NextInt(10)
			api.PlaceStructureInChunk(pos, c, cube.Pos{int(pos[0])<<4 + 2, 60, int(pos[1])<<4 + 2}, pillar, api.StructurePlaceOpts{})
		},
	})
	nc := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 127})
	ng.GenerateChunk(world.ChunkPos{0, 0}, nc, world.GenerationContext{})
	if netherTerrain.Load() != 1 || netherDecorate.Load() != 1 {
		t.Fatalf("nether hooks = (%d, %d), want (1, 1)", netherTerrain.Load(), netherDecorate.Load())
	}
	if nc.Block(2, 60, 2, 0) != goldRID || nc.Block(3, 60, 2, 0) != emeraldRID {
		t.Fatal("PlaceStructureInChunk did not stamp blocks into Nether chunk")
	}
	api.ClearNetherHooks(ng)

	eg := end.New(42)
	var endDecorate atomic.Int32
	api.AttachEndHooks(eg, api.WorldGenHooks{
		OnDecorate: func(pos world.ChunkPos, c *chunk.Chunk, rng api.WorldGenRandom) {
			endDecorate.Add(1)
			api.PlaceStructureInChunk(pos, c, cube.Pos{int(pos[0])<<4 + 4, 70, int(pos[1])<<4 + 4}, pillar, api.StructurePlaceOpts{Rotation: api.Rotate90})
		},
	})
	ec := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	eg.GenerateChunk(world.ChunkPos{0, 0}, ec, world.GenerationContext{})
	if endDecorate.Load() != 1 || ec.Block(4, 70, 4, 0) != goldRID || ec.Block(4, 70, 5, 0) != emeraldRID {
		t.Fatalf("End hook/rotated structure failed: dec=%d gold=%v emerald=%v",
			endDecorate.Load(), ec.Block(4, 70, 4, 0) == goldRID, ec.Block(4, 70, 5, 0) == emeraldRID)
	}
	api.ClearEndHooks(eg)

	// Live world transaction placement.
	w := newTestWorld(t, nil, world.EntityRegistry{})
	<-w.Exec(func(tx *world.Tx) {
		n := api.PlaceStructure(tx, cube.Pos{10, 64, 10}, pillar, api.StructurePlaceOpts{IgnoreAir: true})
		if n != 3 {
			t.Fatalf("PlaceStructure placed %d blocks, want 3", n)
		}
		if _, ok := tx.Block(cube.Pos{10, 65, 10}).(block.Diamond); !ok {
			t.Fatalf("expected Diamond at (10,65,10), got %T", tx.Block(cube.Pos{10, 65, 10}))
		}
	})
}

func TestSchedulerStoragePermissionsHologramWorldsAndEconomy(t *testing.T) {
	// 1. Scheduler
	srv, app := newTestApp(t)
	srv.Listen()
	w := newTestWorld(t, nil, world.EntityRegistry{})
	var onceCount, repeatCount atomic.Int32
	onceTask := app.Scheduler().After(w, 2, func(*world.Tx) { onceCount.Add(1) })
	repTask := app.Scheduler().Every(w, 2, func(*world.Tx) { repeatCount.Add(1) })
	for range 6 {
		w.AdvanceTick()
	}
	if onceCount.Load() != 1 || !onceTask.Cancelled() {
		t.Fatalf("After task count=%d cancelled=%v, want 1 true", onceCount.Load(), onceTask.Cancelled())
	}
	if repeatCount.Load() != 3 {
		t.Fatalf("Every task count=%d, want 3", repeatCount.Load())
	}
	repTask.Cancel()
	w.AdvanceTick()
	w.AdvanceTick()
	if repeatCount.Load() != 3 {
		t.Fatalf("cancelled Every task fired again: %d", repeatCount.Load())
	}

	// 2. Storage (JSON + TOML + path traversal protection)
	app.SetDataDir(t.TempDir())
	st := app.Storage("myplugin")
	type cfg struct {
		Motd    string `json:"motd" toml:"motd"`
		MaxPets int    `json:"max_pets" toml:"max_pets"`
	}
	loadedJSON, err := api.LoadOrDefaultJSON(st, "config.json", cfg{Motd: "hi", MaxPets: 4})
	if err != nil || loadedJSON.Motd != "hi" || loadedJSON.MaxPets != 4 {
		t.Fatalf("LoadOrDefaultJSON: %+v %v", loadedJSON, err)
	}
	loadedTOML, err := api.LoadOrDefaultTOML(st, "settings.toml", cfg{Motd: "toml", MaxPets: 7})
	if err != nil || loadedTOML.Motd != "toml" || loadedTOML.MaxPets != 7 {
		t.Fatalf("LoadOrDefaultTOML: %+v %v", loadedTOML, err)
	}
	if _, err := st.Path("../escape.txt"); err == nil {
		t.Fatal("expected path traversal rejection")
	}

	// 3. Permissions & RBAC
	perms := app.Permissions()
	perms.GrantGroup("default", "core.chat")
	perms.GrantGroup("vip", "shop.*")
	perms.AddGroupParent("vip", "default")
	perms.GrantGroup("vip", "-shop.admin")
	perms.AddPlayerGroup("user-1", "vip")
	if !perms.Has("user-1", "core.chat") || !perms.Has("user-1", "shop.buy") || perms.Has("user-1", "shop.admin") {
		t.Fatal("hierarchical permission evaluation failed")
	}
	permFile := filepath.Join(t.TempDir(), "perms.json")
	if err := perms.SaveFile(permFile); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	perms2 := api.NewPermissionStore(nil)
	if err := perms2.LoadFile(permFile); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !perms2.Has("user-1", "shop.buy") || perms2.Has("user-1", "shop.admin") {
		t.Fatal("reloaded permissions mismatch")
	}

	// 4. Holograms
	<-w.Exec(func(tx *world.Tx) {
		holo := api.SpawnHologram(tx, mgl64.Vec3{2, 65, 2}, "Line 1", "Line 2")
		if len(holo.Handles()) != 2 || len(holo.Lines()) != 2 {
			t.Fatalf("hologram lines/handles = %d/%d, want 2/2", len(holo.Lines()), len(holo.Handles()))
		}
		holo.SetLines(tx, "Only Line")
		if len(holo.Handles()) != 1 {
			t.Fatalf("after shrink handles = %d, want 1", len(holo.Handles()))
		}
		holo.Move(tx, mgl64.Vec3{5, 70, 5})
		if holo.Position() != (mgl64.Vec3{5, 70, 5}) {
			t.Fatalf("Move position = %v", holo.Position())
		}
		holo.Close(tx)
		if len(holo.Handles()) != 0 {
			t.Fatal("Close did not clear hologram handles")
		}
	})

	// 5. Dynamic Worlds & TransferPlayer
	arena, err := app.CreateWorld(api.WorldSpec{
		Name:            "arena-1",
		Synchronous:     true,
		RandomTickSpeed: -1,
	})
	if err != nil {
		t.Fatalf("CreateWorld: %v", err)
	}
	if got, ok := app.WorldByName("arena-1"); !ok || got != arena {
		t.Fatalf("WorldByName(arena-1) = (%v, %v)", got, ok)
	}
	var pHandle *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "Traveler", mgl64.Vec3{0, 64, 0}, nil)
		pHandle = p.H()
		if !api.TransferPlayer(p, arena, mgl64.Vec3{10, 70, 10}, tx) {
			t.Fatal("TransferPlayer returned false")
		}
	})
	<-arena.Exec(func(tx *world.Tx) {
		ent, ok := pHandle.Entity(tx)
		if !ok {
			t.Fatal("transferred player not found in arena world")
		}
		if ent.Position() != (mgl64.Vec3{10, 70, 10}) {
			t.Fatalf("transferred player pos = %v, want (10,70,10)", ent.Position())
		}
	})
	if err := app.UnloadWorld("arena-1"); err != nil {
		t.Fatalf("UnloadWorld: %v", err)
	}

	// 6. Economy (including concurrency under -race)
	eco := app.Economy()
	eco.SetDefaultBalance(100)
	var changes atomic.Int32
	eco.OnChange(func(string, int64, int64) { changes.Add(1) })
	eco.Deposit("alice", 50)
	if !eco.Transfer("alice", "bob", 75) || eco.Balance("alice") != 75 || eco.Balance("bob") != 175 {
		t.Fatalf("economy transfer mismatch: alice=%d bob=%d", eco.Balance("alice"), eco.Balance("bob"))
	}
	if eco.Withdraw("alice", 999) {
		t.Fatal("expected insufficient funds withdraw to fail")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				eco.Deposit("concurrent", 1)
			}
		}()
	}
	wg.Wait()
	if eco.Balance("concurrent") != 500 {
		t.Fatalf("concurrent balance = %d, want 500", eco.Balance("concurrent"))
	}
	top := eco.TopBalances(2)
	if len(top) != 2 || top[0].Account != "concurrent" {
		t.Fatalf("TopBalances = %+v", top)
	}
	ecoFile := filepath.Join(t.TempDir(), "eco.json")
	if err := eco.SaveFile(ecoFile); err != nil {
		t.Fatalf("economy SaveFile: %v", err)
	}
	if _, err := os.Stat(ecoFile); err != nil {
		t.Fatalf("economy file missing: %v", err)
	}
	eco2 := api.NewEconomy("gold", 0)
	if err := eco2.LoadFile(ecoFile); err != nil || eco2.Balance("bob") != 175 {
		t.Fatalf("economy LoadFile: err=%v bob=%d", err, eco2.Balance("bob"))
	}
}
