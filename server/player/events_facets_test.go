package player

import (
	"log/slog"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/client"
	"github.com/dm-vev/adamant/server/client/clienttest"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/event"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player/bossbar"
	"github.com/dm-vev/adamant/server/player/chat"
	"github.com/dm-vev/adamant/server/player/dialogue"
	"github.com/dm-vev/adamant/server/player/form"
	"github.com/dm-vev/adamant/server/player/scoreboard"
	"github.com/dm-vev/adamant/server/player/title"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type testMenuSubmittable struct{}

func (testMenuSubmittable) Submit(form.Submitter, form.Button, *world.Tx) {}

type testDialogueSubmittable struct{}

func (testDialogueSubmittable) Submit(dialogue.Submitter, dialogue.Button, *world.Tx) {}

type testPlayerCmd struct {
	runs *int
}

func (c testPlayerCmd) Run(_ cmd.Source, _ *cmd.Output, _ *world.Tx) {
	if c.runs != nil {
		*c.runs++
	}
}

type testEntityBroadcastRecorder struct {
	*clienttest.Recorder
	actionShared   int
	velocityShared int
	itemsShared    int
	armourShared   int
	reused         int
}

func (c *testEntityBroadcastRecorder) ViewEntityStateShared(e world.Entity, b *world.Broadcast) {
	c.ViewEntityState(e)
}

func (c *testEntityBroadcastRecorder) ViewEntityActionShared(e world.Entity, a world.EntityAction, b *world.Broadcast) {
	c.actionShared++
	if b.Load() != nil {
		c.reused++
	} else {
		b.Store("action")
	}
	c.Recorder.ViewEntityAction(e, a)
}

func (c *testEntityBroadcastRecorder) ViewEntityVelocityShared(e world.Entity, vel mgl64.Vec3, b *world.Broadcast) {
	c.velocityShared++
	if b.Load() != nil {
		c.reused++
	} else {
		b.Store("velocity")
	}
	c.Recorder.ViewEntityVelocity(e, vel)
}

func (c *testEntityBroadcastRecorder) ViewEntityItemsShared(e world.Entity, b *world.Broadcast) {
	c.itemsShared++
	if b.Load() != nil {
		c.reused++
	} else {
		b.Store("items")
	}
	c.Recorder.ViewEntityItems(e)
}

func (c *testEntityBroadcastRecorder) ViewEntityArmourShared(e world.Entity, b *world.Broadcast) {
	c.armourShared++
	if b.Load() != nil {
		c.reused++
	} else {
		b.Store("armour")
	}
	c.Recorder.ViewEntityArmour(e)
}

var _ world.EntityBroadcastViewer = (*testEntityBroadcastRecorder)(nil)

type cancelAllHandler struct {
	NopHandler
	cancelled *[]string
}

func (h cancelAllHandler) record(name string, ctx *Context) {
	if h.cancelled != nil {
		*h.cancelled = append(*h.cancelled, name)
	}
	ctx.Cancel()
}

func (h cancelAllHandler) HandleMove(ctx *Context, _ mgl64.Vec3, _ cube.Rotation) {
	h.record("handler:move", ctx)
}

func (h cancelAllHandler) HandleHurt(ctx *Context, _ *float64, _ bool, _ *time.Duration, _ world.DamageSource) {
	h.record("handler:hurt", ctx)
}

func (h cancelAllHandler) HandleChat(ctx *Context, _ *string) {
	h.record("handler:chat", ctx)
}

type chatRecSubscriber struct {
	id   uuid.UUID
	msgs []string
}

func (s *chatRecSubscriber) UUID() uuid.UUID { return s.id }
func (s *chatRecSubscriber) Message(a ...any) {
	s.msgs = append(s.msgs, format(a))
}

func testFlatWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
	})
	return w
}

