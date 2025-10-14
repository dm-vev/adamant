package plugin

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"log/slog"
)

type eventRegistration[T any] struct {
	plugin  string
	handler T
	id      uint64
}

type eventList[T any] struct {
	regs []eventRegistration[T]
	next uint64
}

func (l *eventList[T]) add(plugin string, handler T) uint64 {
	id := l.next
	l.next++
	l.regs = append(l.regs, eventRegistration[T]{plugin: plugin, handler: handler, id: id})
	return id
}

func (l *eventList[T]) removeByID(id uint64) {
	if len(l.regs) == 0 {
		return
	}
	regs := l.regs[:0]
	for _, reg := range l.regs {
		if reg.id == id {
			continue
		}
		regs = append(regs, reg)
	}
	l.regs = regs
}

func (l *eventList[T]) removePlugin(plugin string) {
	if len(l.regs) == 0 {
		return
	}
	regs := l.regs[:0]
	for _, reg := range l.regs {
		if reg.plugin == plugin {
			continue
		}
		regs = append(regs, reg)
	}
	l.regs = regs
}

func (l *eventList[T]) rename(oldName, newName string) {
	if oldName == newName || len(l.regs) == 0 {
		return
	}
	for i := range l.regs {
		if l.regs[i].plugin == oldName {
			l.regs[i].plugin = newName
		}
	}
}

func (l *eventList[T]) snapshot() []eventRegistration[T] {
	if len(l.regs) == 0 {
		return nil
	}
	out := make([]eventRegistration[T], len(l.regs))
	copy(out, l.regs)
	return out
}

type eventHub[S any, C any] struct {
	mu             sync.Mutex
	log            *slog.Logger
	manager        *Manager[S, C]
	player         eventList[player.Handler]
	world          eventList[world.Handler]
	inventory      eventList[inventory.Handler]
	playerChain    atomic.Value // []eventRegistration[player.Handler]
	worldChain     atomic.Value // []eventRegistration[world.Handler]
	inventoryChain atomic.Value // []eventRegistration[inventory.Handler]
}

func newEventHub[S any, C any](manager *Manager[S, C], log *slog.Logger) *eventHub[S, C] {
	if log == nil {
		log = slog.Default()
	}
	hub := &eventHub[S, C]{manager: manager, log: log.With("subsystem", "plugin.events")}
	hub.playerChain.Store([]eventRegistration[player.Handler]{})
	hub.worldChain.Store([]eventRegistration[world.Handler]{})
	hub.inventoryChain.Store([]eventRegistration[inventory.Handler]{})
	return hub
}

func (pe *eventHub[S, C]) addPlayer(plugin string, handler player.Handler) func() {
	if handler == nil {
		return func() {}
	}
	pe.mu.Lock()
	id := pe.player.add(plugin, handler)
	pe.playerChain.Store(pe.player.snapshot())
	pe.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			pe.mu.Lock()
			pe.player.removeByID(id)
			pe.playerChain.Store(pe.player.snapshot())
			pe.mu.Unlock()
		})
	}
}

func (pe *eventHub[S, C]) addWorld(plugin string, handler world.Handler) func() {
	if handler == nil {
		return func() {}
	}
	pe.mu.Lock()
	id := pe.world.add(plugin, handler)
	pe.worldChain.Store(pe.world.snapshot())
	pe.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			pe.mu.Lock()
			pe.world.removeByID(id)
			pe.worldChain.Store(pe.world.snapshot())
			pe.mu.Unlock()
		})
	}
}

func (pe *eventHub[S, C]) addInventory(plugin string, handler inventory.Handler) func() {
	if handler == nil {
		return func() {}
	}
	pe.mu.Lock()
	id := pe.inventory.add(plugin, handler)
	pe.inventoryChain.Store(pe.inventory.snapshot())
	pe.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			pe.mu.Lock()
			pe.inventory.removeByID(id)
			pe.inventoryChain.Store(pe.inventory.snapshot())
			pe.mu.Unlock()
		})
	}
}

func (pe *eventHub[S, C]) clear(plugin string) {
	pe.mu.Lock()
	pe.player.removePlugin(plugin)
	pe.world.removePlugin(plugin)
	pe.inventory.removePlugin(plugin)
	pe.playerChain.Store(pe.player.snapshot())
	pe.worldChain.Store(pe.world.snapshot())
	pe.inventoryChain.Store(pe.inventory.snapshot())
	pe.mu.Unlock()
}

