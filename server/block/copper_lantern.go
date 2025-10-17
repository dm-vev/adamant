package block

import (
    "github.com/df-mc/dragonfly/server/block/cube"
    "github.com/df-mc/dragonfly/server/block/model"
    "github.com/df-mc/dragonfly/server/item"
    "github.com/df-mc/dragonfly/server/world"
    "github.com/df-mc/dragonfly/server/world/sound"
    "github.com/go-gl/mathgl/mgl64"
    "math/rand/v2"
)

// CopperLantern is a light-emitting decorative block that can oxidise and be waxed.
type CopperLantern struct {
    transparent
    sourceWaterDisplacer

    // Hanging determines if a lantern is hanging off a block.
    Hanging bool
    // Oxidation is the current oxidation level of the lantern.
    Oxidation OxidationType
    // Waxed indicates whether the lantern was waxed to stop oxidation.
    Waxed bool
}

// Model ...
func (l CopperLantern) Model() world.BlockModel {
    return model.Lantern{Hanging: l.Hanging}
}

// NeighbourUpdateTick ...
func (l CopperLantern) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
    if l.Hanging {
        up := pos.Side(cube.FaceUp)
        // Allow hanging from solid faces or chains (iron or copper).
        if _, ok := tx.Block(up).(IronChain); ok {
            return
        }
        if _, ok := tx.Block(up).(CopperChain); ok {
            return
        }
        if !tx.Block(up).Model().FaceSolid(up, cube.FaceDown, tx) {
            breakBlock(l, pos, tx)
        }
    } else {
        down := pos.Side(cube.FaceDown)
        if !tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx) {
            breakBlock(l, pos, tx)
        }
    }
}

// LightEmissionLevel ...
func (CopperLantern) LightEmissionLevel() uint8 {
    return 15
}

// UseOnBlock ...
func (l CopperLantern) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
    pos, face, used := firstReplaceable(tx, pos, face, l)
    if !used {
        return false
    }
    if face == cube.FaceDown {
        upPos := pos.Side(cube.FaceUp)
        if _, ok := tx.Block(upPos).(IronChain); !ok {
            if _, ok := tx.Block(upPos).(CopperChain); !ok && !tx.Block(upPos).Model().FaceSolid(upPos, cube.FaceDown, tx) {
                face = cube.FaceUp
            }
        }
    }
    if face != cube.FaceDown {
        downPos := pos.Side(cube.FaceDown)
        if !tx.Block(downPos).Model().FaceSolid(downPos, cube.FaceUp, tx) {
            return false
        }
    }
    l.Hanging = face == cube.FaceDown

    place(tx, pos, l, user, ctx)
    return placed(ctx)
}

// SideClosed ...
func (CopperLantern) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
    return false
}

// BreakInfo ...
func (l CopperLantern) BreakInfo() BreakInfo {
    return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(l))
}

// Wax waxes the lantern to stop it from oxidising further.
func (l CopperLantern) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
    before := l.Waxed
    l.Waxed = true
    return l, !before
}

// Strip removes wax or oxidation from the lantern.
func (l CopperLantern) Strip() (world.Block, world.Sound, bool) {
    if l.Waxed {
        l.Waxed = false
        return l, sound.WaxRemoved{}, true
    }
    if ot, ok := l.Oxidation.Decrease(); ok {
        l.Oxidation = ot
        return l, sound.CopperScraped{}, true
    }
    return l, nil, false
}

// CanOxidate returns whether the lantern can still oxidise.
func (l CopperLantern) CanOxidate() bool {
    return !l.Waxed
}

// OxidationLevel returns the current oxidation level of the lantern.
func (l CopperLantern) OxidationLevel() OxidationType {
    return l.Oxidation
}

// WithOxidationLevel returns the lantern with its oxidation level set to the given value.
func (l CopperLantern) WithOxidationLevel(o OxidationType) Oxidisable {
    l.Oxidation = o
    return l
}

// RandomTick handles the natural oxidation of the lantern.
func (l CopperLantern) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
    attemptOxidation(pos, tx, r, l)
}

// EncodeItem ...
func (l CopperLantern) EncodeItem() (name string, meta int16) {
    name = "copper_lantern"
    if l.Oxidation != UnoxidisedOxidation() {
        name = l.Oxidation.String() + "_" + name
    }
    if l.Waxed {
        name = "waxed_" + name
    }
    return "minecraft:" + name, 0
}

// EncodeBlock ...
func (l CopperLantern) EncodeBlock() (string, map[string]any) {
    name := "copper_lantern"
    if l.Oxidation != UnoxidisedOxidation() {
        name = l.Oxidation.String() + "_" + name
    }
    if l.Waxed {
        name = "waxed_" + name
    }
    return "minecraft:" + name, map[string]any{"hanging": l.Hanging}
}

// allCopperLanterns returns all possible copper lantern states.
func allCopperLanterns() (lanterns []world.Block) {
    for _, waxed := range []bool{false, true} {
        for _, oxidation := range OxidationTypes() {
            for _, hanging := range []bool{false, true} {
                lanterns = append(lanterns, CopperLantern{Hanging: hanging, Oxidation: oxidation, Waxed: waxed})
            }
        }
    }
    return
}