func TestPlayerFacetsZeroAllocAndAPI(t *testing.T) {
	w := testFlatWorld(t)
	r := clienttest.New()
	r.Answers.Addr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}
	r.Answers.Latency = 18 * time.Millisecond
	r.Answers.ChunkRadius = 10
	r.Answers.Profile = client.Profile{DeviceID: "dev-1", DeviceModel: "Linux", SelfSignedID: "self-1"}
	r.Answers.NetStats = client.NetStats{Queued: 4, Inbox: 2}

	start := mgl64.Vec3{0.5, 64, 0.5}
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name:     "FacetUser",
		Position: start,
		GameMode: world.GameModeSurvival,
		Client:   r,
	})

	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		defer func() { _ = p.Close() }()

		// Verify facets are zero-allocation.
		var pos mgl64.Vec3
		var hp float64
		var addr net.Addr
		var bag *ecs.Bag
		if n := testing.AllocsPerRun(50, func() {
			pos = p.Movement().Position()
			hp = p.Combat().Health()
			_ = p.UI()
			addr = p.Net().Addr()
			bag = p.Components()
		}); n != 0 {
			t.Fatalf("facet access allocated %v times/op, want 0", n)
		}
		if pos != start || hp != 20 || addr == nil || bag == nil {
			t.Fatalf("unexpected facet state pos=%v hp=%v addr=%v bag=%v", pos, hp, addr, bag)
		}

		// MovementFacet.
		mv := p.Movement()
		mv.SetSpeed(0.2)
		if mv.Speed() != 0.2 {
			t.Fatalf("Speed = %v, want 0.2", mv.Speed())
		}
		mv.Move(mgl64.Vec3{0.5, 0, 0}, 15, 5)
		if mv.Position().X() != 1.0 || mv.Rotation().Yaw() != 15 {
			t.Fatalf("Move produced pos=%v rot=%v", mv.Position(), mv.Rotation())
		}
		mv.Teleport(start)
		if mv.Position() != start {
			t.Fatalf("Teleport pos = %v, want %v", mv.Position(), start)
		}
		mv.Sprint(true)
		if !mv.Sprinting() {
			t.Fatal("expected Sprinting=true after Sprint(true)")
		}
		mv.StopSprinting()
		if mv.Sprinting() {
			t.Fatal("expected Sprinting=false after StopSprinting")
		}
		mv.StartSprinting()
		mv.Sneak(true)
		if !mv.Sneaking() {
			t.Fatal("expected Sneaking=true after Sneak(true)")
		}
		mv.StopSneaking()
		if mv.Sneaking() {
			t.Fatal("expected Sneaking=false after StopSneaking")
		}
		mv.StartSneaking()
		mv.Sneak(false)
		mv.Swim(true)
		if !mv.Swimming() {
			t.Fatal("expected Swimming=true")
		}
		mv.Swim(false)
		mv.SetVelocity(mgl64.Vec3{0, 0.4, 0})
		_ = mv.Velocity()
		_ = mv.OnGround()
		mv.Fly(true)
		mv.Fly(false)
		mv.Glide(true)
		mv.Glide(false)

		// CombatFacet.
		cb := p.Combat()
		cb.SetMaxHealth(30)
		if cb.MaxHealth() != 30 {
			t.Fatalf("MaxHealth = %v, want 30", cb.MaxHealth())
		}
		cb.Hurt(4, entity.VoidDamageSource{})
		if cb.Health() != 16 {
			t.Fatalf("Health after Hurt(4) = %v, want 16", cb.Health())
		}
		healed := cb.Heal(2, entity.FoodHealingSource{})
		if healed != 2 || cb.Health() != 18 {
			t.Fatalf("Heal = %v health=%v, want 2, 18", healed, cb.Health())
		}
		eff := effect.New(effect.Speed, 1, time.Second*5)
		cb.AddEffect(eff)
		if gotEff, ok := cb.Effect(effect.Speed); !ok || len(cb.Effects()) != 1 || gotEff.Level() != 1 {
			t.Fatalf("Effect state = (%v, %v, len=%d)", gotEff, ok, len(cb.Effects()))
		}
		cb.RemoveEffect(effect.Speed)
		if len(cb.Effects()) != 0 {
			t.Fatalf("expected 0 effects after RemoveEffect, got %d", len(cb.Effects()))
		}
		if cb.Armour() == nil || cb.Dead() {
			t.Fatalf("unexpected Armour=%v Dead=%v", cb.Armour(), cb.Dead())
		}
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: start.Add(mgl64.Vec3{1, 0, 0})}))
		if !cb.AttackEntity(cow) {
			t.Fatal("expected AttackEntity(cow) to succeed")
		}
		cb.Kill(nil)
		if !cb.Dead() {
			t.Fatal("expected Dead=true after CombatFacet.Kill")
		}
		if cb.Respawn() != p.H() {
			t.Fatal("Respawn did not return player's handle")
		}
	})

	// UIFacet and NetFacet checks on a fresh alive player.
	h2 := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "UIUser", Position: start, GameMode: world.GameModeSurvival, Client: r,
	})
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(h2).(*Player)
		defer func() { _ = p.Close() }()

		tx.SetBlock(cube.Pos{1, 64, 0}, block.NewChest(), nil)
		r.Reset()
		ui := p.UI()
		ui.Message("hi")
		ui.Messagef("n=%d", 7)
		ui.Messaget(chat.MessageJoin, "UIUser")
		ui.Tip("tip")
		ui.Popup("popup")
		ui.JukeboxPopup("juke")
		ui.SendToast("t", "m")
		ui.SendTitle(title.New("hello"))
		ui.SendForm(form.NewMenu(testMenuSubmittable{}, "menu"))
		ui.CloseForm()
		ui.SendDialogue(dialogue.New(testDialogueSubmittable{}, "dlg"), p)
		ui.CloseDialogue()
		ui.SendBossBar(bossbar.New("boss"))
		ui.RemoveBossBar()
		ui.RemoveBossBarID("custom")
		sb := scoreboard.New("board")
		ui.SendScoreboard(sb)
		ui.RemoveScoreboard()
		ui.OpenMenu(cube.Pos{1, 64, 0}, tx)
		if ui.Menu() == nil {
			t.Fatal("expected non-nil Menu after OpenMenu")
		}
		ui.CloseMenu(false)
		if ui.Menu() != nil {
			t.Fatal("expected nil Menu after CloseMenu")
		}
		if len(r.CallsTo("SendMessage")) != 3 || len(r.CallsTo("SendTip")) != 1 || len(r.CallsTo("SendPopup")) != 1 ||
			len(r.CallsTo("SendJukeboxPopup")) != 1 || len(r.CallsTo("SendToast")) != 1 || len(r.CallsTo("SendTitle")) != 1 ||
			len(r.CallsTo("SendForm")) != 1 || len(r.CallsTo("SendDialogue")) != 1 || len(r.CallsTo("SendBossBarID")) != 1 ||
			len(r.CallsTo("SendScoreboard")) != 1 || len(r.CallsTo("OpenMenu")) != 1 || len(r.CallsTo("CloseMenu")) != 1 {
			t.Fatalf("missing UIFacet client calls: %+v", r.Calls())
		}

		nf := p.Net()
		if nf.Addr() != r.Answers.Addr || nf.Latency() != 18*time.Millisecond || nf.ChunkRadius() != 10 || nf.Profile() != r.Answers.Profile {
			t.Fatalf("unexpected NetFacet values: addr=%v lat=%v cr=%d prof=%+v", nf.Addr(), nf.Latency(), nf.ChunkRadius(), nf.Profile())
		}
		if stats, ok := nf.NetStats(); !ok || stats.Queued != 4 {
			t.Fatalf("NetStats = (%+v, %v), want Queued=4, true", stats, ok)
		}
		if err := nf.Transfer("127.0.0.1:19132"); err != nil {
			t.Fatalf("Transfer error: %v", err)
		}
		if len(r.CallsTo("Transfer")) != 1 {
			t.Fatal("expected NetFacet.Transfer to call client.Transfer")
		}
		nf.Disconnect("bye")
		if len(r.CallsTo("Disconnect")) != 1 {
			t.Fatal("expected NetFacet.Disconnect to disconnect client")
		}
	})
}

