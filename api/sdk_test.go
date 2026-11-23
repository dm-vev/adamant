package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/api"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var (
	customPlaced, customBroken, customInteracted atomic.Int32
	customTicked, customInside, customStepped    atomic.Int32
	customArcaneBox                              = cube.Box(0, 0, 0, 1, 0.5, 1)

	arcaneTestBlock = api.NewCustomBlock(api.CustomBlockSpec{
		Name:          "sdk_test:arcane_slab",
		CollisionKind: world.CollisionDynamic,
		CustomBoxes:   []cube.BBox{customArcaneBox},
		LightEmission: 11,
		LightFilter:   3,
		RandomTicks:   true,
		Hardness:      0,
		Drops:         []item.Stack{item.NewStack(item.Diamond{}, 2)},
		OnPlace:       func(cube.Pos, *world.Tx, world.Block) { customPlaced.Add(1) },
		OnBreak:       func(cube.Pos, *world.Tx, item.User) { customBroken.Add(1) },
		OnInteract: func(cube.Pos, cube.Face, *world.Tx, item.User, *item.UseContext) bool {
			customInteracted.Add(1)
			return true
		},
		OnRandomTick:   func(cube.Pos, *world.Tx, *rand.Rand) { customTicked.Add(1) },
		OnEntityInside: func(cube.Pos, *world.Tx, world.Entity) { customInside.Add(1) },
		OnEntityStep:   func(cube.Pos, *world.Tx, world.Entity) { customStepped.Add(1) },
	})
	ghostTestBlock = api.NewCustomBlock(api.CustomBlockSpec{
		Name:          "sdk_test:ghost_block",
		CollisionKind: world.CollisionEmpty,
		Unbreakable:   true,
		Replaceable:   true,
	})
	vaultTestBlock = api.NewCustomBlock(api.CustomBlockSpec{
		Name:          "sdk_test:solid_vault",
		CollisionKind: world.CollisionFullCube,
	})
)

func init() {
	api.RegisterBlock(arcaneTestBlock)
	api.RegisterBlock(ghostTestBlock)
	api.RegisterBlock(vaultTestBlock)
	world.DefaultBlockRegistry.Finalize()
}