func (pe *eventHub[S, C]) rename(oldName, newName string) {
	if newName == "" || oldName == newName {
		return
	}
	pe.mu.Lock()
	pe.player.rename(oldName, newName)
	pe.world.rename(oldName, newName)
	pe.inventory.rename(oldName, newName)
	pe.playerChain.Store(pe.player.snapshot())
	pe.worldChain.Store(pe.world.snapshot())
	pe.inventoryChain.Store(pe.inventory.snapshot())
	pe.mu.Unlock()
}

func (pe *eventHub[S, C]) loadPlayerChain() []eventRegistration[player.Handler] {
	if v := pe.playerChain.Load(); v != nil {
		return v.([]eventRegistration[player.Handler])
	}
	return nil
}

func (pe *eventHub[S, C]) loadWorldChain() []eventRegistration[world.Handler] {
	if v := pe.worldChain.Load(); v != nil {
		return v.([]eventRegistration[world.Handler])
	}
	return nil
}

func (pe *eventHub[S, C]) loadInventoryChain() []eventRegistration[inventory.Handler] {
	if v := pe.inventoryChain.Load(); v != nil {
		return v.([]eventRegistration[inventory.Handler])
	}
	return nil
}

func (pe *eventHub[S, C]) wrapPlayer(_ *player.Player, base player.Handler) player.Handler {
	if chain, ok := base.(*playerHandlerChain[S, C]); ok {
		base = chain.base
	}
	return &playerHandlerChain[S, C]{manager: pe, base: base}
}

func (pe *eventHub[S, C]) wrapWorld(_ *world.World, base world.Handler) world.Handler {
	if chain, ok := base.(*worldHandlerChain[S, C]); ok {
		base = chain.base
	}
	return &worldHandlerChain[S, C]{manager: pe, base: base}
}

func (pe *eventHub[S, C]) wrapInventory(_ *inventory.Inventory, base inventory.Handler) inventory.Handler {
	if chain, ok := base.(*inventoryHandlerChain[S, C]); ok {
		base = chain.base
	}
	return &inventoryHandlerChain[S, C]{manager: pe, base: base}
}

type cancellable interface {
	Cancelled() bool
}

type playerHandlerChain[S any, C any] struct {
	manager *eventHub[S, C]
	base    player.Handler
}

func (c *playerHandlerChain[S, C]) callCtx(ctx cancellable, fn func(player.Handler)) {
	for _, reg := range c.manager.loadPlayerChain() {
		handler := reg.handler
		c.manager.invoke(reg.plugin, func() {
			fn(handler)
		})
		if ctx.Cancelled() {
			return
		}
	}
	fn(c.base)
}

func (c *playerHandlerChain[S, C]) call(fn func(player.Handler)) {
	for _, reg := range c.manager.loadPlayerChain() {
		handler := reg.handler
		c.manager.invoke(reg.plugin, func() {
			fn(handler)
		})
	}
	fn(c.base)
}

func (c *playerHandlerChain[S, C]) HandleMove(ctx *player.Context, newPos mgl64.Vec3, newRot cube.Rotation) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleMove(ctx, newPos, newRot) })
}

func (c *playerHandlerChain[S, C]) HandleJump(p *player.Player) {
	c.call(func(h player.Handler) { h.HandleJump(p) })
}

func (c *playerHandlerChain[S, C]) HandleTeleport(ctx *player.Context, pos mgl64.Vec3) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleTeleport(ctx, pos) })
}

func (c *playerHandlerChain[S, C]) HandleChangeWorld(p *player.Player, before, after *world.World) {
	c.call(func(h player.Handler) { h.HandleChangeWorld(p, before, after) })
}

func (c *playerHandlerChain[S, C]) HandleToggleSprint(ctx *player.Context, after bool) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleToggleSprint(ctx, after) })
}

func (c *playerHandlerChain[S, C]) HandleToggleSneak(ctx *player.Context, after bool) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleToggleSneak(ctx, after) })
}

func (c *playerHandlerChain[S, C]) HandleChat(ctx *player.Context, message *string) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleChat(ctx, message) })
}

func (c *playerHandlerChain[S, C]) HandleFoodLoss(ctx *player.Context, from int, to *int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleFoodLoss(ctx, from, to) })
}

func (c *playerHandlerChain[S, C]) HandleHeal(ctx *player.Context, health *float64, src world.HealingSource) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleHeal(ctx, health, src) })
}

func (c *playerHandlerChain[S, C]) HandleHurt(ctx *player.Context, damage *float64, immune bool, attackImmunity *time.Duration, src world.DamageSource) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleHurt(ctx, damage, immune, attackImmunity, src) })
}

func (c *playerHandlerChain[S, C]) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	c.call(func(h player.Handler) { h.HandleDeath(p, src, keepInv) })
}