func TestPlayerLazyEventsAndECSComponents(t *testing.T) {
	w1 := testFlatWorld(t)
	w2 := testFlatWorld(t)

	type playerBadge struct {
		Rank int
		Tag  string
	}
	kBadge := ecs.NewKey[playerBadge]("player_test:badge")

	start := mgl64.Vec3{0.5, 64, 0.5}
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "LazyPlayer", Position: start, GameMode: world.GameModeSurvival,
	})

	<-w1.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		if p.eventBus() != nil {
			t.Fatal("expected idle player to keep eventBus nil with 0 allocations")
		}
		if n := testing.AllocsPerRun(50, func() {
			p.Move(mgl64.Vec3{0, 0, 0}, 0, 0)
			_ = p.Components()
		}); n != 0 {
			t.Fatalf("idle Move + Components allocated %v times/op, want 0", n)
		}
		if p.eventBus() != nil {
			t.Fatal("expected idle Move not to initialize eventBus")
		}

		ecs.Set(p.Components(), kBadge, playerBadge{Rank: 7, Tag: "knight"})
		if got, ok := ecs.Get(p.H().Components(), kBadge); !ok || got.Rank != 7 || got.Tag != "knight" {
			t.Fatalf("H().Components() = (%+v, %v), want Rank=7 Tag=knight", got, ok)
		}

		bus := p.Events()
		if bus == nil || p.Events() != bus {
			t.Fatal("expected lazy Events() to initialize once and return same Bus")
		}
		var moves int
		event.On(bus, event.PriorityNormal, func(e *MoveEvent) {
			moves++
		})
		p.Move(mgl64.Vec3{0.1, 0, 0}, 0, 0)
		if moves != 1 {
			t.Fatalf("moves = %d, want 1", moves)
		}

		// Components and Events bus survive TeleportToWorld across worlds.
		p.TeleportToWorld(tx, w2, start)
	})

	<-w2.Exec(func(tx *world.Tx) {
		ent, ok := h.Entity(tx)
		if !ok {
			t.Fatal("player did not arrive in w2")
		}
		p := ent.(*Player)
		defer func() { _ = p.Close() }()

		got, ok := ecs.Get(p.Components(), kBadge)
		if !ok || got.Rank != 7 || got.Tag != "knight" {
			t.Fatalf("components after world transfer = (%+v, %v), want Rank=7 Tag=knight", got, ok)
		}
		ecs.Update(p.Components(), kBadge, func(b *playerBadge) { b.Rank = 9 })
		if got, ok := ecs.Get(p.Components(), kBadge); !ok || got.Rank != 9 {
			t.Fatalf("components after Update = (%+v, %v), want Rank=9", got, ok)
		}

		var movesAfter int
		sub := event.On(p.Events(), event.PriorityLowest, func(e *MoveEvent) {
			movesAfter++
		})
		defer sub.Unsubscribe()
		p.Move(mgl64.Vec3{0.1, 0, 0}, 0, 0)
		if movesAfter != 1 {
			t.Fatalf("movesAfter = %d, want 1", movesAfter)
		}
	})
}