func newTestWorld(t *testing.T, blocks world.BlockRegistry, entities world.EntityRegistry) *world.World {
	t.Helper()
	if blocks == nil {
		blocks = world.DefaultBlockRegistry
		blocks.Finalize()
	}
	if len(entities.Types()) == 0 {
		entities = entity.DefaultRegistry
	}
	w := world.Config{
		Log:             discardLogger(),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          blocks,
		Entities:        entities,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func spawnTestPlayer(t *testing.T, tx *world.Tx, name string, pos mgl64.Vec3, rec *clienttest.Recorder) *player.Player {
	t.Helper()
	if rec == nil {
		rec = clienttest.New()
	}
	id := uuid.New()
	rec.Answers.Profile = client.Profile{}
	cfg := player.Config{
		Client:      rec,
		UUID:        id,
		Name:        name,
		Position:    pos,
		GameMode:    world.GameModeSurvival,
		Permissions: func(*player.Player) client.Permissions { return client.MemberPermissions() },
		Skin:        skin.New(64, 64),
	}
	h := world.EntitySpawnOpts{Position: pos, ID: id}.New(player.Type, cfg)
	return tx.AddEntity(h).(*player.Player)
}

func newTestApp(t *testing.T, listeners ...func(server.Config, server.Host) (server.Listener, error)) (*server.Server, *api.App) {
	t.Helper()
	srv := server.Config{
		Log:             discardLogger(),
		Name:            "SDK Integration Server",
		PlayerProvider:  player.NopProvider{},
		WorldProvider:   world.NopProvider{},
		Generator:       func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		RandomTickSpeed: -1,
		Listeners:       listeners,
	}.New()
	app := api.New(srv)
	t.Cleanup(func() {
		app.Disable()
		_ = srv.Close()
	})
	return srv, app
}

func TestPluginLifecycleAndAppFacade(t *testing.T) {
	listener := clienttest.NewListener()
	srv, app := newTestApp(t, func(server.Config, server.Host) (server.Listener, error) {
		return listener, nil
	})
	if app.Server() != srv || app.Events() != srv.Events() {
		t.Fatal("App facade accessor mismatch")
	}
	if app.World() == nil || app.Nether() == nil || app.End() == nil {
		t.Fatal("expected overworld, nether, and end worlds to be non-nil")
	}

	var order []string
	p1 := api.FuncPlugin{
		OnEnable:  func(*api.App) { order = append(order, "enable:1") },
		OnDisable: func(*api.App) { order = append(order, "disable:1") },
	}
	p2 := api.FuncPlugin{
		OnEnable:  func(*api.App) { order = append(order, "enable:2") },
		OnDisable: func(*api.App) { order = append(order, "disable:2") },
	}
	app.Use(p1, nil, p2)
	if got := len(app.Plugins()); got != 2 || !slices.Equal(order, []string{"enable:1", "enable:2"}) {
		t.Fatalf("Use() plugins=%d order=%v", got, order)
	}

	srv.Listen()
	acceptDone := make(chan *player.Player, 1)
	go func() {
		for p := range srv.Accept() {
			acceptDone <- p
		}
	}()

	listener.Send(&clienttest.Join{
		L: client.Login{
			Identity: client.Identity{Name: "SdkPlayer", UUID: uuid.New(), XUID: "1001"},
			Skin:     skin.New(64, 64),
		},
	})
	var joined *player.Player
	select {
	case joined = <-acceptDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server Accept()")
	}

	online := app.Online()
	if len(online) != 1 || online[0].Name != "SdkPlayer" {
		t.Fatalf("Online() = %+v, want SdkPlayer", online)
	}
	var names, txNames []string
	for p := range app.Players() {
		names = append(names, p.Name())
	}
	app.ExecuteWorld(app.World(), func(tx *world.Tx) {
		for p := range app.PlayersIn(tx) {
			txNames = append(txNames, p.Name())
		}
	})
	if !slices.Equal(names, []string{"SdkPlayer"}) || !slices.Equal(txNames, []string{"SdkPlayer"}) || joined == nil {
		t.Fatalf("Players=%v PlayersIn=%v", names, txNames)
	}

	app.Disable()
	app.Disable() // Idempotent on repeat disable.
	if want := []string{"enable:1", "enable:2", "disable:2", "disable:1"}; !slices.Equal(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
}

type customTag struct {
	Label string
	Level int
}

type bagHolder struct{ bag api.ComponentBag }

func (b *bagHolder) Components() *api.ComponentBag { return &b.bag }

func TestECSComponentsAndPersistence(t *testing.T) {
	t.Parallel()

	tagKey := api.NewComponent[customTag](
		"sdk_test:tag",
		api.WithComponentCodec(
			func(v customTag) map[string]any {
				return map[string]any{"label": v.Label, "level": int32(v.Level)}
			},
			func(m map[string]any) (customTag, bool) {
				lbl, ok1 := m["label"].(string)
				lvl, ok2 := m["level"].(int32)
				return customTag{Label: lbl, Level: int(lvl)}, ok1 && ok2
			},
		),
	)
	ephemeralKey := api.NewComponent[int]("sdk_test:ephemeral")

	w := newTestWorld(t, nil, entity.DefaultRegistry)
	holder := &bagHolder{}
	api.Set(holder, tagKey, customTag{Label: "guild", Level: 7})
	api.Update(holder, tagKey, func(v *customTag) { v.Level += 3 })
	if got, ok := api.Get(holder, tagKey); !ok || got != (customTag{Label: "guild", Level: 10}) {
		t.Fatalf("ComponentHolder Get = (%+v, %v)", got, ok)
	}

	api.Set(w, tagKey, customTag{Label: "overworld", Level: 1})
	if !api.Has(w, tagKey) || api.WorldComponents(w) == nil {
		t.Fatal("world component missing")
	}

	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "EcsPlayer", mgl64.Vec3{0.5, 64, 0.5}, nil)
		cowH := world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, 0.5}}.New(entity.CowType, entity.CowConfig{})
		cow := tx.AddEntity(cowH).(*entity.Cow)

		api.Set(p, tagKey, customTag{Label: "paladin", Level: 4})
		api.Set(cow.Ent, tagKey, customTag{Label: "prize_cow", Level: 99})
		api.Set(cow.Ent, ephemeralKey, 42)
		api.Set(cowH, ephemeralKey, 77)

		if got, ok := api.Get(p, tagKey); !ok || got.Label != "paladin" {
			t.Fatalf("player Get = (%+v, %v)", got, ok)
		}
		if api.PlayerComponents(p) == nil || api.EntityComponents(cow) == nil || api.HandleComponents(cowH) == nil {
			t.Fatal("component bag helper returned nil")
		}
		if got, ok := api.Get(cowH, ephemeralKey); !ok || got != 77 {
			t.Fatalf("EntityHandle Get = (%v, %v), want 77", got, ok)
		}
		api.Delete(cowH, ephemeralKey)
		if api.Has(cowH, ephemeralKey) {
			t.Fatal("EntityHandle component remained after Delete")
		}

		// EncodeNBT + DecodeNBT persistence on the entity component bag.
		nbtMap := api.EntityComponents(cow).EncodeNBT()

		var restored api.ComponentBag
		restored.DecodeNBT(nbtMap)
		if got, ok := api.GetBag(&restored, tagKey); !ok || got != (customTag{Label: "prize_cow", Level: 99}) {
			t.Fatalf("restored persisted component = (%+v, %v)", got, ok)
		}
		if api.HasBag(&restored, ephemeralKey) {
			t.Fatal("ephemeral component should not persist across EncodeNBT/DecodeNBT")
		}

		// Chunk & Column components (force chunk load at {0, 0}).
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Stone{}, nil)
		chunkBag := api.ChunkComponents(tx, world.ChunkPos{0, 0})
		if chunkBag == nil {
			t.Fatal("ChunkComponents(0,0) = nil after SetBlock")
		}
		api.SetBag(chunkBag, tagKey, customTag{Label: "claim", Level: 2})
		api.UpdateBag(chunkBag, tagKey, func(v *customTag) { v.Level++ })
		if got, ok := api.GetBag(chunkBag, tagKey); !ok || got.Level != 3 {
			t.Fatalf("chunk bag GetBag = (%+v, %v)", got, ok)
		}
		if api.ColumnComponents(nil) != nil {
			t.Fatal("ColumnComponents(nil) != nil")
		}
		api.DeleteBag(chunkBag, tagKey)
		if api.HasBag(chunkBag, tagKey) {
			t.Fatal("DeleteBag failed on chunk bag")
		}
	})
}

