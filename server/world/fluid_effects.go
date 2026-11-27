package world

import (
	"fmt"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world/chunk"
)

func (c *shapeContext) nativeFluid(fluid fluidState) Liquid {
	if fluid.kind == fluidEmpty {
		return nil
	}
	name := "minecraft:water"
	if fluid.kind == fluidLava {
		name = "minecraft:lava"
	}
	b, ok := c.tx.World().resolveScheduledTickBlock(chunk.ScheduledTick{Kind: chunk.FluidTick, Type: name})
	if !ok {
		panic(fmt.Sprintf("missing native fluid %s", name))
	}
	return b.(Liquid).WithDepth(int(fluid.amount), fluid.falling)
}

func (c *shapeContext) beforeFluidDestroy(pos cube.Pos, captured fluidBlock, fluid fluidState) {
	if captured.native == nil && stateIsAir(captured.state) {
		return
	}
	c.nativeFluid(fluid).LiquidRemoveBlock(pos, c.tx, c.tx.Block(pos))
}

func (c *shapeContext) flowEvent(src, dst cube.Pos, fluid fluidState) bool {
	event := c.tx.Event()
	c.tx.World().Handler().HandleLiquidFlow(event, src, dst, c.nativeFluid(fluid), c.tx.Block(dst))
	return !event.Cancelled()
}

func (c *shapeContext) decayEvent(pos cube.Pos, from, to fluidState) bool {
	event := c.tx.Event()
	if to.kind != fluidEmpty && (to.amount > from.amount || to.source && !from.source) {
		c.tx.World().Handler().HandleLiquidFlow(event, pos, pos, c.nativeFluid(to), c.tx.Block(pos))
	} else {
		c.tx.World().Handler().HandleLiquidDecay(event, pos, c.nativeFluid(from), c.nativeFluid(to))
	}
	return !event.Cancelled()
}

func (c *shapeContext) hardenFlow(src, dst cube.Pos, fluid fluidState, replacement uint32) bool {
	registry := c.tx.World().conf.Blocks
	native := ResolveStateID(registry, replacement)
	result := registry.BlockByRuntimeIDOrAir(native.Block)
	before, present := c.tx.Liquid(dst)
	if !present {
		panic(fmt.Sprintf("Java fluid hardening without target fluid at %v", dst))
	}
	incoming := c.nativeFluid(fluid)
	event := c.tx.Event()
	c.tx.World().Handler().HandleLiquidHarden(event, dst, before, incoming, result)
	if event.Cancelled() {
		return false
	}
	c.set(dst, replacement, 3)
	// Hardening uses the lava's fizz effect, never water's block-drop hook.
	lava := incoming
	if before.LiquidType() == "lava" {
		lava = before
	}
	lava.LiquidRemoveBlock(dst, c.tx, result)
	return true
}
