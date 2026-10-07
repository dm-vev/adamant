package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func gatewayFindOrCreateExit(tx *world.Tx, entry cube.Pos) cube.Pos {
	direction := mgl64.Vec3{float64(entry[0]), 0, float64(entry[2])}
	if length := direction.Len(); length >= 1e-4 { direction = direction.Mul(1/length) } else { direction = mgl64.Vec3{} }
	tentative := direction.Mul(1024)
	heights := make(map[world.ChunkPos]int)
	topAt := func(pos mgl64.Vec3) int {
		chunk := world.ChunkPos{int32(math.Floor(pos[0]/16)), int32(math.Floor(pos[2]/16))}
		if top, known := heights[chunk]; known { return top }
		top := tx.Range().Min()-1
		for x := range 16 { for z := range 16 {
			wx, wz := int(chunk[0])*16+x, int(chunk[1])*16+z
			y := tx.HighestBlock(wx,wz)
			if y == tx.Range().Min() { if _, air := tx.Block(cube.Pos{wx,y,wz}).(Air); air { continue } }
			top = max(top,y)
		} }
		heights[chunk] = top
		return top
	}
	for left := 16; topAt(tentative) >= tx.Range().Min() && left > 0; left-- { tentative = tentative.Sub(direction.Mul(16)) }
	for left := 16; topAt(tentative) < tx.Range().Min() && left > 0; left-- { tentative = tentative.Add(direction.Mul(16)) }
	chunkX,chunkZ := int(math.Floor(tentative[0]/16))*16,int(math.Floor(tentative[2]/16))*16
	var closest cube.Pos
	found := false
	closestDistance := 0.
	top := topAt(tentative)
	for z := range 16 { for y := 30; y <= top; y++ { for x := range 16 {
		pos := cube.Pos{chunkX+x,y,chunkZ+z}
		if _, endStone := tx.Block(pos).(EndStone); !endStone { continue }
		if gatewayFullBlock(tx,pos.Side(cube.FaceUp)) || gatewayFullBlock(tx,pos.Add(cube.Pos{0,2,0})) { continue }
		distance := pos.Vec3Centre().LenSqr()
		if !found || distance < closestDistance { closest,closestDistance,found = pos,distance,true }
	} } }
	if !found {
		closest = cube.Pos{int(math.Floor(tentative[0]+.5)),75,int(math.Floor(tentative[2]+.5))}
		gatewayIsland(tx,closest)
	}
	return gatewayTallestBlock(tx,closest,16,true)
}

func gatewayTallestBlock(tx *world.Tx, around cube.Pos, distance int, allowBedrock bool) cube.Pos {
	tallest,found := around,false
	for dx := -distance; dx <= distance; dx++ { for dz := -distance; dz <= distance; dz++ {
		if dx == 0 && dz == 0 && !allowBedrock { continue }
		minimum := tx.Range().Min()
		if found { minimum = tallest[1] }
		for y := tx.HighestBlock(around[0]+dx,around[2]+dz); y > minimum; y-- {
			pos := cube.Pos{around[0]+dx,y,around[2]+dz}
			if !allowBedrock { if _, bedrock := tx.Block(pos).(Bedrock); bedrock { continue } }
			if gatewayFullBlock(tx,pos) { tallest,found = pos,true; break }
		}
	} }
	return tallest
}

func gatewayFullBlock(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) { return false }
	return gatewayShapeCovers(mgl64.Vec3{},mgl64.Vec3{1,1,1},tx.Block(pos).Model().BBox(pos,tx))
}

// Check the union, not merely six solid faces: hollow blocks are not full
// collision cubes, while a model may partition a full cube into several boxes.
func gatewayShapeCovers(minimum, maximum mgl64.Vec3, boxes []cube.BBox) bool {
	for i, box := range boxes {
		low,high := box.Min(),box.Max()
		intersects := true
		for axis := range 3 {
			low[axis],high[axis] = max(low[axis],minimum[axis]),min(high[axis],maximum[axis])
			if low[axis] >= high[axis] { intersects = false; break }
		}
		if !intersects { continue }
		for axis := range 3 {
			if minimum[axis] < low[axis] {
				end := maximum; end[axis] = low[axis]
				if !gatewayShapeCovers(minimum,end,boxes[i+1:]) { return false }
				minimum[axis] = low[axis]
			}
			if maximum[axis] > high[axis] {
				start := minimum; start[axis] = high[axis]
				if !gatewayShapeCovers(start,maximum,boxes[i+1:]) { return false }
				maximum[axis] = high[axis]
			}
		}
		return true
	}
	return false
}

func gatewayIsland(tx *world.Tx, origin cube.Pos) {
	packed := (uint64(int64(origin[0]))&0x3ffffff)<<38 | (uint64(int64(origin[2]))&0x3ffffff)<<12 | uint64(origin[1])&0xfff
	random := java26.NewLegacyRandom(int64(packed))
	size := float32(random.NextInt(3))+4
	opts := &world.SetOpts{DisableBlockUpdates:true,DisableRedstoneUpdates:true}
	for y := 0; size > .5; y-- {
		for x := int(math.Floor(float64(-size))); x <= int(math.Ceil(float64(size))); x++ {
			for z := int(math.Floor(float64(-size))); z <= int(math.Ceil(float64(size))); z++ {
				if float32(x*x+z*z) <= (size+1)*(size+1) { tx.SetBlock(origin.Add(cube.Pos{x,y,z}),EndStone{},opts) }
			}
		}
		size -= float32(random.NextInt(2))+.5
	}
}

// GenerateEndGateway places the Java gateway frame and configured native actor.
func GenerateEndGateway(tx *world.Tx, origin cube.Pos, gateway EndGateway) {
	opts := &world.SetOpts{DisableBlockUpdates:true,DisableRedstoneUpdates:true}
	for z := -1; z <= 1; z++ { for y := -2; y <= 2; y++ { for x := -1; x <= 1; x++ {
		var value world.Block = Air{}
		switch {
		case x == 0 && y == 0 && z == 0: value = gateway
		case y == 0:
		case (y == -2 || y == 2) && x == 0 && z == 0: value = Bedrock{}
		case y != -2 && y != 2 && (x == 0 || z == 0): value = Bedrock{}
		}
		tx.SetBlock(origin.Add(cube.Pos{x,y,z}),value,opts)
	} } }
}
