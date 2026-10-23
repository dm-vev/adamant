package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

var endFeatureSetOpts = &world.SetOpts{DisableBlockUpdates:true, DisableRedstoneUpdates:true}

// The pattern's depth axis is not assumed vertical: BlockPattern searches all
// twenty-four orthogonal forward/up pairs, including player-built rotations.
type endPortalPattern struct { origin, right, up, forward cube.Pos }
func (p endPortalPattern) block(x,y,z int) cube.Pos {
	return p.origin.Add(golemScale(p.right,x)).Sub(golemScale(p.up,y)).Add(golemScale(p.forward,z))
}

var endPortalBedrockOffsets = func() []cube.Pos {
	rows := [5][7]string{
		{"       ","       ","       ","   #   ","       ","       ","       "},
		{"       ","       ","       ","   #   ","       ","       ","       "},
		{"       ","       ","       ","   #   ","       ","       ","       "},
		{"  ###  "," #   # ","#     #","#  #  #","#     #"," #   # ","  ###  "},
		{"       ","  ###  "," ##### "," ##### "," ##### ","  ###  ","       "},
	}
	positions := make([]cube.Pos,0,48)
	for z, layer := range rows { for y, row := range layer { for x, char := range row { if char == '#' { positions = append(positions,cube.Pos{x,y,z}) } } } }
	return positions
}()

func findEndPortalPattern(tx *world.Tx, origin cube.Pos, bedrock map[cube.Pos]bool) (endPortalPattern,bool) {
	var result endPortalPattern
	for z := range 7 { for y := range 7 { for x := range 7 {
		anchor := origin.Add(cube.Pos{x,y,z})
		if forEachGolemPatternOrientation(func(right,up,forward cube.Pos) bool {
			pattern := endPortalPattern{anchor,right,up,forward}
			for _, offset := range endPortalBedrockOffsets {
				pos := pattern.block(offset[0],offset[1],offset[2])
				if pos[1] < tx.Range().Min() || pos[1] > tx.Range().Max() { return false }
				matches, known := bedrock[pos]
				if !known { _, matches = tx.Block(pos).(block.Bedrock); bedrock[pos] = matches }
				if !matches { return false }
			}
			result = pattern
			return true
		}) { return result,true }
	} } }
	return result,false
}

func (f *dragonFight) findExitPortal(tx *world.Tx) (endPortalPattern,bool) {
	bedrock := make(map[cube.Pos]bool)
	accept := func(pattern endPortalPattern) (endPortalPattern,bool) {
		if f.data.ExitPortal == nil { pos := pattern.block(3,3,3); f.data.ExitPortal = &pos }
		return pattern,true
	}
	for pos := range tx.EndPortalPositions() {
		if pattern, found := findEndPortalPattern(tx,pos,bedrock); found { return accept(pattern) }
	}
	for y := tx.JavaMotionBlockingHeight(0,0,false); y >= tx.Range().Min(); y-- {
		if pattern, found := findEndPortalPattern(tx,cube.Pos{0,y,0},bedrock); found { return accept(pattern) }
	}
	return endPortalPattern{},false
}

func (f *dragonFight) spawnExitPortal(tx *world.Tx, active bool) {
	if f.data.ExitPortal == nil {
		pos := cube.Pos{0,tx.JavaMotionBlockingHeight(0,0,true)-1,0}
		for pos[1] > 63 { if _, bedrock := tx.Block(pos).(block.Bedrock); !bedrock { break }; pos[1]-- }
		pos[1] = max(tx.Range().Min()+1,pos[1])
		f.data.ExitPortal = &pos
	}
	origin := *f.data.ExitPortal
	// BlockPos.betweenClosed advances x first, then y, then z.
	for z := -4; z <= 4; z++ { for y := -1; y <= 32; y++ { for x := -4; x <= 4; x++ {
		distance := x*x+y*y+z*z
		inside := float64(distance) < 2.5*2.5
		if !inside && float64(distance) >= 3.5*3.5 { continue }
		pos := origin.Add(cube.Pos{x,y,z})
		if pos[1] < tx.Range().Min() || pos[1] > tx.Range().Max() { continue }
		switch {
		case y < 0 && inside:
			tx.SetBlock(pos,block.Bedrock{},endFeatureSetOpts)
		case y < 0:
			setEndPodiumBlock(tx,pos,block.EndStone{},active)
		case y > 0:
			setEndPodiumBlock(tx,pos,block.Air{},active)
		case !inside:
			tx.SetBlock(pos,block.Bedrock{},endFeatureSetOpts)
		case active:
			setEndPodiumBlock(tx,pos,block.EndPortal{},true)
		default:
			tx.SetBlock(pos,block.Air{},endFeatureSetOpts)
		}
	} } }
	for y := range 4 { tx.SetBlock(origin.Add(cube.Pos{0,y,0}),block.Bedrock{},endFeatureSetOpts) }
	for _, face := range cube.HorizontalFaces() {
		tx.SetBlock(origin.Add(cube.Pos{0,2,0}).Side(face),block.Torch{Facing:face.Opposite(),Type:block.NormalFire()},endFeatureSetOpts)
	}
}