type sampleEvent struct {
	api.Cancellable
	Trace []string
}

func TestPrioritizedEventsAndRealWorldPlayerEvents(t *testing.T) {
	bus := api.NewEventBus()
	var seenCancelled bool
	subLowest := api.On[sampleEvent](bus, api.PriorityLowest, func(ev *sampleEvent) {
		ev.Trace = append(ev.Trace, "lowest")
	})
	api.On[sampleEvent](bus, api.PriorityNormal, func(ev *sampleEvent) {
		ev.Trace = append(ev.Trace, "normal")
		ev.Cancel()
	})
	api.On[sampleEvent](bus, api.PriorityMonitor, func(ev *sampleEvent) {
		ev.Trace = append(ev.Trace, "monitor")
		seenCancelled = ev.Cancelled()
	})

	if !api.HasListeners[sampleEvent](bus) {
		t.Fatal("HasListeners should be true")
	}
	ev := &sampleEvent{}
	if ok := api.Emit(bus, ev); ok {
		t.Fatal("Emit should return false when cancelled")
	}
	if !seenCancelled || !slices.Equal(ev.Trace, []string{"lowest", "normal", "monitor"}) {
		t.Fatalf("unexpected priority trace=%v seenCancelled=%v", ev.Trace, seenCancelled)
	}

	subLowest.Unsubscribe()
	ev2 := &sampleEvent{}
	api.Emit(bus, ev2)
	if slices.Contains(ev2.Trace, "lowest") {
		t.Fatalf("unsubscribed listener still ran: %v", ev2.Trace)
	}

	srv := server.Config{
		Log:             discardLogger(),
		PlayerProvider:  player.NopProvider{},
		WorldProvider:   world.NopProvider{},
		Generator:       func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		RandomTickSpeed: -1,
	}.New()
	srv.Listen()
	app := api.New(srv)
	t.Cleanup(func() { _ = srv.Close() })

	var serverEventHits int
	api.OnServer[sampleEvent](app, api.PriorityNormal, func(e *sampleEvent) { serverEventHits++ })
	api.Emit(app.Events(), &sampleEvent{})
	if serverEventHits != 1 {
		t.Fatalf("serverEventHits = %d, want 1", serverEventHits)
	}

	var worldBlockEvents []cube.Pos
	api.OnWorld[world.BlockChangeEvent](app.World(), api.PriorityHigh, func(ev *world.BlockChangeEvent) {
		worldBlockEvents = append(worldBlockEvents, ev.Pos)
		if ev.Pos == (cube.Pos{1, 64, 0}) {
			ev.Cancel()
		}
	})

	var chatIntercepted string
	var chatCancelled bool
	rec := clienttest.New()
	app.ExecuteWorld(app.World(), func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "EventPlayer", mgl64.Vec3{0.5, 64, 0.5}, rec)
		api.OnPlayer[player.ChatEvent](p, api.PriorityNormal, func(ev *player.ChatEvent) {
			chatIntercepted = ev.Message
			if ev.Message == "blocked" {
				ev.Cancel()
				chatCancelled = true
			}
		})

		tx.SetBlock(cube.Pos{0, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil) // Cancelled by OnWorld.

		if _, ok := tx.Block(cube.Pos{0, 64, 0}).(block.Stone); !ok {
			t.Fatal("allowed BlockChangeEvent did not place Stone")
		}
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Air); !ok {
			t.Fatal("cancelled BlockChangeEvent still placed Stone")
		}

		p.Chat("blocked")
	})

	if len(worldBlockEvents) != 2 || chatIntercepted != "blocked" || !chatCancelled {
		t.Fatalf("events mismatch: blockEvents=%v chat=(%q, %v)", worldBlockEvents, chatIntercepted, chatCancelled)
	}
}