func (c *playerHandlerChain[S, C]) HandleRespawn(p *player.Player, pos *mgl64.Vec3, w **world.World) {
	c.call(func(h player.Handler) { h.HandleRespawn(p, pos, w) })
}

func (c *playerHandlerChain[S, C]) HandleSkinChange(ctx *player.Context, sk *skin.Skin) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleSkinChange(ctx, sk) })
}

func (c *playerHandlerChain[S, C]) HandleFireExtinguish(ctx *player.Context, pos cube.Pos) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleFireExtinguish(ctx, pos) })
}

func (c *playerHandlerChain[S, C]) HandleStartBreak(ctx *player.Context, pos cube.Pos) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleStartBreak(ctx, pos) })
}

func (c *playerHandlerChain[S, C]) HandleBlockBreak(ctx *player.Context, pos cube.Pos, drops *[]item.Stack, xp *int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleBlockBreak(ctx, pos, drops, xp) })
}

func (c *playerHandlerChain[S, C]) HandleBlockPlace(ctx *player.Context, pos cube.Pos, b world.Block) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleBlockPlace(ctx, pos, b) })
}

func (c *playerHandlerChain[S, C]) HandleBlockPick(ctx *player.Context, pos cube.Pos, b world.Block) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleBlockPick(ctx, pos, b) })
}

func (c *playerHandlerChain[S, C]) HandleItemUse(ctx *player.Context) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemUse(ctx) })
}

func (c *playerHandlerChain[S, C]) HandleItemUseOnBlock(ctx *player.Context, pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemUseOnBlock(ctx, pos, face, clickPos) })
}

func (c *playerHandlerChain[S, C]) HandleItemUseOnEntity(ctx *player.Context, e world.Entity) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemUseOnEntity(ctx, e) })
}

func (c *playerHandlerChain[S, C]) HandleItemRelease(ctx *player.Context, it item.Stack, dur time.Duration) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemRelease(ctx, it, dur) })
}

func (c *playerHandlerChain[S, C]) HandleItemConsume(ctx *player.Context, it item.Stack) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemConsume(ctx, it) })
}

func (c *playerHandlerChain[S, C]) HandleAttackEntity(ctx *player.Context, e world.Entity, force, height *float64, critical *bool) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleAttackEntity(ctx, e, force, height, critical) })
}

func (c *playerHandlerChain[S, C]) HandleExperienceGain(ctx *player.Context, amount *int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleExperienceGain(ctx, amount) })
}

func (c *playerHandlerChain[S, C]) HandlePunchAir(ctx *player.Context) {
	c.callCtx(ctx, func(h player.Handler) { h.HandlePunchAir(ctx) })
}

func (c *playerHandlerChain[S, C]) HandleSignEdit(ctx *player.Context, pos cube.Pos, frontSide bool, oldText, newText string) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleSignEdit(ctx, pos, frontSide, oldText, newText) })
}

func (c *playerHandlerChain[S, C]) HandleLecternPageTurn(ctx *player.Context, pos cube.Pos, oldPage int, newPage *int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleLecternPageTurn(ctx, pos, oldPage, newPage) })
}

func (c *playerHandlerChain[S, C]) HandleItemDamage(ctx *player.Context, it item.Stack, damage int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemDamage(ctx, it, damage) })
}

func (c *playerHandlerChain[S, C]) HandleItemPickup(ctx *player.Context, it *item.Stack) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemPickup(ctx, it) })
}

func (c *playerHandlerChain[S, C]) HandleHeldSlotChange(ctx *player.Context, from, to int) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleHeldSlotChange(ctx, from, to) })
}

func (c *playerHandlerChain[S, C]) HandleItemDrop(ctx *player.Context, it item.Stack) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleItemDrop(ctx, it) })
}

func (c *playerHandlerChain[S, C]) HandleTransfer(ctx *player.Context, addr *net.UDPAddr) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleTransfer(ctx, addr) })
}

func (c *playerHandlerChain[S, C]) HandleCommandExecution(ctx *player.Context, command cmd.Command, args []string) {
	c.callCtx(ctx, func(h player.Handler) { h.HandleCommandExecution(ctx, command, args) })
}

func (c *playerHandlerChain[S, C]) HandleQuit(p *player.Player) {
	c.call(func(h player.Handler) { h.HandleQuit(p) })
}

func (c *playerHandlerChain[S, C]) HandleDiagnostics(p *player.Player, d session.Diagnostics) {
	c.call(func(h player.Handler) { h.HandleDiagnostics(p, d) })
}

