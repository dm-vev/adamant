package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// modernMonumentChild describes a Java child piece before its bounding box is
// moved by the MonumentBuilding's getWorldPos(9, 0, 22) offset.
type modernMonumentChild struct {
 kind modernMonumentChildKind
 room int
 design int
}

type modernMonumentChildKind uint8

const (
 monumentChildCore modernMonumentChildKind = iota
 monumentChildDoubleX
 monumentChildDoubleXY
 monumentChildDoubleY
 monumentChildDoubleYZ
 monumentChildDoubleZ
 monumentChildEntry
 monumentChildSimple
 monumentChildSimpleTop
 monumentChildWing
 monumentChildPenthouse
)

// fitModernMonumentRooms reproduces the Java fitter ordering and claim
// transitions. The graph builder already claims the source room and core's
// eight-room volume. Room children are returned in room-definition order,
// followed by the left wing, right wing, and penthouse.
func fitModernMonumentRooms(piece *modernOceanMonumentPiece, random *java26.Random) []modernMonumentChild {
 children := make([]modernMonumentChild, 0, 49)
 children = append(children,
  modernMonumentChild{kind: monumentChildEntry, room: 2},
  modernMonumentChild{kind: monumentChildCore, room: piece.core},
 )
 for _, index := range piece.roomOrder {
  room := &piece.rooms[index]
  if room.index < 0 || room.claimed || room.index >= 1000 {
   continue
  }
  east := room.connections[monumentEast]
  up := room.connections[monumentUp]
  north := room.connections[monumentNorth]
  var kind modernMonumentChildKind
  switch {
  case room.hasOpening[monumentEast] && east >= 0 && !piece.rooms[east].claimed && room.hasOpening[monumentUp] && up >= 0 && !piece.rooms[up].claimed && piece.rooms[east].hasOpening[monumentUp] && piece.rooms[east].connections[monumentUp] >= 0 && !piece.rooms[piece.rooms[east].connections[monumentUp]].claimed:
   kind = monumentChildDoubleXY
   room.claimed = true
   piece.rooms[east].claimed = true
   piece.rooms[up].claimed = true
   piece.rooms[piece.rooms[east].connections[monumentUp]].claimed = true
  case room.hasOpening[monumentNorth] && north >= 0 && !piece.rooms[north].claimed && room.hasOpening[monumentUp] && up >= 0 && !piece.rooms[up].claimed && piece.rooms[north].hasOpening[monumentUp] && piece.rooms[north].connections[monumentUp] >= 0 && !piece.rooms[piece.rooms[north].connections[monumentUp]].claimed:
   kind = monumentChildDoubleYZ
   room.claimed = true
   piece.rooms[north].claimed = true
   piece.rooms[up].claimed = true
   piece.rooms[piece.rooms[north].connections[monumentUp]].claimed = true
  case room.hasOpening[monumentNorth] && north >= 0 && !piece.rooms[north].claimed:
   kind = monumentChildDoubleZ
   room.claimed = true
   piece.rooms[north].claimed = true
  case room.hasOpening[monumentEast] && east >= 0 && !piece.rooms[east].claimed:
   kind = monumentChildDoubleX
   room.claimed = true
   piece.rooms[east].claimed = true
  case room.hasOpening[monumentUp] && up >= 0 && !piece.rooms[up].claimed:
   kind = monumentChildDoubleY
   room.claimed = true
   piece.rooms[up].claimed = true
  case !room.hasOpening[monumentWest] && !room.hasOpening[monumentEast] && !room.hasOpening[monumentNorth] && !room.hasOpening[monumentSouth] && !room.hasOpening[monumentUp]:
   kind = monumentChildSimpleTop
   room.claimed = true
  default:
   kind = monumentChildSimple
   room.claimed = true
  }
  child := modernMonumentChild{kind: kind, room: index}
  if kind == monumentChildSimple {
   child.design = int(random.NextInt(3))
  }
  children = append(children, child)
 }
 // Java draws one signed int, then increments it for the second wing; the
 // wing constructor masks each value to one bit.
 wingRandom := random.NextInt32()
 children = append(children,
  modernMonumentChild{kind: monumentChildWing, room: 1001, design: int(wingRandom & 1)},
  modernMonumentChild{kind: monumentChildWing, room: 1002, design: int((wingRandom + 1) & 1)},
  modernMonumentChild{kind: monumentChildPenthouse, room: 1003},
 )
 return children
}

