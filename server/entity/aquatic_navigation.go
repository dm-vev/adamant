package entity

import (
	"container/heap"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type aquaticPathNode struct {
	pos cube.Pos
	g, f float64
	parent *aquaticPathNode
	index int
}
type aquaticPathHeap []*aquaticPathNode
func (h aquaticPathHeap) Len() int { return len(h) }
func (h aquaticPathHeap) Less(i,j int) bool { return h[i].f<h[j].f }
func (h aquaticPathHeap) Swap(i,j int) { h[i],h[j]=h[j],h[i]; h[i].index=i; h[j].index=j }
func (h *aquaticPathHeap) Push(v any) { n:=v.(*aquaticPathNode); n.index=len(*h); *h=append(*h,n) }
func (h *aquaticPathHeap) Pop() any { old:=*h; n:=len(old); v:=old[n-1]; old[n-1]=nil; v.index=-1; *h=old[:n-1]; return v }

// findAquaticPath finds a water-bound 3D route. Nodes are positions at the
// centre of a fluid block, and the whole entity volume must fit in water.
func findAquaticPath(tx *world.Tx, start, goal cube.Pos, width, height float64, maxNodes int) []cube.Pos {
	if maxNodes <= 0 || start == goal || !aquaticNodeOpen(tx,start,width,height) || !aquaticNodeOpen(tx,goal,width,height) { return nil }
	open:=aquaticPathHeap{}
	startNode:=&aquaticPathNode{pos:start,f:aquaticHeuristic(start,goal)}
	heap.Push(&open,startNode)
	index:=map[cube.Pos]*aquaticPathNode{start:startNode}
	closed:=make(map[cube.Pos]struct{},maxNodes)
	dirs:=[10]cube.Pos{{1,0,0},{-1,0,0},{0,1,0},{0,-1,0},{0,0,1},{0,0,-1},{1,0,1},{1,0,-1},{-1,0,1},{-1,0,-1}}
	visited:=0
	for open.Len()>0 && visited<maxNodes {
		current:=heap.Pop(&open).(*aquaticPathNode)
		if _,ok:=closed[current.pos];ok { continue }; closed[current.pos]=struct{}{}; visited++
		if current.pos==goal { return aquaticPathRoute(current) }
		for _, dir := range dirs {
			next := current.pos.Add(dir)
			if _, ok := closed[next]; ok || !aquaticNodeOpen(tx, next, width, height) {
				continue
			}
			cost := 1.0
			if dir[0] != 0 && dir[2] != 0 {
				if !aquaticNodeOpen(tx, current.pos.Add(cube.Pos{dir[0], 0, 0}), width, height) ||
					!aquaticNodeOpen(tx, current.pos.Add(cube.Pos{0, 0, dir[2]}), width, height) {
					continue
				}
				cost = 1.4142135623730951
			}
			g := current.g + cost
			if node, ok := index[next]; ok {
				if g < node.g {
					node.g, node.f, node.parent = g, g+aquaticHeuristic(next, goal), current
					heap.Fix(&open, node.index)
				}
				continue
			}
			node := &aquaticPathNode{pos: next, g: g, f: g + aquaticHeuristic(next, goal), parent: current}
			index[next] = node
			heap.Push(&open, node)
		}
	}
	return nil
}

func aquaticNodeOpen(tx *world.Tx, pos cube.Pos, width, height float64) bool {
	centre := pos.Vec3().Add(mgl64.Vec3{.5, 0, .5})
	minPos := cube.PosFromVec3(mgl64.Vec3{centre[0] - width/2, centre[1], centre[2] - width/2})
	maxPos := cube.PosFromVec3(mgl64.Vec3{centre[0] + width/2 - 1e-6, centre[1] + height - 1e-6, centre[2] + width/2 - 1e-6})
	for x := minPos.X(); x <= maxPos.X(); x++ {
		for y := minPos.Y(); y <= maxPos.Y(); y++ {
			for z := minPos.Z(); z <= maxPos.Z(); z++ {
				p := cube.Pos{x, y, z}
				if p.OutOfBounds(tx.Range()) {
					return false
				}
				liquid, ok := tx.Liquid(p)
				if !ok {
					return false
				}
				if _, ok = liquid.(block.Water); !ok {
					return false
				}
				if len(tx.Block(p).Model().BBox(p, tx)) != 0 {
					return false
				}
			}
		}
	}
	return true
}
func aquaticHeuristic(a,b cube.Pos) float64 { dx,dy,dz:=float64(a.X()-b.X()),float64(a.Y()-b.Y()),float64(a.Z()-b.Z());return math.Sqrt(dx*dx+dy*dy+dz*dz) }
func aquaticPathRoute(current *aquaticPathNode) []cube.Pos {
	path:=make([]cube.Pos,0,8)
	for current!=nil&&current.parent!=nil { path=append(path,current.pos);current=current.parent }
	for i,j:=0,len(path)-1;i<j;i,j=i+1,j-1 { path[i],path[j]=path[j],path[i] }
	return path
}
func aquaticSteer(b *AquaticMobBehaviour, e *Ent, tx *world.Tx, target mgl64.Vec3, speed float64) mgl64.Vec3 {
	goal:=cube.PosFromVec3(target)
	b.navTicks--
	if !b.navGoalSet||goal!=b.navGoal||b.navTicks<=0 {
		box:=e.H().Type().BBox(aquaticWorldEntity(e))
		b.navGoal,b.navGoalSet,b.navTicks=goal,true,10
		b.navPath=findAquaticPath(tx,cube.PosFromVec3(e.Position()),goal,box.Width(),box.Height(),384)
		b.navIndex=0
	}
	if b.navIndex >= len(b.navPath) {
		if cube.PosFromVec3(e.Position()) != goal {
			return mgl64.Vec3{}
		}
		delta := target.Sub(e.Position())
		if delta.LenSqr() < .16 { return mgl64.Vec3{} }
		return delta.Normalize().Mul(speed)
	}
	waypoint:=b.navPath[b.navIndex].Vec3().Add(mgl64.Vec3{.5,0,.5})
	delta:=waypoint.Sub(e.Position())
	if delta.LenSqr()<.16 {
		b.navIndex++
		if b.navIndex>=len(b.navPath) { return mgl64.Vec3{} }
		waypoint=b.navPath[b.navIndex].Vec3().Add(mgl64.Vec3{.5,0,.5})
		delta=waypoint.Sub(e.Position())
	}
	if delta.Len()==0 { return mgl64.Vec3{} }
	return delta.Normalize().Mul(speed)
}