func TestTickSystemsAndFillRID(t *testing.T) {
	t.Parallel()

	w := newTestWorld(t, nil, entity.DefaultRegistry)
	var phases []string
	rmPre := api.AddSystem(w, api.PhasePreEntities, func(_ *world.Tx, tick int64) {
		phases = append(phases, "pre")
	})
	api.AddSystem(w, api.PhasePostEntities, func(_ *world.Tx, tick int64) {
		phases = append(phases, "postEntities")
	})
	api.AddSystem(w, api.PhasePostTick, func(_ *world.Tx, tick int64) {
		phases = append(phases, "postTick")
	})

	w.AdvanceTick()
	if want := []string{"pre", "postEntities", "postTick"}; !slices.Equal(phases, want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	phases = nil
	rmPre()
	w.AdvanceTick()
	if want := []string{"postEntities", "postTick"}; !slices.Equal(phases, want) {
		t.Fatalf("phases after remove = %v, want %v", phases, want)
	}

	ridStone := api.BlockRuntimeID(block.Stone{})
	if ridStone == 0 {
		t.Fatal("BlockRuntimeID(Stone) returned 0")
	}

	<-w.Exec(func(tx *world.Tx) {
		singlePos := cube.Pos{-2, 64, -2}
		api.SetBlockRID(tx, singlePos, ridStone, nil)
		if _, ok := tx.Block(singlePos).(block.Stone); !ok {
			t.Fatalf("SetBlockRID left %T, want Stone", tx.Block(singlePos))
		}

		// Inverted bounds normalize automatically in FillRID.
		n := api.FillRID(tx, cube.Pos{3, 66, 2}, cube.Pos{1, 64, 0}, ridStone)
		if n != 27 {
			t.Fatalf("FillRID wrote %d blocks, want 27", n)
		}
		for x := 1; x <= 3; x++ {
			for y := 64; y <= 66; y++ {
				for z := 0; z <= 2; z++ {
					if _, ok := tx.Block(cube.Pos{x, y, z}).(block.Stone); !ok {
						t.Fatalf("block at (%d,%d,%d) = %T, want Stone", x, y, z, tx.Block(cube.Pos{x, y, z}))
					}
				}
			}
		}
		if _, ok := tx.Block(cube.Pos{4, 65, 1}).(block.Air); !ok {
			t.Fatal("FillRID leaked outside bounding box")
		}
	})
}

func TestCustomBlocksSpecAndCallbacks(t *testing.T) {
	arcane := arcaneTestBlock
	ghost := ghostTestBlock
	vault := vaultTestBlock
	customPlaced.Store(0)
	customBroken.Store(0)
	customInteracted.Store(0)
	customTicked.Store(0)
	customInside.Store(0)
	customStepped.Store(0)

	// Comparable & hashable check.
	m := map[world.Block]string{arcane: "arcane", ghost: "ghost", vault: "vault"}
	if len(m) != 3 {
		t.Fatalf("custom block map len = %d, want 3", len(m))
	}
	if api.BlockRuntimeID(arcane) == 0 {
		t.Fatal("BlockRuntimeID returned 0 for custom block on DefaultBlockRegistry")
	}

	isolated := api.NewCustomBlock(api.CustomBlockSpec{
		Name:          "sdk_test:isolated_block",
		CollisionKind: world.CollisionFullCube,
	})
	reg := world.NewBlockRegistry()
	api.RegisterBlockIn(reg, isolated)
	reg.Finalize()
	if api.BlockRuntimeIDIn(reg, isolated) == 0 {
		t.Fatal("BlockRuntimeIDIn returned 0 for registered isolated custom block")
	}

	staticArcane := arcane.(world.StaticCollisionModel)
	boxes := arcane.Model().BBox(cube.Pos{}, nil)
	if staticArcane.StaticCollisionKind() != world.CollisionDynamic || !slices.Equal(boxes, []cube.BBox{customArcaneBox}) {
		t.Fatalf("unexpected static collision on arcane block: %v %v", staticArcane.StaticCollisionKind(), boxes)
	}
	if modelStatic, ok := arcane.Model().(world.StaticCollisionModel); !ok || modelStatic.StaticCollisionKind() != world.CollisionDynamic {
		t.Fatalf("arcane.Model() StaticCollisionModel = (%v, %v), want CollisionDynamic", modelStatic, ok)
	}
	if ghost.Model().(world.StaticCollisionModel).StaticCollisionKind() != world.CollisionEmpty {
		t.Fatal("ghost block model should report CollisionEmpty")
	}
	if vault.Model().(world.StaticCollisionModel).StaticCollisionKind() != world.CollisionFullCube {
		t.Fatal("vault block model should report CollisionFullCube")
	}
	if _, ok := ghost.(block.Breakable); ok {
		t.Fatal("unbreakable block must not implement block.Breakable")
	}
	if brk, ok := arcane.(block.Breakable); !ok || len(brk.BreakInfo().Drops(item.ToolNone{}, nil)) != 1 {
		t.Fatal("arcane block Breakable/Drops mismatch")
	}
	if _, ok := arcane.(world.RandomTicker); !ok {
		t.Fatal("RandomTicks=true block must implement world.RandomTicker")
	}

	w := newTestWorld(t, nil, entity.DefaultRegistry)
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{2, 64, 2}
		tx.SetBlock(pos, arcane, nil)
		tx.SetBlock(cube.Pos{2, 63, 2}, vault, nil)
		p := spawnTestPlayer(t, tx, "BlockTester", mgl64.Vec3{2.5, 65, 2.5}, nil)
		p.UseItemOnBlock(pos, cube.FaceUp, mgl64.Vec3{0.5, 0.5, 0.5})
		arcane.(world.RandomTicker).RandomTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		arcane.(block.EntityInsider).EntityInside(pos, tx, p)
		arcane.(block.EntityStepper).EntityStepOn(pos, tx, p)
		p.BreakBlock(pos)
		if _, isAir := tx.Block(pos).(block.Air); !isAir {
			t.Fatalf("expected Air after BreakBlock, got %T", tx.Block(pos))
		}
	})

	if customPlaced.Load() < 1 || customInteracted.Load() != 1 || customTicked.Load() != 1 || customInside.Load() != 1 || customStepped.Load() != 1 || customBroken.Load() != 1 {
		t.Fatalf("callbacks placed=%d interact=%d tick=%d inside=%d step=%d broken=%d",
			customPlaced.Load(), customInteracted.Load(), customTicked.Load(), customInside.Load(), customStepped.Load(), customBroken.Load())
	}
}