func TestPlayerEventsPriorityCancellationAndHandlers(t *testing.T) {
	w := testFlatWorld(t)
	cmdRuns := 0
	cmd.Register(cmd.New("test_player_event_cmd", "", nil, testPlayerCmd{runs: &cmdRuns}))

	start := mgl64.Vec3{0.5, 64, 0.5}
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "EventUser", Position: start, GameMode: world.GameModeSurvival,
		Client: clienttest.New(),
	})

	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		defer func() { _ = p.Close() }()

		// 1. Priority ordering + cancellation + PriorityMonitor on MoveEvent (p.Events + tx.Events).
		var trace []string
		sub1 := event.On(p.Events(), event.PriorityLowest, func(e *MoveEvent) {
			trace = append(trace, "p:lowest")
		})
		sub2 := event.On(p.Events(), event.PriorityNormal, func(e *MoveEvent) {
			trace = append(trace, "p:normal")
			e.Cancel()
		})
		sub3 := event.On(p.Events(), event.PriorityHigh, func(e *MoveEvent) {
			trace = append(trace, "p:high-should-skip")
		})
		sub4 := event.On(p.Events(), event.PriorityMonitor, func(e *MoveEvent) {
			if !e.Cancelled() {
				t.Error("PriorityMonitor expected Cancelled()=true")
			}
			trace = append(trace, "p:monitor")
		})
		subW := event.On(tx.Events(), event.PriorityNormal, func(e *MoveEvent) {
			trace = append(trace, "w:normal-should-skip")
		})
		subWM := event.On(tx.Events(), event.PriorityMonitor, func(e *MoveEvent) {
			trace = append(trace, "w:monitor")
		})

		before := p.Position()
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if p.Position() != before {
			t.Fatalf("cancelled Move changed position to %v", p.Position())
		}
		want := []string{"p:lowest", "p:normal", "p:monitor", "w:monitor"}
		if !slices.Equal(trace, want) {
			t.Fatalf("Move trace = %v, want %v", trace, want)
		}
		sub1.Unsubscribe()
		sub2.Unsubscribe()
		sub3.Unsubscribe()
		sub4.Unsubscribe()
		subW.Unsubscribe()
		subWM.Unsubscribe()

		// 2. Handler cancellation propagates to Bus and still notifies PriorityMonitor.
		trace = nil
		p.Handle(cancelAllHandler{cancelled: &trace})
		subM := event.On(p.Events(), event.PriorityMonitor, func(e *MoveEvent) {
			if !e.Cancelled() {
				t.Error("expected Bus PriorityMonitor to see Handler cancellation")
			}
			trace = append(trace, "bus:monitor")
		})
		subN := event.On(p.Events(), event.PriorityNormal, func(e *MoveEvent) {
			trace = append(trace, "bus:normal-should-skip")
		})
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if !slices.Equal(trace, []string{"handler:move", "bus:monitor"}) {
			t.Fatalf("Handler+Bus trace = %v, want [handler:move bus:monitor]", trace)
		}
		subM.Unsubscribe()
		subN.Unsubscribe()
		p.Handle(NopHandler{})

		// 3. TeleportEvent.
		var tpSeen bool
		subTP := event.On(p.Events(), event.PriorityNormal, func(e *TeleportEvent) {
			tpSeen = true
			e.Cancel()
		})
		p.Teleport(mgl64.Vec3{3, 64, 3})
		if !tpSeen || p.Position() != before {
			t.Fatalf("Teleport cancel failed: tpSeen=%v pos=%v", tpSeen, p.Position())
		}
		subTP.Unsubscribe()

		// 4. ChatEvent (mutation and cancellation).
		subChat := &chatRecSubscriber{id: uuid.New()}
		chat.Global.Subscribe(subChat)
		defer chat.Global.Unsubscribe(subChat)
		subChatEv := event.On(p.Events(), event.PriorityNormal, func(e *ChatEvent) {
			e.Message += " [edited]"
		})
		p.Chat("hello")
		if len(subChat.msgs) != 1 || subChat.msgs[0] != "<EventUser> hello [edited]" {
			t.Fatalf("chat msgs = %v", subChat.msgs)
		}
		subChatEv.Unsubscribe()

		subChatCancel := event.On(tx.Events(), event.PriorityNormal, func(e *ChatEvent) {
			e.Cancel()
		})
		p.Chat("blocked")
		if len(subChat.msgs) != 1 {
			t.Fatalf("expected cancelled chat not to broadcast, got %v", subChat.msgs)
		}
		subChatCancel.Unsubscribe()

		// 5. BlockPlaceEvent and BlockBreakEvent.
		placePos := cube.Pos{1, 64, 0}
		subPlace := event.On(p.Events(), event.PriorityNormal, func(e *BlockPlaceEvent) {
			e.Cancel()
		})
		p.PlaceBlock(placePos, block.Stone{}, nil)
		if _, isAir := tx.Block(placePos).(block.Air); !isAir {
			t.Fatalf("expected cancelled BlockPlace to leave air, got %T", tx.Block(placePos))
		}
		subPlace.Unsubscribe()
		p.PlaceBlock(placePos, block.Stone{}, nil)
		if _, isStone := tx.Block(placePos).(block.Stone); !isStone {
			t.Fatalf("expected uncancelled BlockPlace to place stone, got %T", tx.Block(placePos))
		}

		var brokeSeen bool
		subBreak := event.On(p.Events(), event.PriorityNormal, func(e *BlockBreakEvent) {
			brokeSeen = true
			e.Cancel()
		})
		p.BreakBlock(placePos)
		if !brokeSeen {
			t.Fatal("expected BlockBreakEvent to fire")
		}
		if _, isStone := tx.Block(placePos).(block.Stone); !isStone {
			t.Fatalf("expected cancelled BlockBreak to keep stone, got %T", tx.Block(placePos))
		}
		subBreak.Unsubscribe()

		// 6. AttackEntityEvent and HurtEvent.
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: start.Add(mgl64.Vec3{1, 0, 0})}))
		subAtk := event.On(p.Events(), event.PriorityNormal, func(e *AttackEntityEvent) {
			e.Cancel()
		})
		if p.AttackEntity(cow) {
			t.Fatal("expected cancelled AttackEntityEvent to abort attack")
		}
		subAtk.Unsubscribe()

		subHurt := event.On(p.Events(), event.PriorityNormal, func(e *HurtEvent) {
			e.Damage = 6
		})
		dealt, ok := p.Hurt(2, entity.VoidDamageSource{})
		if !ok || dealt != 2 || p.Health() != 14 {
			t.Fatalf("Hurt with modified damage = (%v, %v, hp=%v), want (2, true, hp=14)", dealt, ok, p.Health())
		}
		subHurt.Unsubscribe()

		// 7. ToggleSprintEvent & ToggleSneakEvent.
		subSprint := event.On(p.Events(), event.PriorityNormal, func(e *ToggleSprintEvent) {
			if e.Sprinting {
				e.Cancel()
			}
		})
		p.StartSprinting()
		if p.Sprinting() {
			t.Fatal("expected cancelled ToggleSprintEvent to prevent sprinting")
		}
		subSprint.Unsubscribe()

		subSneak := event.On(p.Events(), event.PriorityNormal, func(e *ToggleSneakEvent) {
			if e.Sneaking {
				e.Cancel()
			}
		})
		p.StartSneaking()
		if p.Sneaking() {
			t.Fatal("expected cancelled ToggleSneakEvent to prevent sneaking")
		}
		subSneak.Unsubscribe()

		// 8. ItemUseEvent, CommandEvent, DropEvent.
		_ = p.Inventory().SetItem(0, item.NewStack(item.Snowball{}, 1))
		var usedItem bool
		subUse := event.On(p.Events(), event.PriorityNormal, func(e *ItemUseEvent) {
			usedItem = true
			e.Cancel()
		})
		p.UseItem()
		held, _ := p.HeldItems()
		if !usedItem || held.Count() != 1 {
			t.Fatalf("ItemUse cancel failed: usedItem=%v count=%d", usedItem, held.Count())
		}
		subUse.Unsubscribe()

		subCmd := event.On(p.Events(), event.PriorityNormal, func(e *CommandEvent) {
			e.Cancel()
		})
		p.ExecuteCommand("/test_player_event_cmd")
		if cmdRuns != 0 {
			t.Fatalf("expected cancelled CommandEvent not to run command, got %d runs", cmdRuns)
		}
		subCmd.Unsubscribe()
		p.ExecuteCommand("/test_player_event_cmd")
		if cmdRuns != 1 {
			t.Fatalf("expected CommandEvent to allow command, got %d runs", cmdRuns)
		}

		subDrop := event.On(p.Events(), event.PriorityNormal, func(e *DropEvent) {
			e.Cancel()
		})
		if dropped := p.Drop(item.NewStack(item.Diamond{}, 1)); dropped != 0 {
			t.Fatalf("expected cancelled DropEvent to drop 0, got %d", dropped)
		}
		subDrop.Unsubscribe()

		// 9. DeathEvent & QuitEvent.
		var deathSeen, quitPlayerSeen, quitWorldSeen bool
		playerID := p.UUID()
		event.On(p.Events(), event.PriorityNormal, func(e *DeathEvent) {
			deathSeen = true
			e.KeepInventory = true
		})
		event.On(p.Events(), event.PriorityNormal, func(e *QuitEvent) {
			quitPlayerSeen = true
		})
		subWQuit := event.On(tx.Events(), event.PriorityNormal, func(e *QuitEvent) {
			if e.Player.UUID() == playerID {
				quitWorldSeen = true
			}
		})
		defer subWQuit.Unsubscribe()

		p.Combat().Kill(entity.VoidDamageSource{})
		if !deathSeen {
			t.Fatal("expected DeathEvent to fire")
		}
		if heldAfterDeath, _ := p.HeldItems(); heldAfterDeath.Count() != 1 {
			t.Fatalf("expected DeathEvent KeepInventory=true to preserve held snowball, got %d", heldAfterDeath.Count())
		}
		_ = p.Close()
		if !quitPlayerSeen || !quitWorldSeen {
			t.Fatalf("QuitEvent missing: player=%v world=%v", quitPlayerSeen, quitWorldSeen)
		}
	})
}

