package overworld

import (
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/world"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("desert_pyramid", buildModernDesertPyramid) }

type modernDesertPyramid struct {
	box javastructure.Box
	dir int
}

func (p modernDesertPyramid) Bounds() javastructure.Box { return p.box }

func (p modernDesertPyramid) pos(x, y, z int32) [3]int32 {
	b := p.box
	switch p.dir {
	case 0: // north
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Max[2] - z}
	case 1: // east
		return [3]int32{b.Min[0] + z, b.Min[1] + y, b.Min[2] + x}
	case 2: // south
		return [3]int32{b.Min[0] + x, b.Min[1] + y, b.Min[2] + z}
	default: // west
		return [3]int32{b.Max[0] - z, b.Min[1] + y, b.Min[2] + x}
	}
}

func (p modernDesertPyramid) facing(local string) string {
	switch p.dir {
	case 0: return map[string]string{"north":"north","east":"east","south":"south","west":"west"}[local]
	case 1: return map[string]string{"north":"east","east":"south","south":"west","west":"north"}[local]
	case 2: return map[string]string{"north":"south","east":"east","south":"north","west":"west"}[local]
	default: return map[string]string{"north":"west","east":"south","south":"east","west":"north"}[local]
	}
}

func (p modernDesertPyramid) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	if env.Random == nil || env.BaseBlock == nil {
		return javastructure.Placement{}, fmt.Errorf("desert pyramid requires placement random and base-block query")
	}
	out := javastructure.Placement{Blocks: make([]javastructure.Block, 0, 12000)}
	overlay := make(map[[3]int32]world.BlockState)
	inside := func(pos [3]int32) bool {
		return pos[0] >= clip.Min[0] && pos[0] <= clip.Max[0] && pos[1] >= clip.Min[1] && pos[1] <= clip.Max[1] && pos[2] >= clip.Min[2] && pos[2] <= clip.Max[2]
	}
	put := func(x, y, z int32, name string, props map[string]any) {
		pos := p.pos(x, y, z)
		if inside(pos) {
			state := world.BlockState{Name: "minecraft:" + name, Properties: props}
			out.Blocks = append(out.Blocks, javastructure.Block{Pos: pos, State: state})
			overlay[pos] = state
		}
	}
	isAir := func(pos [3]int32) (bool, error) {
		if !inside(pos) { return true, nil }
		if state, ok := overlay[pos]; ok {
			return state.Name == "minecraft:air" || state.Name == "minecraft:cave_air" || state.Name == "minecraft:void_air", nil
		}
		block := env.BaseBlock(pos[0], pos[1], pos[2])
		if block == nil { return false, fmt.Errorf("desert pyramid: missing base block at %v", pos) }
		name, _ := block.EncodeBlock()
		return name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air", nil
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, edge, fill string, skipAir bool) error {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					if skipAir {
						air, err := isAir(p.pos(x, y, z)); if err != nil { return err }; if air { continue }
					}
					name := fill
					if x == x0 || x == x1 || y == y0 || y == y1 || z == z0 || z == z1 { name = edge }
					put(x, y, z, name, nil)
				}
			}
		}
		return nil
	}
	place := func(x, y, z int32, name string, props map[string]any) { put(x, y, z, name, props) }
	stair := func(x, y, z int32, facing string) { place(x, y, z, "sandstone_stairs", map[string]any{"facing": p.facing(facing), "half": "bottom", "shape": "straight", "waterlogged": false}) }
	full := func(x0, y0, z0, x1, y1, z1 int32, name string) error { return box(x0, y0, z0, x1, y1, z1, name, name, false) }
	if err := full(0, -4, 0, 20, 0, 20, "sandstone"); err != nil { return out, err }
	for pos := int32(1); pos <= 9; pos++ {
		if err := full(pos, pos, pos, 20-pos, pos, 20-pos, "sandstone"); err != nil { return out, err }
		if err := full(pos+1, pos, pos+1, 19-pos, pos, 19-pos, "air"); err != nil { return out, err }
	}
	for x := int32(0); x < 21; x++ {
		for z := int32(0); z < 21; z++ {
			pos := p.pos(x, -5, z)
			if !inside(pos) { continue }
			for y := pos[1]; y > -64; y-- {
				block := env.BaseBlock(pos[0], y, pos[2])
				if block == nil { return out, fmt.Errorf("desert pyramid: missing base block at %d,%d,%d", pos[0], y, pos[2]) }
				name, _ := block.EncodeBlock()
				if name != "minecraft:air" && name != "minecraft:water" && name != "minecraft:lava" && name != "minecraft:glow_lichen" && name != "minecraft:seagrass" && name != "minecraft:tall_seagrass" { break }
				put(x, y-p.box.Min[1], z, "sandstone", nil)
			}
		}
	}

	// Entry towers, main stairs, and ornamental sandstone face.
	if err := box(0, 0, 0, 4, 9, 4, "sandstone", "air", false); err != nil { return out, err }
	if err := full(1, 10, 1, 3, 10, 3, "sandstone"); err != nil { return out, err }
	for _, v := range []struct{x,y,z int32; f string}{{2,10,0,"north"},{2,10,4,"south"},{0,10,2,"east"},{4,10,2,"west"}} { stair(v.x,v.y,v.z,v.f) }
	if err := box(16, 0, 0, 20, 9, 4, "sandstone", "air", false); err != nil { return out, err }
	if err := full(17, 10, 1, 19, 10, 3, "sandstone"); err != nil { return out, err }
	for _, v := range []struct{x,y,z int32; f string}{{18,10,0,"north"},{18,10,4,"south"},{16,10,2,"east"},{20,10,2,"west"}} { stair(v.x,v.y,v.z,v.f) }
	if err := box(8,0,0,12,4,4,"sandstone","air",false); err != nil { return out, err }
	if err := full(9,1,0,11,3,4,"air"); err != nil { return out, err }
	for _,v:=range [][3]int32{{9,1,1},{9,2,1},{9,3,1},{10,3,1},{11,3,1},{11,2,1},{11,1,1}} { place(v[0],v[1],v[2],"cut_sandstone",nil) }
	if err:=box(4,1,1,8,3,3,"sandstone","air",false);err!=nil{return out,err};if err:=full(4,1,2,8,2,2,"air");err!=nil{return out,err}
	if err:=box(12,1,1,16,3,3,"sandstone","air",false);err!=nil{return out,err};if err:=full(12,1,2,16,2,2,"air");err!=nil{return out,err}
	if err:=full(5,4,5,15,4,15,"sandstone");err!=nil{return out,err};if err:=full(9,4,9,11,4,11,"air");err!=nil{return out,err}
	for _,v:=range [][2]int32{{8,8},{12,8},{8,12},{12,12}} { if err:=full(v[0],1,v[1],v[0],3,v[1],"cut_sandstone");err!=nil{return out,err} }
	if err:=full(1,1,5,4,4,11,"sandstone");err!=nil{return out,err};if err:=full(16,1,5,19,4,11,"sandstone");err!=nil{return out,err}
	if err:=full(6,7,9,6,7,11,"sandstone");err!=nil{return out,err};if err:=full(14,7,9,14,7,11,"sandstone");err!=nil{return out,err}
	if err:=full(5,5,9,5,7,11,"cut_sandstone");err!=nil{return out,err};if err:=full(15,5,9,15,7,11,"cut_sandstone");err!=nil{return out,err}
	for _,v:=range [][3]int32{{5,5,10},{5,6,10},{6,6,10},{15,5,10},{15,6,10},{14,6,10}} { place(v[0],v[1],v[2],"air",nil) }
	if err:=full(2,4,4,2,6,4,"air");err!=nil{return out,err};if err:=full(18,4,4,18,6,4,"air");err!=nil{return out,err}
	for _,v:=range [][3]int32{{2,4,5},{2,3,4},{18,4,5},{18,3,4}} { stair(v[0],v[1],v[2],"north") }
	if err:=full(1,1,3,2,2,3,"sandstone");err!=nil{return out,err};if err:=full(18,1,3,19,2,3,"sandstone");err!=nil{return out,err}
	for _,x:=range []int32{1,19} { place(x,1,2,"sandstone",nil);place(x,2,2,"sandstone_slab",map[string]any{"type":"bottom","waterlogged":false}) }
	stair(2,1,2,"west");stair(18,1,2,"east")
	if err:=full(4,3,5,4,3,17,"sandstone");err!=nil{return out,err};if err:=full(16,3,5,16,3,17,"sandstone");err!=nil{return out,err}
	if err:=full(3,1,5,4,2,16,"air");err!=nil{return out,err};if err:=full(15,1,5,17,2,16,"air");err!=nil{return out,err}
	for z:=int32(5);z<=17;z+=2 { for _,x:=range []int32{4,16} { place(x,1,z,"cut_sandstone",nil);place(x,2,z,"chiseled_sandstone",nil) } }
	for _,v:=range [][3]int32{{10,0,7},{10,0,8},{9,0,9},{11,0,9},{8,0,10},{12,0,10},{7,0,10},{13,0,10},{9,0,11},{11,0,11},{10,0,12},{10,0,13}} { place(v[0],v[1],v[2],"orange_terracotta",nil) };place(10,0,10,"blue_terracotta",nil)
	for _,x:=range []int32{0,20} { for _,y:=range []int32{2,3,4,5,6,7,8} { for _,z:=range []int32{1,2,3} { name:="orange_terracotta";switch y {case 2,3,5,8: if y==8||z!=2{name="cut_sandstone"};case 4,6:if z==2{name="chiseled_sandstone"}};place(x,y,z,name,nil) } } }
	for _,x:=range []int32{2,18} { for _,dx:=range []int32{-1,0,1} { for _,y:=range []int32{2,3,4,5,6,7,8} { name:="orange_terracotta";switch y {case 2,3,5,8:if y==8||dx!=0{name="cut_sandstone"};case 4,6:if dx==0{name="chiseled_sandstone"}};place(x+dx,y,0,name,nil) } } }
	if err:=full(8,4,0,12,6,0,"cut_sandstone");err!=nil{return out,err};place(8,6,0,"air",nil);place(12,6,0,"air",nil);place(9,5,0,"orange_terracotta",nil);place(10,5,0,"chiseled_sandstone",nil);place(11,5,0,"orange_terracotta",nil)

	// Central trap room, four loot chests, and exact cellar walls.
	if err:=full(8,-14,8,12,-11,12,"cut_sandstone");err!=nil{return out,err};if err:=full(8,-10,8,12,-10,12,"chiseled_sandstone");err!=nil{return out,err};if err:=full(8,-9,8,12,-9,12,"cut_sandstone");err!=nil{return out,err};if err:=full(8,-8,8,12,-1,12,"sandstone");err!=nil{return out,err};if err:=full(9,-11,9,11,-1,11,"air");err!=nil{return out,err};place(10,-11,10,"stone_pressure_plate",nil);if err:=box(9,-13,9,11,-13,11,"tnt","air",false);err!=nil{return out,err}
	for _,v:=range [][3]int32{{8,-11,10},{8,-10,10},{12,-11,10},{12,-10,10},{10,-11,8},{10,-10,8},{10,-11,12},{10,-10,12}} { place(v[0],v[1],v[2],"air",nil) }
	for _,v:=range []struct{x,z int32;face string}{{10,8,"north"},{12,10,"east"},{10,12,"south"},{8,10,"west"}} { pos:=p.pos(v.x,-11,v.z);if !inside(pos){continue};name:="";if state,ok:=overlay[pos];ok{name=state.Name}else{block:=env.BaseBlock(pos[0],pos[1],pos[2]);if block==nil{return out,fmt.Errorf("desert pyramid: missing chest position block at %v",pos)};name,_=block.EncodeBlock()};if name=="minecraft:chest"{continue};out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:"minecraft:chest",Properties:map[string]any{"facing":p.facing(v.face),"type":"single","waterlogged":false}},NBT:map[string]any{"id":"minecraft:chest","LootTable":"minecraft:chests/desert_pyramid","LootTableSeed":blockPosLong(pos)}})}
	centerX, centerY, centerZ := int32(16), int32(-4), int32(13)
	variant := env.Random.NextBoolean()
	stair(13, -1, 17, "west")
	stair(14, -2, 17, "west")
	stair(15, -3, 17, "west")
	for _, v := range []struct{x,y,z int32; name string}{
		{12,0,17,"sand"},{13,0,17,"sand"},{14,0,17,"sand"},{15,0,17,"sand"},{16,0,17,"sand"},
		{14,-1,17,"sand"},{15,-1,17,"sand"},{16,-1,17,"sand"},{15,-2,17,"sand"},{16,-2,17,"sand"},{16,-3,17,"sand"},
	} { name:=v.name; if v.x==15&&v.y==-1&&v.z==17&&!variant{name="sandstone"};if v.x==16&&v.y==-1&&v.z==17&&variant{name="sandstone"};if v.x==16&&v.y==-2&&v.z==17{name="sandstone"};place(v.x,v.y,v.z,name,nil) }
	// Cellar walls use generateBox(..., skipAir=true), unlike the solid
	// above-ground boxes: preserve air from the already-built pyramid interior.
	for _, y := range []int32{-3,-2} {
		material := "cut_sandstone";if y==-2{material="chiseled_sandstone"}
		if err:=box(13,y,10,13,y,15,material,material,true);err!=nil{return out,err}
		if err:=box(19,y,10,19,y,15,material,material,true);err!=nil{return out,err}
		if err:=box(13,y,10,19,y,11,material,material,true);err!=nil{return out,err}
		if err:=box(13,y,16,19,y,16,material,material,true);err!=nil{return out,err}
	}
	for _, bounds := range [][6]int32{{13,-1,10,13,-1,15},{19,-1,10,19,-1,15},{13,-1,10,19,-1,11},{13,-1,16,19,-1,16}} { if err:=box(bounds[0],bounds[1],bounds[2],bounds[3],bounds[4],bounds[5],"cut_sandstone","cut_sandstone",true);err!=nil{return out,err} }
	candidates:=make([][3]int32,0,83)
	for y:=centerY+1;y<=centerY+3;y++ { for x:=centerX-2;x<=centerX+2;x++ { for z:=centerZ-2;z<=centerZ+2;z++ { candidates=append(candidates,p.pos(x,y,z)) } } }
	for _,v:=range [][3]int32{{19,-3,13},{19,-2,13},{13,-3,13},{13,-2,13},{16,-3,16},{16,-2,16},{16,-3,10},{16,-2,10}} { candidates=append(candidates,p.pos(v[0],v[1],v[2])) }
	for _,v:=range []struct{x,y,z int32;n string}{
		{16,-4,13,"blue_terracotta"},{17,-4,12,"orange_terracotta"},{17,-4,14,"orange_terracotta"},{15,-4,12,"orange_terracotta"},{15,-4,14,"orange_terracotta"},{18,-4,13,"orange_terracotta"},{14,-4,13,"orange_terracotta"},{16,-4,15,"orange_terracotta"},{16,-4,11,"orange_terracotta"},
		{19,-3,13,"cut_sandstone"},{19,-2,13,"chiseled_sandstone"},{13,-3,13,"cut_sandstone"},{13,-2,13,"chiseled_sandstone"},{16,-3,16,"cut_sandstone"},{16,-2,16,"chiseled_sandstone"},{16,-3,10,"cut_sandstone"},{16,-2,10,"chiseled_sandstone"},
	} { place(v.x,v.y,v.z,v.n,nil) }
	for x:=int32(14);x<=18;x++ { for z:=int32(11);z<=15;z++ { if env.Random.NextFloat()<0.33 { put(x,0,z,"sandstone",nil) } else { put(x,0,z,"sand",nil) } } }
	roofSeed:=java26.NewXoroshiroRandom(env.WorldSeed)
	roofRandom:=roofSeed.ForkPositional().At(p.pos(14,0,11)[0],p.pos(14,0,11)[1],p.pos(14,0,11)[2])
	roofX,roofZ:=int32(14)+roofRandom.NextInt(5),int32(11)+roofRandom.NextInt(5)
	roofPos:=p.pos(roofX,0,roofZ)
	if inside(roofPos) { out.Blocks=append(out.Blocks,javastructure.Block{Pos:roofPos,State:world.BlockState{Name:"minecraft:suspicious_sand"},NBT:map[string]any{"id":"minecraft:brushable_block","LootTable":"minecraft:archaeology/desert_pyramid","LootTableSeed":blockPosLong(roofPos)}}) }
	sort.Slice(candidates,func(i,j int)bool{a,b:=candidates[i],candidates[j];if a[1]!=b[1]{return a[1]<b[1]};if a[2]!=b[2]{return a[2]<b[2]};return a[0]<b[0]})
	placementSeed:=java26.NewXoroshiroRandom(env.WorldSeed)
	r:=placementSeed.ForkPositional().At((p.box.Min[0]+p.box.Max[0])/2,(p.box.Min[1]+p.box.Max[1])/2,(p.box.Min[2]+p.box.Max[2])/2)
	for i:=len(candidates)-1;i>0;i--{j:=int(r.NextInt(int32(i+1)));candidates[i],candidates[j]=candidates[j],candidates[i]}
	suspiciousCount:=int(r.NextInt(3))+5
	for i,pos:=range candidates { if !inside(pos){continue};state:=world.BlockState{Name:"minecraft:sand"};var nbt map[string]any;if i<suspiciousCount{state.Name="minecraft:suspicious_sand";nbt=map[string]any{"id":"minecraft:brushable_block","LootTable":"minecraft:archaeology/desert_pyramid","LootTableSeed":blockPosLong(pos)}};out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:state,NBT:nbt}) }
	return out,nil
}
func blockPosLong(pos [3]int32) int64 {
	return (int64(pos[0])&0x3ffffff)<<38 | (int64(pos[2])&0x3ffffff)<<12 | int64(pos[1])&0xfff
}

func buildModernDesertPyramid(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random == nil || in.source == nil { return modernStructureStart{}, false, fmt.Errorf("desert pyramid requires structure random and world source") }
	x,z := in.chunkX*16,in.chunkZ*16
	lowest := int32(1<<30)
	for xx:=x;xx<=x+20;xx++ { for zz:=z;zz<=z+20;zz++ { if y:=in.source.BaseHeight(xx,zz,false); y<lowest { lowest=y } } }
	if lowest == int32(1<<30) || lowest < modernSeaLevel { return modernStructureStart{},false,nil }
	direction := int(in.random.NextInt(4))
	baseY:=lowest-in.random.NextInt(3)
	box:=javastructure.Box{Min:[3]int32{x,baseY,z},Max:[3]int32{x+20,baseY+14,z+20}}
	piece:=modernDesertPyramid{box:box,dir:direction}
	return modernStructureStart{name:in.name,pieces:[]modernStructurePiece{piece},box:box,terrain:terrainAdjustment(stringValue(in.config["terrain_adaptation"]))},true,nil
}