func exerciseLivingSentinel(t *testing.T, tx *world.Tx, spec, markerSpec api.CustomEntitySpec) {
	t.Helper()
	var patch world.ActorMetaPatch
	spawned := api.SpawnCustomEntity(tx, spec, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})
	spawned.ContributeMetadata(&patch)
	if patch.NameTag == nil || *patch.NameTag != "Guardian" || patch.Scale == nil || *patch.Scale != 1.25 {
		t.Fatalf("OnMeta patch mismatch: %+v", patch)
	}

	rawEnt, _ := spawned.H().Entity(tx)
	living, ok := rawEnt.(*api.LivingCustomEntity)
	if !ok {
		t.Fatalf("expected *LivingCustomEntity, got %T", rawEnt)
	}
	if living.Health() != 30 || living.MaxHealth() != 30 {
		t.Fatalf("living health = %v/%v, want 30/30", living.Health(), living.MaxHealth())
	}
	living.Hurt(6, entity.GenericDamageSource{})
	living.Heal(3, entity.FoodHealingSource{})
	if living.Health() != 27 {
		t.Fatalf("health after Hurt/Heal = %v, want 27", living.Health())
	}

	marker := api.SpawnCustomEntity(tx, markerSpec, world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 0.5}})
	markerEnt, _ := marker.H().Entity(tx)
	if _, isLiving := markerEnt.(entity.Living); isLiving {
		t.Fatal("MaxHealth==0 entity must not implement entity.Living")
	}

	p := spawnTestPlayer(t, tx, "Attacker", mgl64.Vec3{0.5, 64, 2.0}, nil)
	living.Interact(tx, p, &item.UseContext{})
	p.AttackEntity(living)
}