func TestExtendedSharedBroadcastsAndSelf(t *testing.T) {
	t.Parallel()
	w := testFlatWorld(t)

	v1 := &testEntityBroadcastRecorder{Recorder: clienttest.New()}
	v2 := &testEntityBroadcastRecorder{Recorder: clienttest.New()}
	self := &testEntityBroadcastRecorder{Recorder: clienttest.New()}
	l1 := world.NewLoader(1, w, v1)
	l2 := world.NewLoader(1, w, v2)
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			l1.Close(tx)
			l2.Close(tx)
		})
	}()

	start := mgl64.Vec3{0.5, 64, 0.5}
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		l1.Move(tx, start)
		l1.Load(tx, 9)
		l2.Move(tx, start)
		l2.Load(tx, 9)
		h = world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "Broadcaster", Position: start, GameMode: world.GameModeSurvival, Client: self,
		})
		tx.AddEntity(h)
	})

	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "SimBroadcaster", Position: start, GameMode: world.GameModeSurvival,
		})).(*Player)
		defer func() { _ = p.Close() }()

		v1.actionShared, v2.actionShared, v1.reused, v2.reused = 0, 0, 0, 0
		p.SwingArm()
		if v1.actionShared != 1 || v2.actionShared != 1 || v1.reused+v2.reused != 1 {
			t.Fatalf("sim SwingArm shared = (%d,%d) reused=%d", v1.actionShared, v2.actionShared, v1.reused+v2.reused)
		}

		v1.itemsShared, v2.itemsShared, v1.reused, v2.reused = 0, 0, 0, 0
		_ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 1))
		if v1.itemsShared != 1 || v2.itemsShared != 1 || v1.reused+v2.reused != 1 {
			t.Fatalf("sim items shared = (%d,%d) reused=%d", v1.itemsShared, v2.itemsShared, v1.reused+v2.reused)
		}

		v1.armourShared, v2.armourShared, v1.reused, v2.reused = 0, 0, 0, 0
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		if v1.armourShared != 1 || v2.armourShared != 1 || v1.reused+v2.reused != 1 {
			t.Fatalf("sim armour shared = (%d,%d) reused=%d", v1.armourShared, v2.armourShared, v1.reused+v2.reused)
		}
	})

	<-w.Exec(func(tx *world.Tx) {
		p := mustPlayer(t, h, tx)
		defer func() { _ = p.Close() }()

		v1.actionShared, v2.actionShared, self.actionShared, v1.reused, v2.reused, self.reused = 0, 0, 0, 0, 0, 0
		p.SwingArm()
		if v1.actionShared != 1 || v2.actionShared != 1 || self.actionShared != 1 || v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("client SwingArm shared = (%d,%d,%d) reused=%d", v1.actionShared, v2.actionShared, self.actionShared, v1.reused+v2.reused+self.reused)
		}

		v1.velocityShared, v2.velocityShared, self.velocityShared, v1.reused, v2.reused, self.reused = 0, 0, 0, 0, 0, 0
		p.SetVelocity(mgl64.Vec3{0.2, 0.4, 0})
		if v1.velocityShared != 1 || v2.velocityShared != 1 || self.velocityShared != 1 || v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("client SetVelocity shared = (%d,%d,%d) reused=%d", v1.velocityShared, v2.velocityShared, self.velocityShared, v1.reused+v2.reused+self.reused)
		}

		v1.itemsShared, v2.itemsShared, self.itemsShared, v1.reused, v2.reused, self.reused = 0, 0, 0, 0, 0, 0
		if err := p.SetHeldSlot(1); err != nil {
			t.Fatal(err)
		}
		if v1.itemsShared != 1 || v2.itemsShared != 1 || self.itemsShared != 1 || v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("client SetHeldSlot shared = (%d,%d,%d) reused=%d", v1.itemsShared, v2.itemsShared, self.itemsShared, v1.reused+v2.reused+self.reused)
		}
	})
}

