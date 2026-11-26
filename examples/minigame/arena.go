package main

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/sdk/bedrock"
	"github.com/dm-vev/adamant/sdk/hud"
	sdkitem "github.com/dm-vev/adamant/sdk/item"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/schedule"
	"github.com/dm-vev/adamant/sdk/worldgen"
	"github.com/dm-vev/adamant/sdk/worlds"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/bossbar"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	blinkOnce sync.Once
	blinkItem world.CustomItem
	blinkUses atomic.Int32
)

func registerBlinkWand() world.CustomItem {
	blinkOnce.Do(func() {
		blinkItem = sdkitem.Define("minigame:blink_wand",
			sdkitem.DisplayName("Blink Wand"),
			sdkitem.MaxCount(1),
			sdkitem.Cooldown(3*time.Second),
			sdkitem.Glinted(),
			sdkitem.OnUse(func(_ *world.Tx, u item.User, _ *item.UseContext) bool {
				blinkUses.Add(1)
				if p, ok := u.(*player.Player); ok {
					hud.ActionBar(p, "Whoosh!")
				}
				return true
			}),
		)
		sdkitem.Register(blinkItem)
	})
	return blinkItem
}

func arenaPlatformStructure() *worldgen.Structure {
	var blocks []worldgen.Block
	for x := range 5 {
		for z := range 5 {
			blocks = append(blocks, worldgen.At(cube.Pos{x, 0, z}, block.Quartz{}))
		}
	}
	return worldgen.NewStructure("arena_pad", blocks...)
}

type minigameService struct {
	manager     *worlds.Manager
	arena       *world.World
	wand        world.CustomItem
	roundTicks  atomic.Int32
	leaderboard atomic.Pointer[string]
}

func newMinigameService() *minigameService {
	return &minigameService{}
}

func (m *minigameService) enable(ctx *plugin.Context) {
	m.wand = registerBlinkWand()
	m.manager = worlds.NewScopedManager(ctx)

	w, err := m.manager.Open("arena", worlds.RandomTickSpeed(-1))
	if err != nil {
		ctx.Log().Error("open arena world", "err", err)
		return
	}
	m.arena = w

	platform := arenaPlatformStructure()
	<-w.Exec(func(tx *world.Tx) {
		platform.Place(tx, cube.Pos{-2, 63, -2}, worldgen.PlaceOpts{IgnoreAir: true})
		if _, err := worlds.ForceChunk(ctx, tx, world.ChunkPos{0, 0}); err != nil {
			ctx.Log().Error("force arena chunk", "err", err)
		}
	})

	// Drive a 20-tick (1s in game loop) bossbar countdown inside the arena world.
	const totalSteps = 5
	remaining := totalSteps
	schedule.Every(ctx, w, 1, func(tx *world.Tx) {
		m.roundTicks.Add(1)
		if remaining > 0 {
			remaining--
		}
		frac := float64(remaining) / float64(totalSteps)
		bar := hud.NewBossBar(fmt.Sprintf("Round Starts: %ds", remaining)).
			ID("arena_round").
			Health(frac).
			Colour(bossbar.Purple())
		for p := range ctx.App().PlayersIn(tx) {
			bar.Show(p)
		}
	})

	// Simulate an off-thread leaderboard lookup marshalled safely back into the arena world.
	schedule.Async(ctx, func(bg context.Context) (string, error) {
		if bg.Err() != nil {
			return "", bg.Err()
		}
		return "Top Player: Alex (12 wins)", nil
	}).ThenWorld(w, func(tx *world.Tx, top string, err error) {
		if err != nil || tx == nil {
			return
		}
		m.leaderboard.Store(&top)
	})
}

// joinArena transfers p from srcTx into the minigame arena, equips the kit,
// plays a camera/fog intro with input lock, and unlocks movement after 2 ticks.
func (m *minigameService) joinArena(ctx *plugin.Context, p *player.Player, srcTx *world.Tx) bool {
	if m.arena == nil || p == nil || srcTx == nil {
		return false
	}
	h := p.H()
	if !worlds.Transfer(p, m.arena, mgl64.Vec3{0.5, 64, 0.5}, srcTx) {
		return false
	}
	<-m.arena.Exec(func(tx *world.Tx) {
		ent, ok := h.Entity(tx)
		if !ok {
			return
		}
		ap := ent.(*player.Player)
		sword := sdkitem.Stack(item.Sword{Tier: item.ToolTierDiamond}).
			Name("Sky Blade").
			Lore("Arena Starter Kit").
			Enchant(enchantment.Sharpness, 3).
			Unbreakable().
			Build()
		wandStack := sdkitem.Stack(m.wand).Name("Blink Wand").Build()
		// Place wand in the active hotbar slot first so AddItem places the kit
		// sword into the next free slot without overwriting slot 0.
		ap.SetHeldItems(wandStack, item.Stack{})
		_, _ = ap.Inventory().AddItem(sword)

		hud.LockInput(ap, hud.LockMovement())
		bedrock.CameraFade(ap, 0.2, 0.4, 0.2, color.RGBA{R: 12, G: 10, B: 22, A: 255})
		bedrock.SetFog(ap, "minecraft:fog_crimson_forest")
	})

	schedule.After(ctx, m.arena, 2, func(tx *world.Tx) {
		if ent, ok := h.Entity(tx); ok {
			ap := ent.(*player.Player)
			hud.ClearInputLocks(ap)
			bedrock.ClearFog(ap)
		}
	})
	return true
}