func TestCustomEntitiesGoalsAndMeta(t *testing.T) {
	t.Parallel()

	var ticked, started, goalTicked, stopped, interacted, attacked atomic.Int32
	var canContinue atomic.Bool
	canContinue.Store(true)

	sentinelSpec := api.CustomEntitySpec{
		Name:        "sdk_test:sentinel",
		NetworkType: "minecraft:zombie",
		MaxHealth:   30,
		Speed:       0.3,
		EnableAI:    true,
		Goals: []api.GoalEntry{{
			Priority: 1,
			Goal: api.NewGoal(api.GoalSpec{
				Flags:       entity.GoalMove,
				CanStart:    func(*entity.Mob) bool { return true },
				Start:       func(*entity.Mob) { started.Add(1) },
				CanContinue: func(*entity.Mob) bool { return canContinue.Load() },
				Tick:        func(*entity.Mob) { goalTicked.Add(1) },
				Stop:        func(*entity.Mob) { stopped.Add(1) },
			}),
		}},
		OnTick: func(*api.CustomEntity, *world.Tx) { ticked.Add(1) },
		OnInteract: func(*api.CustomEntity, *world.Tx, item.User, *item.UseContext) bool {
			interacted.Add(1)
			return true
		},
		OnAttack: func(*api.CustomEntity, world.Entity, *world.Tx) { attacked.Add(1) },
		OnMeta: func(_ world.Entity, patch *world.ActorMetaPatch) {
			patch.SetNameTag("Guardian")
			patch.SetScale(1.25)
		},
	}
	markerSpec := api.CustomEntitySpec{Name: "sdk_test:marker", NetworkType: "minecraft:armor_stand"}
	entReg := api.RegisterEntityTypes(entity.DefaultRegistry, api.NewCustomEntityType(sentinelSpec), api.NewCustomEntityType(markerSpec))

	w := newTestWorld(t, nil, entReg)
	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		exerciseLivingSentinel(t, tx, sentinelSpec, markerSpec)
		cowH := world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 1.5}}.New(entity.CowType, entity.CowConfig{})
		cow := tx.AddEntity(cowH).(*entity.Cow)
		api.AddGoal(cow, 0, api.NewGoal(api.GoalSpec{CanStart: func(*entity.Mob) bool { return false }}))
		api.AddTargetGoal(cow, 0, api.NewGoal(api.GoalSpec{CanStart: func(*entity.Mob) bool { return false }}))
	})

	w.AdvanceTick()
	w.AdvanceTick()
	canContinue.Store(false)
	w.AdvanceTick()

	if ticked.Load() < 3 || started.Load() < 1 || goalTicked.Load() < 1 || stopped.Load() < 1 {
		t.Fatalf("lifecycle counters ticked=%d started=%d goalTicked=%d stopped=%d",
			ticked.Load(), started.Load(), goalTicked.Load(), stopped.Load())
	}
	if interacted.Load() != 1 || attacked.Load() != 1 {
		t.Fatalf("interaction=%d attack=%d, want 1 and 1", interacted.Load(), attacked.Load())
	}
}

