package main

import (
	"sync/atomic"

	"github.com/dm-vev/adamant/sdk/cmd"
	"github.com/dm-vev/adamant/sdk/event"
	"github.com/dm-vev/adamant/sdk/hud"
	sdkitem "github.com/dm-vev/adamant/sdk/item"
	"github.com/dm-vev/adamant/sdk/menu"
	"github.com/dm-vev/adamant/sdk/npc"
	"github.com/dm-vev/adamant/sdk/perm"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/worlds"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type hubConfig struct {
	Spawn           [3]float64 `toml:"spawn"`
	WelcomeTitle    string     `toml:"welcome_title"`
	WelcomeSubtitle string     `toml:"welcome_subtitle"`
	SidebarTitle    string     `toml:"sidebar_title"`
	NPCPos          [3]float64 `toml:"npc_pos"`
}

func defaultHubConfig() hubConfig {
	return hubConfig{
		Spawn:           [3]float64{0.5, 65, 0.5},
		WelcomeTitle:    "Welcome to Adamant",
		WelcomeSubtitle: "Modular Bedrock SDK",
		SidebarTitle:    "Adamant Hub",
		NPCPos:          [3]float64{4.5, 65, 0.5},
	}
}

func newPermPlugin() plugin.Plugin {
	return plugin.NewFunc("perm", func(ctx *plugin.Context) {
		store := perm.New(ctx.Server())
		store.GrantGroup("default", "hub.join")
		store.Inherit("staff", "default")
		store.GrantGroup("staff", "hub.reload")
		plugin.Provide(ctx.App(), store)
	})
}

type hubService struct {
	worldOverride *world.World
	cfg           atomic.Pointer[hubConfig]
	regions       *worlds.RegionIndex
	guide         *npc.NPC
	holo          *hud.Hologram
	perms         *perm.Store
	modeMenu      *menu.Menu
	enterCount    atomic.Int32
	leaveCount    atomic.Int32
}

func newHubService(worldOverride *world.World) *hubService {
	h := &hubService{
		worldOverride: worldOverride,
		regions:       worlds.NewRegionIndex(),
	}
	cfg := defaultHubConfig()
	h.cfg.Store(&cfg)
	h.modeMenu = menu.Chest("Game Modes",
		menu.ReadOnly(),
		menu.Slot(11, sdkitem.Stack(item.Sword{Tier: item.ToolTierDiamond}).
			Name("Sky Arena").
			Lore("Click to queue").
			Build(),
			menu.Locked(),
			menu.SlotClick(func(p *player.Player, _ int, _ item.Stack) bool {
				if p != nil {
					p.Message("Queued for Sky Arena!")
				}
				return false
			}),
		),
		menu.Slot(15, sdkitem.Stack(item.Compass{}).
			Name("Survival").
			Lore("Click to explore").
			Build(),
			menu.Locked(),
			menu.SlotClick(func(p *player.Player, _ int, _ item.Stack) bool {
				if p != nil {
					p.Message("Sending you to Survival!")
				}
				return false
			}),
		),
	)
	return h
}

func (h *hubService) plugin() plugin.Plugin {
	return plugin.NewFunc("hub", h.enable,
		plugin.WithDepends("perm"),
		plugin.WithOnReload(h.reload),
	)
}

func (h *hubService) targetWorld(ctx *plugin.Context) *world.World {
	if h.worldOverride != nil {
		return h.worldOverride
	}
	return ctx.World()
}

func (h *hubService) reload(ctx *plugin.Context) error {
	loaded, err := plugin.LoadOrDefaultTOML(ctx, "hub.toml", defaultHubConfig())
	if err != nil {
		return err
	}
	h.cfg.Store(&loaded)
	return nil
}

func (h *hubService) enable(ctx *plugin.Context) {
	if err := h.reload(ctx); err != nil {
		ctx.Log().Error("load hub config", "err", err)
	}
	h.perms = plugin.MustService[*perm.Store](ctx.App())
	w := h.targetWorld(ctx)
	if w == nil {
		return
	}

	h.regions.Add(worlds.Region{
		Name:     "spawn",
		Box:      worlds.NewBox(cube.Pos{-24, 0, -24}, cube.Pos{24, 255, 24}),
		Priority: 10,
		Tags:     []string{"safe"},
	})
	h.regions.TrackWorld(ctx, w,
		func(p *player.Player, r *worlds.Region, _ *world.Tx) {
			h.enterCount.Add(1)
			if p != nil && r.HasTag("safe") {
				hud.ActionBar(p, "Entered safe zone: "+r.Name)
			}
		},
		func(p *player.Player, r *worlds.Region, _ *world.Tx) {
			h.leaveCount.Add(1)
			if p != nil && r.HasTag("safe") {
				hud.ActionBar(p, "Left safe zone: "+r.Name)
			}
		},
	)

	h.registerProtection(ctx, w)
	h.registerJoinGreeting(ctx)
	h.spawnEntities(ctx, w)
	h.registerCommands(ctx)
}

