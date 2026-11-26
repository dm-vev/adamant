package main

import (
	"slices"
	"testing"

	"github.com/dm-vev/adamant/sdk/perm"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/sdktest"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	corecmd "github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type cmdCapture struct {
	level    corecmd.Level
	messages []string
	errors   []string
}

func (*cmdCapture) Name() string                     { return "Console" }
func (*cmdCapture) Position() mgl64.Vec3             { return mgl64.Vec3{} }
func (c *cmdCapture) PermissionLevel() corecmd.Level { return c.level }
func (c *cmdCapture) SendCommandOutput(o *corecmd.Output) {
	for _, m := range o.Messages() {
		c.messages = append(c.messages, m.String())
	}
	for _, e := range o.Errors() {
		c.errors = append(c.errors, e.Error())
	}
}

func TestHubPluginEndToEnd(t *testing.T) {
	w := sdktest.NewWorld(t)
	app := sdktest.NewApp(t)
	hub := newHubService(w)

	// Verify dependency ordering resolves perm before hub regardless of argument order.
	if err := app.UseOrdered(hub.plugin(), newPermPlugin()); err != nil {
		t.Fatalf("UseOrdered: %v", err)
	}
	var enabled []string
	for _, p := range app.Plugins() {
		enabled = append(enabled, p.Name())
	}
	if !slices.Equal(enabled, []string{"perm", "hub"}) {
		t.Fatalf("plugin load order = %v, want [perm hub]", enabled)
	}
	store, ok := plugin.Service[*perm.Store](app)
	if !ok {
		t.Fatal("perm service not provided")
	}
	store.AddGroup("staff-member", "staff")
	if !store.Has("staff-member", "hub.reload") || !store.Has("staff-member", "hub.join") {
		t.Fatal("missing inherited hub permissions for staff group")
	}

	// Verify Guide NPC & welcome Hologram spawned.
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := hub.guide.Player(tx); !ok {
			t.Fatal("Guide NPC not present in world")
		}
		if len(hub.holo.Handles()) != 2 {
			t.Fatalf("expected 2 hologram lines, got %d", len(hub.holo.Handles()))
		}
	})

	// Spawn a player outside spawn, then walk/teleport into safe zone.
	fp := sdktest.SpawnPlayer(t, w, "Alex", mgl64.Vec3{40, 65, 40})
	fp.Run(func(p *player.Player, tx *world.Tx) {
		p.Teleport(mgl64.Vec3{1.5, 65, 0.5})
		if hub.enterCount.Load() != 1 {
			t.Fatalf("enterCount = %d, want 1", hub.enterCount.Load())
		}

		// Spawn region protects players from damage and blocks from breaking.
		dealt, vuln := p.Hurt(8, entity.VoidDamageSource{})
		if vuln || dealt != 0 {
			t.Fatalf("expected Hurt cancelled in spawn region, got dealt=%v vuln=%v", dealt, vuln)
		}
		tx.SetBlock(cube.Pos{2, 64, 2}, block.Stone{}, nil)
		p.BreakBlock(cube.Pos{2, 64, 2})
		if _, ok := tx.Block(cube.Pos{2, 64, 2}).(block.Stone); !ok {
			t.Fatal("expected Stone protected in spawn region")
		}

		// Interacting with Guide NPC opens the mode menu without hurting the NPC.
		np, ok := hub.guide.Player(tx)
		if !ok {
			t.Fatal("Guide NPC missing")
		}
		p.UseItemOnEntity(np)
		if len(fp.Recorder().CallsTo("OpenMenu")) == 0 {
			t.Fatal("expected UseItemOnEntity on Guide NPC to open virtual chest menu")
		}

		p.Teleport(mgl64.Vec3{40, 65, 40})
		if hub.leaveCount.Load() != 1 {
			t.Fatalf("leaveCount = %d, want 1", hub.leaveCount.Load())
		}
	})

	// Execute /hub reload command and verify clean output.
	hubCmd, ok := corecmd.ByAlias("hub")
	if !ok {
		t.Fatal("/hub command not registered")
	}
	src := &cmdCapture{level: corecmd.LevelAdmin}
	hubCmd.Execute("reload", src, nil)
	if len(src.messages) != 1 || src.messages[0] != "Hub config reloaded." {
		t.Fatalf("/hub reload output = messages:%v errors:%v", src.messages, src.errors)
	}

	// Disabling app must automatically tear down NPC and Hologram.
	app.Disable()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := hub.guide.Player(tx); ok {
			t.Fatal("Guide NPC still present after Disable")
		}
		if len(hub.holo.Handles()) != 0 {
			t.Fatalf("Hologram handles not cleared after Disable: %d", len(hub.holo.Handles()))
		}
	})
}