func exerciseFormsAndModals(t *testing.T, p *player.Player, tx *world.Tx) {
	t.Helper()
	var formChoice string
	var formClosed, modalConfirmed, modalClosed bool

	f := api.NewForm(api.FormSpec{
		Title:   "Shop",
		Body:    "Choose",
		Buttons: []api.FormButton{{Text: "Buy Apple", OnClick: func(*player.Player, *world.Tx) { formChoice = "apple" }}},
		OnClose: func(*player.Player, *world.Tx) { formClosed = true },
	})
	api.SendForm(p, api.FormSpec{Title: "Quick"})
	if err := f.SubmitJSON([]byte("0"), p, tx); err != nil {
		t.Fatalf("Form SubmitJSON(0): %v", err)
	}
	if err := f.SubmitJSON(nil, p, tx); err != nil {
		t.Fatalf("Form SubmitJSON(nil): %v", err)
	}

	mod := api.NewModal(api.ModalSpec{
		Title:     "Confirm",
		Body:      "Proceed?",
		OnConfirm: func(*player.Player, *world.Tx) { modalConfirmed = true },
		OnClose:   func(*player.Player, *world.Tx) { modalClosed = true },
	})
	api.SendModal(p, api.ModalSpec{Title: "QuickModal"})
	confirmJSON, _ := json.Marshal(true)
	if err := mod.SubmitJSON(confirmJSON, p, tx); err != nil {
		t.Fatalf("Modal SubmitJSON(true): %v", err)
	}
	if err := mod.SubmitJSON(nil, p, tx); err != nil {
		t.Fatalf("Modal SubmitJSON(nil): %v", err)
	}

	if formChoice != "apple" || !formClosed || !modalConfirmed || !modalClosed {
		t.Fatalf("form/modal mismatch: choice=%q fClosed=%v mConf=%v mClosed=%v",
			formChoice, formClosed, modalConfirmed, modalClosed)
	}
}

func TestMenusAndForms(t *testing.T) {
	t.Parallel()

	w := newTestWorld(t, nil, entity.DefaultRegistry)
	rec := clienttest.New()
	var clicked int
	var changes []int

	m := api.NewMenu(api.MenuSpec{
		Size:     27,
		ReadOnly: false,
		Slots: map[int]api.MenuSlot{
			0: {Item: item.NewStack(item.Apple{}, 8)},
			1: {Item: item.NewStack(item.Diamond{}, 1), Locked: true},
		},
		OnClick:  func(_ *player.Player, slot int, _ item.Stack) bool { clicked++; return slot != 5 },
		OnChange: func(slot int, _ item.Stack, _ item.Stack) { changes = append(changes, slot) },
	})

	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "UiTester", mgl64.Vec3{0.5, 64, 0.5}, rec)
		host := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}, item.NewStack(item.Emerald{}, 1)))
		m.OpenForEntity(p, host, tx)
		if p.Menu() == nil || p.Menu().Inventory() != m.Inventory() {
			t.Fatal("OpenForEntity did not open custom menu inventory")
		}

		batch := p.BeginMenu(func(_ menu.Slot, r menu.Role) menu.Name { return menu.Name(r) })
		if err := batch.Move(menu.Ref{Slot: menu.Slot{Inv: m.Inventory(), Index: 0}}, menu.Ref{Slot: menu.Slot{Inv: m.Inventory(), Index: 2}}, 3); err != nil {
			t.Fatalf("Move unlocked slot failed: %v", err)
		}
		batch.Commit()

		batch2 := p.BeginMenu(func(_ menu.Slot, r menu.Role) menu.Name { return menu.Name(r) })
		if err := batch2.Move(menu.Ref{Slot: menu.Slot{Inv: m.Inventory(), Index: 1}}, menu.Ref{Slot: menu.Slot{Inv: m.Inventory(), Index: 3}}, 1); err == nil {
			t.Fatal("Move out of locked slot 1 succeeded, want refusal")
		}
		batch2.Rollback()
		p.CloseMenu(false)

		if got, _ := m.Inventory().Item(2); got.Count() != 3 {
			t.Fatalf("slot 2 count = %d, want 3", got.Count())
		}
		if got, _ := m.Inventory().Item(1); got.Count() != 1 {
			t.Fatalf("locked slot 1 lost item: %v", got)
		}

		exerciseFormsAndModals(t, p, tx)
		if len(rec.CallsTo("OpenMenu")) != 1 || len(rec.CallsTo("SendForm")) != 2 {
			t.Fatalf("expected OpenMenu and SendForm calls recorded, got open=%d forms=%d",
				len(rec.CallsTo("OpenMenu")), len(rec.CallsTo("SendForm")))
		}
	})
	if clicked == 0 || len(changes) == 0 {
		t.Fatalf("expected menu click/change callbacks, got clicked=%d changes=%v", clicked, changes)
	}
}

type testCmdSource struct {
	player  *player.Player
	outputs []*cmd.Output
}

func (s *testCmdSource) Name() string                    { return s.player.Name() }
func (s *testCmdSource) Position() mgl64.Vec3            { return s.player.Position() }
func (s *testCmdSource) World() *world.World             { return s.player.Tx().World() }
func (s *testCmdSource) SendCommandOutput(o *cmd.Output) { s.outputs = append(s.outputs, o) }