func (h *hubService) registerProtection(ctx *plugin.Context, w *world.World) {
	inSafe := func(pos cube.Pos) bool {
		for _, r := range h.regions.AtBlock(pos) {
			if r.HasTag("safe") {
				return true
			}
		}
		return false
	}
	event.OnWorld[player.BlockBreakEvent](ctx, w, event.Normal, func(ev *player.BlockBreakEvent) {
		if inSafe(ev.Pos) {
			ev.Cancel()
		}
	})
	event.OnWorld[player.BlockPlaceEvent](ctx, w, event.Normal, func(ev *player.BlockPlaceEvent) {
		if inSafe(ev.Pos) {
			ev.Cancel()
		}
	})
	event.OnWorld[player.HurtEvent](ctx, w, event.Normal, func(ev *player.HurtEvent) {
		for _, r := range h.regions.At(ev.Player.Position()) {
			if r.HasTag("safe") {
				ev.Cancel()
				return
			}
		}
	})
}

func (h *hubService) registerJoinGreeting(ctx *plugin.Context) {
	event.OnServer[player.JoinEvent](ctx, event.Normal, func(ev *player.JoinEvent) {
		c := h.cfg.Load()
		hud.NewTitle(c.WelcomeTitle).
			Subtitle(c.WelcomeSubtitle).
			Show(ev.Player)
		hud.NewScoreboard(c.SidebarTitle,
			"Player: "+ev.Player.Name(),
			"Zone: Spawn",
		).Show(ev.Player)
	})
}

func (h *hubService) spawnEntities(ctx *plugin.Context, w *world.World) {
	c := h.cfg.Load()
	npcPos := mgl64.Vec3{c.NPCPos[0], c.NPCPos[1], c.NPCPos[2]}
	holoPos := mgl64.Vec3{c.Spawn[0], c.Spawn[1] + 2.2, c.Spawn[2]}

	<-w.Exec(func(tx *world.Tx) {
		h.guide = npc.Spawn(ctx, tx, "Guide",
			npc.Position(npcPos),
			npc.ScoreTag("Right-click for Games"),
			npc.HeldItems(item.NewStack(item.Compass{}, 1), item.Stack{}),
			npc.LookAtPlayers(12),
			npc.OnInteract(func(p *player.Player, intTx *world.Tx) {
				h.modeMenu.Open(p, intTx)
			}),
			npc.OnAttack(func(p *player.Player, intTx *world.Tx) {
				h.modeMenu.Open(p, intTx)
			}),
		)
		h.holo = hud.SpawnHologram(tx, holoPos, c.WelcomeTitle, "Talk to the Guide NPC")
	})

	ctx.OnDisable(func() {
		if w != nil && h.holo != nil {
			w.Exec(func(closeTx *world.Tx) {
				h.holo.Close(closeTx)
			})
		}
	})
}

func (h *hubService) registerCommands(ctx *plugin.Context) {
	cmd.Group("hub",
		cmd.Sub("spawn", cmd.Run(func(src cmd.Source, _ []string, tx *world.Tx) {
			p, ok := src.(*player.Player)
			if !ok || tx == nil {
				return
			}
			c := h.cfg.Load()
			p.Teleport(mgl64.Vec3{c.Spawn[0], c.Spawn[1], c.Spawn[2]})
		})),
		cmd.Sub("menu", cmd.Run(func(src cmd.Source, _ []string, tx *world.Tx) {
			p, ok := src.(*player.Player)
			if ok && tx != nil {
				h.modeMenu.Open(p, tx)
			}
		})),
		cmd.Sub("reload",
			cmd.Allow(func(src cmd.Source) bool {
				p, ok := src.(*player.Player)
				if !ok {
					return true
				}
				return h.perms.PlayerHas(p, "hub.reload")
			}),
			cmd.RunOutput(func(_ cmd.Source, out *cmd.Output, _ []string, _ *world.Tx) {
				if err := ctx.App().ReloadPlugin("hub"); err != nil {
					out.Errorf("reload failed: %v", err)
					return
				}
				out.Print("Hub config reloaded.")
			}),
		),
	).Desc("Hub navigation and management").Register()
}