func setEndPodiumBlock(tx *world.Tx, pos cube.Pos, replacement world.Block, drop bool) {
	if drop {
		previous := tx.Block(pos)
		oldName,_ := previous.EncodeBlock()
		newName,_ := replacement.EncodeBlock()
		if oldName == newName { return }
		tx.SetBlock(pos,nil,nil)
		if breakable,ok := previous.(block.Breakable); ok {
			info := breakable.BreakInfo()
			if info.BreakHandler != nil { info.BreakHandler(pos,tx,nil) }
			for _, stack := range info.Drops(item.ToolNone{},nil) {
				if !stack.Empty() { tx.AddEntity(NewItem(world.EntitySpawnOpts{Position:pos.Vec3Centre()},stack)) }
			}
		}
	}
	tx.SetBlock(pos,replacement,endFeatureSetOpts)
}

func placeFightSpike(tx *world.Tx, spike *vanilla.EndSpike, beam cube.Pos) {
	for z := spike.Z-spike.Radius; z <= spike.Z+spike.Radius; z++ {
		for y := tx.Range().Min(); y <= spike.Height+10; y++ {
			for x := spike.X-spike.Radius; x <= spike.X+spike.Radius; x++ {
				material, mask := spike.BlockAt(x,y,z,tx.Range().Min())
				var value world.Block
				switch material {
				case vanilla.EndSpikeUnchanged, vanilla.EndSpikeBedrock, vanilla.EndSpikeFire: continue
				case vanilla.EndSpikeAir: value = block.Air{}
				case vanilla.EndSpikeObsidian: value = block.Obsidian{}
				case vanilla.EndSpikeIronBars:
					value = block.IronBars{NorthConnection:mask&1!=0,EastConnection:mask&2!=0,SouthConnection:mask&4!=0,WestConnection:mask&8!=0}
				}
				tx.SetBlock(cube.Pos{x,y,z},value,endFeatureSetOpts)
			}
		}
	}
	target := beam.Vec3()
	opts := world.EntitySpawnOpts{Position:mgl64.Vec3{float64(spike.X)+.5,float64(spike.Height)+1,float64(spike.Z)+.5}, Rotation:cube.Rotation{float64(rand.Float32()*360),0},SpawnReason:"structure",SpawnSeed:rand.Int64()}
	tx.AddEntity(NewEndCrystal(opts,EndCrystalBehaviourConfig{ShowBase:true,BeamTarget:&target,PermanentlyInvulnerable:true}))
	tx.SetBlock(cube.Pos{spike.X,spike.Height,spike.Z},block.Bedrock{},nil)
	tx.SetBlock(cube.Pos{spike.X,spike.Height+1,spike.Z},block.Fire{},nil)
}

func spawnFightGateway(tx *world.Tx, origin cube.Pos) {
	tx.AddParticle(origin.Vec3Centre(),particle.HugeExplosion{})
	tx.PlaySound(origin.Vec3Centre(),sound.Custom{Name:"portal.travel",Volume:10,Pitch:float64((1+(rand.Float32()-rand.Float32())*.2)*.7)})
	block.GenerateEndGateway(tx, origin, block.NewEndGateway())
}

func endFightPulse(tx *world.Tx) {
	tx.PlaySound(mgl64.Vec3{.5,128.5,.5},sound.Custom{Name:"mob.enderdragon.growl",Volume:64,Pitch:float64(.8+rand.Float32()*.3)})
}