func TestDeclarativeCommands(t *testing.T) {
	var gotArgs [][]string
	var allowed atomic.Bool
	allowed.Store(true)

	api.RegisterCommand(api.CommandSpec{
		Name:        "sdkgreet",
		Description: "SDK test command",
		Aliases:     []string{"sg"},
		Permission:  cmd.LevelAll,
		Allow:       func(cmd.Source) bool { return allowed.Load() },
		RunOutput: func(_ cmd.Source, out *cmd.Output, args []string, _ *world.Tx) {
			gotArgs = append(gotArgs, args)
			out.Printf("ok:%d", len(args))
		},
	})

	c, ok := cmd.ByAlias("sg")
	if !ok {
		t.Fatal("command alias sg not registered")
	}

	w := newTestWorld(t, nil, entity.DefaultRegistry)
	<-w.Exec(func(tx *world.Tx) {
		p := spawnTestPlayer(t, tx, "CmdTester", mgl64.Vec3{0.5, 64, 0.5}, nil)
		src := &testCmdSource{player: p}
		c.Execute("", src, tx)
		c.Execute("one two three", src, tx)
		allowed.Store(false)
		c.Execute("denied", src, tx)

		if len(gotArgs) != 2 || len(gotArgs[0]) != 0 || !slices.Equal(gotArgs[1], []string{"one", "two", "three"}) {
			t.Fatalf("gotArgs = %v, want [[], [one two three]]", gotArgs)
		}
		if len(src.outputs) != 3 || len(src.outputs[2].Errors()) == 0 {
			t.Fatalf("expected 3rd invocation to produce permission error, got %+v", src.outputs)
		}
	})
}

func TestOverworldWorldGenHooks(t *testing.T) {
	t.Parallel()

	world.DefaultBlockRegistry.Finalize()
	var biomeCalls, terrainCalls, decorateCalls atomic.Int32
	var rngFirst int32
	var mu sync.Mutex
	var rngDraws []int32

	gen := overworld.New(4242)
	api.AttachOverworldHooks(gen, api.WorldGenHooks{
		OnBiome: func(x, y, z int, def uint8) uint8 {
			biomeCalls.Add(1)
			return def
		},
		OnBiomes: func(_ world.ChunkPos, _ *chunk.Chunk) {
			biomeCalls.Add(1)
		},
		OnTerrain: func(_ world.ChunkPos, _ *chunk.Chunk) {
			terrainCalls.Add(1)
		},
		OnDecorate: func(_ world.ChunkPos, _ *chunk.Chunk, rng api.WorldGenRandom) {
			decorateCalls.Add(1)
			v := rng.NextInt(1000)
			mu.Lock()
			rngFirst = v
			rngDraws = append(rngDraws, v)
			mu.Unlock()
		},
	})

	c1 := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
	gen.GenerateChunk(world.ChunkPos{0, 0}, c1, world.GenerationContext{})

	if biomeCalls.Load() < 1 || terrainCalls.Load() != 1 || decorateCalls.Load() != 1 {
		t.Fatalf("hooks didn't all fire: biome=%d terrain=%d decorate=%d",
			biomeCalls.Load(), terrainCalls.Load(), decorateCalls.Load())
	}

	// Verify deterministic WorldGenRandom across identical seed + chunk position.
	gen2 := overworld.New(4242)
	api.AttachOverworldHooks(gen2, api.WorldGenHooks{
		OnDecorate: func(_ world.ChunkPos, _ *chunk.Chunk, rng api.WorldGenRandom) {
			mu.Lock()
			rngDraws = append(rngDraws, rng.NextInt(1000))
			mu.Unlock()
		},
	})
	c2 := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
	gen2.GenerateChunk(world.ChunkPos{0, 0}, c2, world.GenerationContext{})

	mu.Lock()
	draws := slices.Clone(rngDraws)
	mu.Unlock()
	if len(draws) != 2 || draws[0] != rngFirst || draws[0] != draws[1] {
		t.Fatalf("expected deterministic OnDecorate rng across matching seeds, got %v", draws)
	}

	api.ClearOverworldHooks(gen)
	cleared := gen.Hooks()
	if cleared.OnBiome != nil || cleared.OnBiomes != nil || cleared.OnTerrain != nil || cleared.OnDecorate != nil {
		t.Fatalf("ClearOverworldHooks left hooks set: %+v", cleared)
	}
}