type worldHandlerChain[S any, C any] struct {
	manager *eventHub[S, C]
	base    world.Handler
}

func (c *worldHandlerChain[S, C]) callCtx(ctx cancellable, fn func(world.Handler)) {
	for _, reg := range c.manager.loadWorldChain() {
		handler := reg.handler
		c.manager.invoke(reg.plugin, func() {
			fn(handler)
		})
		if ctx.Cancelled() {
			return
		}
	}
	fn(c.base)
}

func (c *worldHandlerChain[S, C]) call(fn func(world.Handler)) {
	for _, reg := range c.manager.loadWorldChain() {
		handler := reg.handler
		c.manager.invoke(reg.plugin, func() {
			fn(handler)
		})
	}
	fn(c.base)
}

func (c *worldHandlerChain[S, C]) HandleLiquidFlow(ctx *world.Context, from, into cube.Pos, liquid world.Liquid, replaced world.Block) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleLiquidFlow(ctx, from, into, liquid, replaced) })
}

func (c *worldHandlerChain[S, C]) HandleLiquidDecay(ctx *world.Context, pos cube.Pos, before, after world.Liquid) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleLiquidDecay(ctx, pos, before, after) })
}

func (c *worldHandlerChain[S, C]) HandleLiquidHarden(ctx *world.Context, hardenedPos cube.Pos, liquidHardened, otherLiquid, newBlock world.Block) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleLiquidHarden(ctx, hardenedPos, liquidHardened, otherLiquid, newBlock) })
}

func (c *worldHandlerChain[S, C]) HandleSound(ctx *world.Context, s world.Sound, pos mgl64.Vec3) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleSound(ctx, s, pos) })
}

func (c *worldHandlerChain[S, C]) HandleFireSpread(ctx *world.Context, from, to cube.Pos) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleFireSpread(ctx, from, to) })
}

func (c *worldHandlerChain[S, C]) HandleBlockBurn(ctx *world.Context, pos cube.Pos) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleBlockBurn(ctx, pos) })
}

func (c *worldHandlerChain[S, C]) HandleCropTrample(ctx *world.Context, pos cube.Pos) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleCropTrample(ctx, pos) })
}

func (c *worldHandlerChain[S, C]) HandleLeavesDecay(ctx *world.Context, pos cube.Pos) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleLeavesDecay(ctx, pos) })
}

func (c *worldHandlerChain[S, C]) HandleEntitySpawn(tx *world.Tx, e world.Entity) {
	c.call(func(h world.Handler) { h.HandleEntitySpawn(tx, e) })
}

func (c *worldHandlerChain[S, C]) HandleEntityDespawn(tx *world.Tx, e world.Entity) {
	c.call(func(h world.Handler) { h.HandleEntityDespawn(tx, e) })
}

func (c *worldHandlerChain[S, C]) HandleExplosion(ctx *world.Context, position mgl64.Vec3, entities *[]world.Entity, blocks *[]cube.Pos, itemDropChance *float64, spawnFire *bool) {
	c.callCtx(ctx, func(h world.Handler) { h.HandleExplosion(ctx, position, entities, blocks, itemDropChance, spawnFire) })
}

func (c *worldHandlerChain[S, C]) HandleClose(tx *world.Tx) {
	c.call(func(h world.Handler) { h.HandleClose(tx) })
}

type inventoryHandlerChain[S any, C any] struct {
	manager *eventHub[S, C]
	base    inventory.Handler
}

func (c *inventoryHandlerChain[S, C]) callCtx(ctx cancellable, fn func(inventory.Handler)) {
	for _, reg := range c.manager.loadInventoryChain() {
		handler := reg.handler
		c.manager.invoke(reg.plugin, func() {
			fn(handler)
		})
		if ctx.Cancelled() {
			return
		}
	}
	fn(c.base)
}

func (c *inventoryHandlerChain[S, C]) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	c.callCtx(ctx, func(h inventory.Handler) { h.HandleTake(ctx, slot, it) })
}

func (c *inventoryHandlerChain[S, C]) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	c.callCtx(ctx, func(h inventory.Handler) { h.HandlePlace(ctx, slot, it) })
}

func (c *inventoryHandlerChain[S, C]) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	c.callCtx(ctx, func(h inventory.Handler) { h.HandleDrop(ctx, slot, it) })
}

func (pe *eventHub[S, C]) invoke(plugin string, call func()) {
	if call == nil {
		return
	}
	if plugin == "" {
		call()
		return
	}
	defer func() {
		if r := recover(); r != nil {
			pe.manager.handlePluginPanic(plugin, r)
		}
	}()
	call()
}