func TestPlayerEventsAndComponentsRace(t *testing.T) {
	t.Parallel()
	w := testFlatWorld(t)
	start := mgl64.Vec3{0.5, 64, 0.5}
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "RacePlayer", Position: start, GameMode: world.GameModeSurvival,
	})
	kCount := ecs.NewKey[int]("player_test:race_count")

	var playerPtr *Player
	<-w.Exec(func(tx *world.Tx) {
		playerPtr = tx.AddEntity(h).(*Player)
		ecs.Set(playerPtr.Components(), kCount, 0)
	})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus := playerPtr.Events()
			sub := event.On(bus, event.PriorityNormal, func(e *MoveEvent) {})
			sub.Unsubscribe()
		}()
	}
	wg.Wait()

	<-w.Exec(func(tx *world.Tx) {
		p := mustPlayer(t, h, tx)
		defer func() { _ = p.Close() }()
		sub := event.On(p.Events(), event.PriorityNormal, func(e *MoveEvent) {
			ecs.Update(e.Player.Components(), kCount, func(v *int) { *v++ })
		})
		defer sub.Unsubscribe()
		for range 10 {
			p.Move(mgl64.Vec3{0.05, 0, 0}, 0, 0)
		}
		if got, ok := ecs.Get(p.Components(), kCount); !ok || got != 10 {
			t.Fatalf("kCount = (%d, %v), want 10", got, ok)
		}
	})
}