// modernMonumentChildRoomDimensions returns the room-grid dimensions used by
// Java's makeBoundingBox for each room child. Wings and penthouse instead use
// the shell's independently-created bounding boxes.
func modernMonumentChildRoomDimensions(kind modernMonumentChildKind) (width, height, depth int) {
 switch kind {
 case monumentChildCore:
  return 2, 2, 2
 case monumentChildDoubleX:
  return 2, 1, 1
 case monumentChildDoubleXY:
  return 2, 2, 1
 case monumentChildDoubleY:
  return 1, 2, 1
 case monumentChildDoubleYZ:
  return 1, 2, 2
 case monumentChildDoubleZ:
  return 1, 1, 2
 default:
  return 1, 1, 1
 }
}
// modernMonumentRoomBlockSink receives child-piece local coordinates. The
// caller applies the child's Java bounding box/orientation transform and clip.
type modernMonumentRoomBlockSink func(x, y, z int32, state string)

// emitModernMonumentChild emits exact child local geometry. get must observe
// prior shell/child writes; spawn receives child-local entity coordinates.
func emitModernMonumentChild(piece *modernOceanMonumentPiece, child modernMonumentChild, random *java26.Random, get func(x, y, z int32) string, put modernMonumentRoomBlockSink, spawn func(x, y, z int32)) {
 gray, light, black, lamp := "minecraft:prismarine", "minecraft:prismarine_bricks", "minecraft:dark_prismarine", "minecraft:sea_lantern"
 box := func(x0,y0,z0,x1,y1,z1 int32,state string) {
  for y:=y0;y<=y1;y++ { for x:=x0;x<=x1;x++ { for z:=z0;z<=z1;z++ { put(x,y,z,state) } } }
 }
 water := func(x0,y0,z0,x1,y1,z1 int32) {
  for y:=y0;y<=y1;y++ { for x:=x0;x<=x1;x++ { for z:=z0;z<=z1;z++ {
   old:=get(x,y,z); if old=="minecraft:ice"||old=="minecraft:packed_ice"||old=="minecraft:blue_ice"||old=="minecraft:water" { continue }
   state:="minecraft:water";building:=modernMonumentChildToBuilding(piece,child,x,y,z);if building[1]+39>=63 { state="minecraft:air" }; put(x,y,z,state)
  } } }
 }
 fill := func(x0,y0,z0,x1,y1,z1 int32,state string) {
  for y:=y0;y<=y1;y++ { for x:=x0;x<=x1;x++ { for z:=z0;z<=z1;z++ { if get(x,y,z)=="minecraft:water" { put(x,y,z,state) } } } }
 }
 floor := func(xoff,zoff int32,opening bool) {
  if !opening { box(xoff,0,zoff,xoff+7,0,zoff+7,gray); return }
  box(xoff,0,zoff,xoff+2,0,zoff+7,gray); box(xoff+5,0,zoff,xoff+7,0,zoff+7,gray)
  box(xoff+3,0,zoff,xoff+4,0,zoff+2,gray); box(xoff+3,0,zoff+5,xoff+4,0,zoff+7,gray)
  box(xoff+3,0,zoff+2,xoff+4,0,zoff+2,light); box(xoff+3,0,zoff+5,xoff+4,0,zoff+5,light)
  box(xoff+2,0,zoff+3,xoff+2,0,zoff+4,light); box(xoff+5,0,zoff+3,xoff+5,0,zoff+4,light)
 }
 switch child.kind {
 case monumentChildEntry:
  r:=piece.rooms[child.room]
  box(0,3,0,2,3,7,light); box(5,3,0,7,3,7,light)
  box(0,2,0,1,2,7,light); box(6,2,0,7,2,7,light)
  box(0,1,0,0,1,7,light); box(7,1,0,7,1,7,light)
  box(0,1,7,7,3,7,light); box(1,1,0,2,3,0,light); box(5,1,0,6,3,0,light)
  if r.hasOpening[monumentNorth] { water(3,1,7,4,2,7) }
  if r.hasOpening[monumentWest] { water(0,1,3,1,2,4) }
  if r.hasOpening[monumentEast] { water(6,1,3,7,2,4) }
 case monumentChildCore:
  fill(1,8,0,14,8,14,gray)
  box(0,7,0,0,7,15,light); box(15,7,0,15,7,15,light); box(1,7,0,15,7,0,light); box(1,7,15,14,7,15,light)
  for y:=int32(1);y<=6;y++ { s:=light; if y==2||y==6 {s=gray}
   for _,x:=range []int32{0,15} { box(x,y,0,x,y,1,s); box(x,y,6,x,y,9,s); box(x,y,14,x,y,15,s) }
   box(1,y,0,1,y,0,s); box(6,y,0,9,y,0,s); box(14,y,0,14,y,0,s); box(1,y,15,14,y,15,s)
  }
  box(6,3,6,9,6,9,black); box(7,4,7,8,5,8,"minecraft:gold_block")
  for _,y:=range []int32{3,6} { for _,x:=range []int32{6,9} { put(x,y,6,lamp); put(x,y,9,lamp) } }
  for _,v:=range [][6]int32{{5,1,6,5,2,6},{5,1,9,5,2,9},{10,1,6,10,2,6},{10,1,9,10,2,9},{6,1,5,6,2,5},{9,1,5,9,2,5},{6,1,10,6,2,10},{9,1,10,9,2,10},{5,2,5,5,6,5},{5,2,10,5,6,10},{10,2,5,10,6,5},{10,2,10,10,6,10},{5,7,1,5,7,6},{10,7,1,10,7,6},{5,7,9,5,7,14},{10,7,9,10,7,14},{1,7,5,6,7,5},{1,7,10,6,7,10},{9,7,5,14,7,5},{9,7,10,14,7,10},{2,1,2,2,1,3},{3,1,2,3,1,2},{13,1,2,13,1,3},{12,1,2,12,1,2},{2,1,12,2,1,13},{3,1,13,3,1,13},{13,1,12,13,1,13},{12,1,13,12,1,13}} { box(v[0],v[1],v[2],v[3],v[4],v[5],light) }
 case monumentChildSimpleTop:
  r:=piece.rooms[child.room]
  if r.index/25>0 { floor(0,0,r.hasOpening[monumentDown]) }
  if r.connections[monumentUp]<0 { fill(1,4,1,6,4,6,gray) }
  for x:=int32(1);x<=6;x++ { for z:=int32(1);z<=6;z++ { if random.NextInt(3)!=0 { y:=int32(3);if random.NextInt(4)==0 {y=2};box(x,y,z,x,3,z,"minecraft:wet_sponge") } } }
  box(0,1,0,0,1,7,light); box(7,1,0,7,1,7,light); box(1,1,0,6,1,0,light); box(1,1,7,6,1,7,light)
  box(0,2,0,0,2,7,black); box(7,2,0,7,2,7,black); box(1,2,0,6,2,0,black); box(1,2,7,6,2,7,black)
  box(0,3,0,0,3,7,light); box(7,3,0,7,3,7,light); box(1,3,0,6,3,0,light); box(1,3,7,6,3,7,light)
  box(0,1,3,0,2,4,black); box(7,1,3,7,2,4,black); box(3,1,0,4,2,0,black); box(3,1,7,4,2,7,black)
  if r.hasOpening[monumentSouth] { water(3,1,0,4,2,0) }
 case monumentChildPenthouse:
  box(2,-1,2,11,-1,11,light); box(0,-1,0,1,-1,11,gray); box(12,-1,0,13,-1,11,gray); box(2,-1,0,11,-1,1,gray); box(2,-1,12,11,-1,13,gray)
  box(0,0,0,0,0,13,light); box(13,0,0,13,0,13,light); box(1,0,0,12,0,0,light); box(1,0,13,12,0,13,light)
  for _,i:=range []int32{2,5,8,11} { put(0,0,i,lamp); put(13,0,i,lamp); put(i,0,0,lamp) }
  box(2,0,3,4,0,9,light); box(9,0,3,11,0,9,light); box(4,0,9,9,0,11,light)
  for _,v:=range [][3]int32{{5,0,8},{8,0,8},{10,0,10},{3,0,10}} {put(v[0],v[1],v[2],light)}
  box(3,0,3,3,0,7,black); box(10,0,3,10,0,7,black); box(6,0,10,7,0,10,black)
  for _,x:=range []int32{3,10} { for _,z:=range []int32{2,5,8} {box(x,0,z,x,2,z,light)} }
  box(5,0,10,5,2,10,light); box(8,0,10,8,2,10,light); box(6,-1,7,7,-1,8,black); water(6,-1,3,7,-1,4); spawn(6,1,6)
 case monumentChildDoubleX:
  r:=piece.rooms[child.room]; e:=piece.rooms[r.connections[monumentEast]]
  if r.index/25>0 { floor(0,0,r.hasOpening[monumentDown]); floor(8,0,e.hasOpening[monumentDown]) }
  if r.connections[monumentUp]<0 {fill(1,4,1,7,4,6,gray)}
  if e.connections[monumentUp]<0 {fill(8,4,1,14,4,6,gray)}
  for _,v:=range [][6]int32{{0,3,0,0,3,7},{15,3,0,15,3,7},{1,3,0,15,3,0},{1,3,7,14,3,7}} {box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
  for _,v:=range [][6]int32{{0,2,0,0,2,7},{15,2,0,15,2,7},{1,2,0,15,2,0},{1,2,7,14,2,7}} {box(v[0],v[1],v[2],v[3],v[4],v[5],gray)}
  for _,v:=range [][6]int32{{0,1,0,0,1,7},{15,1,0,15,1,7},{1,1,0,15,1,0},{1,1,7,14,1,7},{5,1,0,10,1,4},{5,3,0,10,3,4}} {box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
  box(6,2,0,9,2,3,gray); put(6,2,3,lamp);put(9,2,3,lamp)
  if r.hasOpening[monumentSouth] { water(3,1,0,4,2,0) }
  if r.hasOpening[monumentNorth] { water(3,1,7,4,2,7) }
  if r.hasOpening[monumentWest] { water(0,1,3,0,2,4) }
  if e.hasOpening[monumentSouth] { water(11,1,0,12,2,0) }
  if e.hasOpening[monumentNorth] { water(11,1,7,12,2,7) }
  if e.hasOpening[monumentEast] { water(15,1,3,15,2,4) }
 case monumentChildDoubleZ:
  r:=piece.rooms[child.room];n:=piece.rooms[r.connections[monumentNorth]]
  if r.index/25>0 {floor(0,0,r.hasOpening[monumentDown]);floor(0,8,n.hasOpening[monumentDown])}
  if r.connections[monumentUp]<0 {fill(1,4,1,6,4,7,gray)};if n.connections[monumentUp]<0 {fill(1,4,8,6,4,14,gray)}
  for _,y:=range []int32{1,3} {s:=light;if y==3{s=light};box(0,y,0,0,y,15,s);box(7,y,0,7,y,15,s);box(1,y,0,7,y,0,s);box(1,y,15,6,y,15,s)}
  for _,v:=range [][6]int32{{0,2,0,0,2,15},{7,2,0,7,2,15},{1,2,0,7,2,0},{1,2,15,6,2,15},{1,1,1,1,1,2},{6,1,1,6,1,2},{1,3,1,1,3,2},{6,3,1,6,3,2},{1,1,13,1,1,14},{6,1,13,6,1,14},{1,3,13,1,3,14},{6,3,13,6,3,14},{2,1,6,2,3,6},{5,1,6,5,3,6},{2,1,9,2,3,9},{5,1,9,5,3,9},{3,2,6,4,2,6},{3,2,9,4,2,9},{2,2,7,2,2,8},{5,2,7,5,2,8}} {s:=light;if v[1]==2{s=gray};box(v[0],v[1],v[2],v[3],v[4],v[5],s)}
  for _,v:=range [][3]int32{{2,2,5},{5,2,5},{2,2,10},{5,2,10}} {put(v[0],v[1],v[2],lamp)};for _,v:=range [][3]int32{{2,3,5},{5,3,5},{2,3,10},{5,3,10}} {put(v[0],v[1],v[2],light)}
  if r.hasOpening[monumentSouth]{water(3,1,0,4,2,0)};if r.hasOpening[monumentEast]{water(7,1,3,7,2,4)};if r.hasOpening[monumentWest]{water(0,1,3,0,2,4)}
  if n.hasOpening[monumentNorth]{water(3,1,15,4,2,15)};if n.hasOpening[monumentWest]{water(0,1,11,0,2,12)};if n.hasOpening[monumentEast]{water(7,1,11,7,2,12)}
 case monumentChildDoubleY:
  r:=piece.rooms[child.room];a:=piece.rooms[r.connections[monumentUp]]
  if r.index/25>0 {floor(0,0,r.hasOpening[monumentDown])};if a.connections[monumentUp]<0{fill(1,8,1,6,8,6,gray)}
  for _,y:=range []int32{4} {box(0,y,0,0,y,7,light);box(7,y,0,7,y,7,light);box(1,y,0,6,y,0,light);box(1,y,7,6,y,7,light)}
  for _,v:=range [][6]int32{{2,4,1,2,4,2},{1,4,2,1,4,2},{5,4,1,5,4,2},{6,4,2,6,4,2},{2,4,5,2,4,6},{1,4,5,1,4,5},{5,4,5,5,4,6},{6,4,5,6,4,5}}{box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
  rIndex:=child.room
  for _,y:=range []int32{1,5} {dd:=&piece.rooms[rIndex]
   if dd.hasOpening[monumentSouth]{box(2,y,0,2,y+2,0,light);box(5,y,0,5,y+2,0,light);box(3,y+2,0,4,y+2,0,light)}else{box(0,y,0,7,y+2,0,light);box(0,y+1,0,7,y+1,0,gray)}
   if dd.hasOpening[monumentNorth]{box(2,y,7,2,y+2,7,light);box(5,y,7,5,y+2,7,light);box(3,y+2,7,4,y+2,7,light)}else{box(0,y,7,7,y+2,7,light);box(0,y+1,7,7,y+1,7,gray)}
   if dd.hasOpening[monumentWest]{box(0,y,2,0,y+2,2,light);box(0,y,5,0,y+2,5,light);box(0,y+2,3,0,y+2,4,light)}else{box(0,y,0,0,y+2,7,light);box(0,y+1,0,0,y+1,7,gray)}
   if dd.hasOpening[monumentEast]{box(7,y,2,7,y+2,2,light);box(7,y,5,7,y+2,5,light);box(7,y+2,3,7,y+2,4,light)}else{box(7,y,0,7,y+2,7,light);box(7,y+1,0,7,y+1,7,gray)}
   rIndex=piece.rooms[rIndex].connections[monumentUp]
  }
 case monumentChildDoubleXY:
  r:=piece.rooms[child.room];e:=piece.rooms[r.connections[monumentEast]];u:=piece.rooms[r.connections[monumentUp]];eu:=piece.rooms[e.connections[monumentUp]]
  if r.index/25>0 {floor(0,0,r.hasOpening[monumentDown]);floor(8,0,e.hasOpening[monumentDown])}
  if u.connections[monumentUp]<0{fill(1,8,1,7,8,6,gray)};if eu.connections[monumentUp]<0{fill(8,8,1,14,8,6,gray)}
  for y:=int32(1);y<=7;y++ {s:=light;if y==2||y==6{s=gray};box(0,y,0,0,y,7,s);box(15,y,0,15,y,7,s);box(1,y,0,15,y,0,s);box(1,y,7,14,y,7,s)}
  for _,v:=range [][6]int32{{2,1,3,2,7,4},{3,1,2,4,7,2},{3,1,5,4,7,5},{13,1,3,13,7,4},{11,1,2,12,7,2},{11,1,5,12,7,5},{5,1,3,5,3,4},{10,1,3,10,3,4},{5,7,2,10,7,5},{5,5,2,5,7,2},{10,5,2,10,7,2},{5,5,5,5,7,5},{10,5,5,10,7,5},{5,4,3,6,4,4},{9,4,3,10,4,4}} {box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
  for _,v:=range [][3]int32{{6,6,2},{9,6,2},{6,6,5},{9,6,5}}{put(v[0],v[1],v[2],light)}
  for _,v:=range [][3]int32{{5,4,2},{5,4,5},{10,4,2},{10,4,5}}{put(v[0],v[1],v[2],lamp)}
  for _,rr:=range []struct{room *modernMonumentRoom; zoff,yoff int32; east bool}{{&r,0,0,false},{&e,0,0,true},{&u,0,4,false},{&eu,0,4,true}} {
   rd:=rr.room
   if rd.hasOpening[monumentSouth] {water(3,1+rr.yoff,0,4,2+rr.yoff,0)}
   if rd.hasOpening[monumentNorth] {water(3,1+rr.yoff,7,4,2+rr.yoff,7)}
   if rd.hasOpening[monumentWest] {water(0,1+rr.yoff,3,0,2+rr.yoff,4)}
   if rr.east&&rd.hasOpening[monumentEast] {water(15,1+rr.yoff,3,15,2+rr.yoff,4)}
  }
 case monumentChildDoubleYZ:
  r:=piece.rooms[child.room];n:=piece.rooms[r.connections[monumentNorth]];u:=piece.rooms[r.connections[monumentUp]];nu:=piece.rooms[n.connections[monumentUp]]
  if r.index/25>0 {floor(0,0,r.hasOpening[monumentDown]);floor(0,8,n.hasOpening[monumentDown])}
  if u.connections[monumentUp]<0{fill(1,8,1,6,8,7,gray)};if nu.connections[monumentUp]<0{fill(1,8,8,6,8,14,gray)}
  for y:=int32(1);y<=7;y++ {s:=light;if y==2||y==6{s=gray};box(0,y,0,0,y,15,s);box(7,y,0,7,y,15,s);box(1,y,0,6,y,0,s);box(1,y,15,6,y,15,s)}
  for y:=int32(1);y<=7;y++ {s:=black;if y==2||y==6{s=lamp};box(3,y,7,4,y,8,s)}
  if r.hasOpening[monumentSouth]{water(3,1,0,4,2,0)};if r.hasOpening[monumentEast]{water(7,1,3,7,2,4)};if r.hasOpening[monumentWest]{water(0,1,3,0,2,4)}
  if n.hasOpening[monumentNorth]{water(3,1,15,4,2,15)};if n.hasOpening[monumentWest]{water(0,1,11,0,2,12)};if n.hasOpening[monumentEast]{water(7,1,11,7,2,12)}
  if u.hasOpening[monumentEast]{water(7,5,3,7,6,4);box(5,4,2,6,4,5,light);box(6,1,2,6,3,2,light);box(6,1,5,6,3,5,light)}
  if u.hasOpening[monumentWest]{water(0,5,3,0,6,4);box(1,4,2,2,4,5,light);box(1,1,2,1,3,2,light);box(1,1,5,1,3,5,light)}
  if u.hasOpening[monumentSouth]{water(3,5,0,4,6,0)}
  if nu.hasOpening[monumentNorth]{water(3,5,15,4,6,15)}
  if nu.hasOpening[monumentWest]{water(0,5,11,0,6,12);box(1,4,10,2,4,13,light);box(1,1,10,1,3,10,light);box(1,1,13,1,3,13,light)}
  if nu.hasOpening[monumentEast]{water(7,5,11,7,6,12);box(5,4,10,6,4,13,light);box(6,1,10,6,3,10,light);box(6,1,13,6,3,13,light)}
 case monumentChildSimple:
  r:=piece.rooms[child.room];if r.index/25>0{floor(0,0,r.hasOpening[monumentDown])};if r.connections[monumentUp]<0{fill(1,4,1,6,4,6,gray)}
  count:=0;for _,v:=range r.hasOpening{if v{count++}}
  center:=child.design!=0&&random.NextBoolean()&&!r.hasOpening[monumentDown]&&!r.hasOpening[monumentUp]&&count>1
  if child.design==0 {
   for _,v:=range [][6]int32{{0,1,0,2,1,2},{0,3,0,2,3,2},{0,2,0,0,2,2},{1,2,0,2,2,0},{5,1,0,7,1,2},{5,3,0,7,3,2},{7,2,0,7,2,2},{5,2,0,6,2,0},{0,1,5,2,1,7},{0,3,5,2,3,7},{0,2,5,0,2,7},{1,2,7,2,2,7},{5,1,5,7,1,7},{5,3,5,7,3,7},{7,2,5,7,2,7},{5,2,7,6,2,7}}{s:=light;if v[1]==2{s=gray};box(v[0],v[1],v[2],v[3],v[4],v[5],s)}
   for _,v:=range [][3]int32{{1,2,1},{6,2,1},{1,2,6},{6,2,6}}{put(v[0],v[1],v[2],lamp)}
   if r.hasOpening[monumentSouth]{box(3,3,0,4,3,0,light)}else{box(3,3,0,4,3,1,light);box(3,2,0,4,2,0,gray);box(3,1,0,4,1,1,light)}
   if r.hasOpening[monumentNorth]{box(3,3,7,4,3,7,light)}else{box(3,3,6,4,3,7,light);box(3,2,7,4,2,7,gray);box(3,1,6,4,1,7,light)}
   if r.hasOpening[monumentWest]{box(0,3,3,0,3,4,light)}else{box(0,3,3,1,3,4,light);box(0,2,3,0,2,4,gray);box(0,1,3,1,1,4,light)}
   if r.hasOpening[monumentEast]{box(7,3,3,7,3,4,light)}else{box(6,3,3,7,3,4,light);box(7,2,3,7,2,4,gray);box(6,1,3,7,1,4,light)}
  } else if child.design==1 {
   for _,v:=range [][6]int32{{2,1,2,2,3,2},{2,1,5,2,3,5},{5,1,5,5,3,5},{5,1,2,5,3,2},{0,1,0,1,3,0},{0,1,1,0,3,1},{0,1,7,1,3,7},{0,1,6,0,3,6},{6,1,7,7,3,7},{7,1,6,7,3,6},{6,1,0,7,3,0},{7,1,1,7,3,1}}{box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
   for _,v:=range [][3]int32{{2,2,2},{2,2,5},{5,2,5},{5,2,2}}{put(v[0],v[1],v[2],lamp)}
   for _,v:=range [][3]int32{{1,2,0},{0,2,1},{1,2,7},{0,2,6},{6,2,7},{7,2,6},{6,2,0},{7,2,1}}{put(v[0],v[1],v[2],gray)}
   if !r.hasOpening[monumentSouth]{box(1,3,0,6,3,0,light);box(1,2,0,6,2,0,gray);box(1,1,0,6,1,0,light)}
   if !r.hasOpening[monumentNorth]{box(1,3,7,6,3,7,light);box(1,2,7,6,2,7,gray);box(1,1,7,6,1,7,light)}
   if !r.hasOpening[monumentWest]{box(0,3,1,0,3,6,light);box(0,2,1,0,2,6,gray);box(0,1,1,0,1,6,light)}
   if !r.hasOpening[monumentEast]{box(7,3,1,7,3,6,light);box(7,2,1,7,2,6,gray);box(7,1,1,7,1,6,light)}
  } else {
   for _,v:=range [][6]int32{{0,1,0,0,1,7},{7,1,0,7,1,7},{1,1,0,6,1,0},{1,1,7,6,1,7}}{box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
   for _,v:=range [][6]int32{{0,2,0,0,2,7},{7,2,0,7,2,7},{1,2,0,6,2,0},{1,2,7,6,2,7}}{box(v[0],v[1],v[2],v[3],v[4],v[5],black)}
   for _,v:=range [][6]int32{{0,3,0,0,3,7},{7,3,0,7,3,7},{1,3,0,6,3,0},{1,3,7,6,3,7},{0,1,3,0,2,4},{7,1,3,7,2,4},{3,1,0,4,2,0},{3,1,7,4,2,7}}{box(v[0],v[1],v[2],v[3],v[4],v[5],light)}
   for _,d:=range []int{monumentSouth,monumentNorth,monumentWest,monumentEast}{if r.hasOpening[d]{switch d{case monumentSouth:water(3,1,0,4,2,0);case monumentNorth:water(3,1,7,4,2,7);case monumentWest:water(0,1,3,0,2,4);case monumentEast:water(7,1,3,7,2,4)}}}
  }
  if center {box(3,1,3,4,1,4,light);box(3,2,3,4,2,4,gray);box(3,3,3,4,3,4,light)}
 case monumentChildWing:
  if child.design==0 {
   for i:=int32(0);i<4;i++{box(10-i,3-i,20-i,12+i,3-i,20,light)}
   box(7,0,6,15,0,16,light);box(6,0,6,6,3,20,light);box(16,0,6,16,3,20,light);box(7,1,7,7,1,20,light);box(15,1,7,15,1,20,light);box(7,1,6,9,3,6,light);box(13,1,6,15,3,6,light);box(8,1,7,9,1,7,light);box(13,1,7,14,1,7,light);box(9,0,5,13,0,5,light);box(10,0,7,12,0,7,black);box(8,0,10,8,0,12,black);box(14,0,10,14,0,12,black)
   for z:=int32(18);z>=7;z-=3{put(6,3,z,lamp);put(16,3,z,lamp)}
   for _,v:=range [][3]int32{{10,0,10},{12,0,10},{10,0,12},{12,0,12},{8,3,6},{14,3,6}}{put(v[0],v[1],v[2],lamp)}
   for _,x:=range []int32{4,18}{for _,z:=range []int32{4,18}{put(x,2,z,light);put(x,1,z,lamp);put(x,0,z,light)}}
   put(9,7,20,light);put(13,7,20,light);box(6,0,21,7,4,21,light);box(15,0,21,16,4,21,light);spawn(11,2,16)
  } else {
   box(9,3,18,13,3,20,light);box(9,0,18,9,2,18,light);box(13,0,18,13,2,18,light)
   for _,x:=range []int32{9,13}{put(x,6,20,light);put(x,5,20,lamp);put(x,4,20,light)}
   box(7,3,7,15,3,14,light)
   for _,x:=range []int32{10,12}{for _,z:=range []int32{10,12}{box(x,0,z,x,6,z,light);put(x,0,z,lamp);put(x,4,z,lamp)}}
   for _,x:=range []int32{8,14}{for _,z:=range []int32{7,14}{box(x,0,z,x,2,z,light)}}
   box(8,3,8,8,3,13,black);box(14,3,8,14,3,13,black);spawn(11,5,13)
  }
 }
}

// modernMonumentChildToBuilding maps the local coordinates consumed by
// emitModernMonumentChild into the MonumentBuilding coordinate frame.
func modernMonumentChildToBuilding(piece *modernOceanMonumentPiece,child modernMonumentChild,x,y,z int32)[3]int32{
	orientation:=piece.orientation
	minX,minY,minZ,maxX,maxZ:=int32(0),int32(0),int32(0),int32(0),int32(0)
	if child.kind==monumentChildWing{
		x0,x1:=int32(1),int32(23);if child.room==1002{x0,x1=34,56}
		a,b:=piece.worldPos(x0,1,1),piece.worldPos(x1,8,21)
		minX,minY,minZ=min(a[0],b[0]),min(a[1],b[1]),min(a[2],b[2]);maxX,maxZ=max(a[0],b[0]),max(a[2],b[2])
	}else if child.kind==monumentChildPenthouse{
		a,b:=piece.worldPos(22,13,22),piece.worldPos(35,17,35)
		minX,minY,minZ=min(a[0],b[0]),min(a[1],b[1]),min(a[2],b[2]);maxX,maxZ=max(a[0],b[0]),max(a[2],b[2])
	}else{
		room:=piece.rooms[child.room]
		width,height,depth:=modernMonumentChildRoomDimensions(child.kind);_ = height
		w,d:=int32(width*8),int32(depth*8)
		if orientation==1||orientation==3{w,d=d,w}
		rx,ry,rz:=int32(room.index%5),int32(room.index/25),int32(room.index/5%5)
		switch orientation{
		case 0:minX,minY,minZ=rx*8,ry*4,-(rz+int32(depth))*8+1
		case 1:minX,minY,minZ=rz*8,ry*4,rx*8
		case 2:minX,minY,minZ=rx*8,ry*4,rz*8
		default:minX,minY,minZ=-(rz+int32(depth))*8+1,ry*4,rx*8
		}
		maxX,maxZ=minX+w-1,minZ+d-1
		offset:=piece.worldPos(9,0,22);minX+=offset[0];maxX+=offset[0];minY+=offset[1];minZ+=offset[2];maxZ+=offset[2]
	}
	var worldX,worldY,worldZ int32
	switch orientation{case 0:worldX,worldZ=minX+x,maxZ-z;case 1:worldX,worldZ=minX+z,minZ+x;case 2:worldX,worldZ=minX+x,minZ+z;default:worldX,worldZ=maxX-z,minZ+x}
	worldY=minY+y
	switch orientation{case 0:return [3]int32{worldX-piece.west,worldY-39,piece.north+57-worldZ};case 1:return [3]int32{worldZ-piece.north,worldY-39,worldX-piece.west};case 2:return [3]int32{worldX-piece.west,worldY-39,worldZ-piece.north};default:return [3]int32{worldZ-piece.north,worldY-39,piece.west+57-worldX}}
}
func modernMonumentChildIntersects(piece *modernOceanMonumentPiece,child modernMonumentChild,clip javastructure.Bounds)bool{
	width,height,depth:=modernMonumentChildRoomDimensions(child.kind)
	widthBlocks,heightBlocks,depthBlocks:=width*8,height*4,depth*8
	switch child.kind{case monumentChildWing:widthBlocks,heightBlocks,depthBlocks=23,8,21;case monumentChildPenthouse:widthBlocks,heightBlocks,depthBlocks=14,5,14}
	minX,minY,minZ:=int32(1<<30),int32(1<<30),int32(1<<30);maxX,maxY,maxZ:=int32(-1<<30),int32(-1<<30),int32(-1<<30)
	for _,x:=range [2]int32{0,int32(widthBlocks-1)}{for _,y:=range [2]int32{0,int32(heightBlocks-1)}{for _,z:=range [2]int32{0,int32(depthBlocks-1)}{
		local:=modernMonumentChildToBuilding(piece,child,x,y,z);pos:=piece.worldPos(local[0],local[1],local[2])
		minX,minY,minZ=min(minX,pos[0]),min(minY,pos[1]),min(minZ,pos[2]);maxX,maxY,maxZ=max(maxX,pos[0]),max(maxY,pos[1]),max(maxZ,pos[2])
	}}}
	return minX<=clip.Max[0]&&maxX>=clip.Min[0]&&minY<=clip.Max[1]&&maxY>=clip.Min[1]&&minZ<=clip.Max[2]&&maxZ>=clip.Min[2]
}
