package overworld

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init(){registerModernStructureBuilder("ocean_ruin",buildModernOceanRuin)}

type modernOceanRuinPiece struct{ template *javastructure.Template; origin [3]int32; rotation javastructure.Rotation; integrity float32; warm,large bool; seed int64 }
func(p modernOceanRuinPiece)Bounds()javastructure.Box{return modernTemplateBounds(p.template,p.origin,p.rotation)}
func(p modernOceanRuinPiece)Place(clip javastructure.Bounds,env javastructure.ProcessorEnvironment)(javastructure.Placement,error){
	if env.Random==nil{return javastructure.Placement{},fmt.Errorf("ocean ruin requires placement random")}
	var capSelected map[int]struct{}
	processors:=[]javastructure.Processor{
		{Process: func(ctx javastructure.ProcessContext,b javastructure.Block)(javastructure.Block,bool,error){r:=java26.NewLegacyRandom(modernJavaPositionSeed(ctx.Position));return b,r.NextFloat()<=p.integrity,nil}},
		{Process: func(_ javastructure.ProcessContext,b javastructure.Block)(javastructure.Block,bool,error){return b,b.State.Name!="minecraft:air"&&b.State.Name!="minecraft:structure_block",nil}},
		{Process: func(ctx javastructure.ProcessContext,b javastructure.Block)(javastructure.Block,bool,error){if _,ok:=capSelected[ctx.BlockIndex];!ok{return b,true,nil};candidate:="minecraft:gravel";replacement,loot:="minecraft:suspicious_gravel","minecraft:archaeology/ocean_ruin_cold";if p.warm{candidate,replacement,loot="minecraft:sand","minecraft:suspicious_sand","minecraft:archaeology/ocean_ruin_warm"};if b.State.Name!=candidate{return b,true,nil};b.State=world.BlockState{Name:replacement};b.NBT=map[string]any{"id":"minecraft:brushable_block","LootTable":loot};return b,true,nil}},
	}
	origin:=p.origin
	if env.BaseHeight==nil||env.BaseBlock==nil{return javastructure.Placement{},fmt.Errorf("ocean ruin requires terrain queries")}
	height:=env.BaseHeight(origin[0],origin[2],true)
	corner:=javastructure.TransformPosition([3]int32{p.template.Size[0]-1,0,p.template.Size[2]-1},javastructure.NoMirror,p.rotation);corner[0]+=origin[0];corner[2]+=origin[2]
	minX,maxX:=min(origin[0],corner[0]),max(origin[0],corner[0]);minZ,maxZ:=min(origin[2],corner[2]),max(origin[2],corner[2])
	minY,topY,area:=int32(512),height-1,0
	for dz:=range maxZ-minZ+1{for dx:=range maxX-minX+1{x,z:=minX+dx,minZ+dz;floorY:=height-1;for floorY>clip.Min[1]+1{blockName,_:=env.BaseBlock(x,floorY,z).EncodeBlock();if blockName!="minecraft:air"&&blockName!="minecraft:water"&&blockName!="minecraft:bubble_column"&&blockName!="minecraft:ice"&&blockName!="minecraft:packed_ice"&&blockName!="minecraft:blue_ice"{break};floorY--};minY=min(minY,floorY);if floorY<topY-2{area++}}}
	width:=int32(math.Abs(float64(origin[0]-corner[0])))
	if topY-minY>2&&area>int(width-2){origin[1]=minY+1}else{origin[1]=height}
	capSelected=modernOceanRuinCappedBlocks(p.template,origin,p.rotation,p.integrity,env.WorldSeed,p.warm)
	placed,err:=p.template.Place(0,origin,p.rotation,javastructure.NoMirror,clip,p.seed,processors...)
	if err!=nil{return javastructure.Placement{},err}
	for i:=range placed.Blocks{javastructure.SeedRandomizableContainerLoot(&placed.Blocks[i],env.Random)}
	inside:=func(pos [3]int32)bool{return pos[0]>=clip.Min[0]&&pos[0]<=clip.Max[0]&&pos[1]>=clip.Min[1]&&pos[1]<=clip.Max[1]&&pos[2]>=clip.Min[2]&&pos[2]<=clip.Max[2]}
	for _,source:=range p.template.Palettes[0]{
		if source.State.Name!="minecraft:structure_block"||source.NBT==nil||source.NBT["mode"]!="DATA"{continue}
		marker,_:=source.NBT["metadata"].(string);pos:=javastructure.TransformPosition(source.Pos,javastructure.NoMirror,p.rotation);for axis:=range 3{pos[axis]+=origin[axis]};if !inside(pos){continue}
		switch marker{
		case "chest":
			if env.Random==nil{return javastructure.Placement{},fmt.Errorf("ocean ruin chest marker requires placement random")}
			blockName,properties:=env.BaseBlock(pos[0],pos[1],pos[2]).EncodeBlock();waterlogged:=blockName=="minecraft:water"||blockName=="minecraft:bubble_column";if property,ok:=properties["waterlogged"].(bool);ok&&property{waterlogged=true}
			table:="minecraft:chests/underwater_ruin_small";if p.large{table="minecraft:chests/underwater_ruin_big"}
			block := javastructure.Block{Pos: pos, State: world.BlockState{Name: "minecraft:chest", Properties: map[string]any{"facing": "north", "type": "single", "waterlogged": waterlogged}}, NBT: map[string]any{"id": "minecraft:chest", "LootTable": table}}
			javastructure.SeedRandomizableContainerLoot(&block, env.Random)
			placed.PostShapeBlocks = append(placed.PostShapeBlocks, block)
		case "drowned":
			placed.PostShapeEntities = append(placed.PostShapeEntities, javastructure.Entity{Pos: [3]float64{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}, BlockPos: pos, NBT: map[string]any{"id": "minecraft:drowned", "PersistenceRequired": true}, Record: int32(len(placed.Entities)), Seed: p.seed, SpawnReason: "structure"})
			state := "minecraft:water"
			if pos[1] > 63 {
				state = "minecraft:air"
			}
			placed.PostShapeBlocks = append(placed.PostShapeBlocks, javastructure.Block{Pos: pos, State: world.BlockState{Name: state}})
		}
	}
	return placed,nil
}
func buildModernOceanRuin(input modernStructureBuildInput)(modernStructureStart,bool,error){
	if input.source==nil||input.random==nil{return modernStructureStart{},false,fmt.Errorf("ocean ruin requires terrain source and generation random")}
	allowed,err:=modernStructureBiomeAllowed(input,stringValue(input.config["biomes"]),true);if err!=nil{return modernStructureStart{},false,err};if !allowed{return modernStructureStart{},false,nil}
	warm:=normalizeJavaResource(stringValue(input.config["biome_temp"]))!="cold"
	rotation:=javastructure.Rotation(input.random.NextInt(4))
	large:=input.random.NextFloat()<=float32(number(input.config["large_probability"]))
	integrity:=float32(0.8);if large{integrity=0.9}
	pieces:=make([]modernStructurePiece,0,9)
	parent:=[3]int32{input.chunkX*16,90,input.chunkZ*16}
	appendPiece:=func(origin [3]int32,r javastructure.Rotation,large bool,base float32)error{
		var names []string
		if warm { if large { names=[]string{"big_warm_4","big_warm_5","big_warm_6","big_warm_7"} } else { names=[]string{"warm_1","warm_2","warm_3","warm_4","warm_5","warm_6","warm_7","warm_8"} }; name:=names[input.random.NextInt(int32(len(names)))];t,err:=javastructure.Load("underwater_ruin/"+name);if err!=nil{return err};pieces=append(pieces,modernOceanRuinPiece{template:t,origin:origin,rotation:r,integrity:base,warm:true,large:large,seed:input.seed});return nil }
		if large { idx:=int(input.random.NextInt(4));for _,v:=range []struct{prefix string;integrity float32}{{"big_brick_",base},{"big_cracked_",.7},{"big_mossy_",.5}} {suffix:=[]int{1,2,3,8}[idx];t,err:=javastructure.Load(fmt.Sprintf("underwater_ruin/%s%d",v.prefix,suffix));if err!=nil{return err};pieces=append(pieces,modernOceanRuinPiece{template:t,origin:origin,rotation:r,integrity:v.integrity,warm:false,large:true,seed:input.seed})};return nil}
		idx:=int(input.random.NextInt(8));for _,v:=range []struct{prefix string;integrity float32}{{"brick_",base},{"cracked_",.7},{"mossy_",.5}} {t,err:=javastructure.Load(fmt.Sprintf("underwater_ruin/%s%d",v.prefix,idx+1));if err!=nil{return err};pieces=append(pieces,modernOceanRuinPiece{template:t,origin:origin,rotation:r,integrity:v.integrity,warm:false,large:false,seed:input.seed})};return nil
	}
	if warm{if err:=appendPiece(parent,rotation,large,integrity);err!=nil{return modernStructureStart{},false,err}}else{if err:=appendPiece(parent,rotation,large,integrity);err!=nil{return modernStructureStart{},false,err}}
	if large&&input.random.NextFloat()<=float32(number(input.config["cluster_probability"])){
		parentCorner:=javastructure.TransformPosition([3]int32{15,0,15},javastructure.NoMirror,rotation); parentCorner[0]+=parent[0];parentCorner[2]+=parent[2]
		origin:=[3]int32{min(parent[0],parentCorner[0]),90,min(parent[2],parentCorner[2])}
		positions:=[][3]int32{{origin[0]-16+1+input.random.NextInt(8),90,origin[2]+16+1+input.random.NextInt(7)},{origin[0]-16+1+input.random.NextInt(8),90,origin[2]+input.random.NextInt(7)},{origin[0]-16+1+input.random.NextInt(8),90,origin[2]-16+4+input.random.NextInt(5)},{origin[0]+input.random.NextInt(7),90,origin[2]+16+1+input.random.NextInt(7)},{origin[0]+input.random.NextInt(7),90,origin[2]-16+4+input.random.NextInt(3)},{origin[0]+16+1+input.random.NextInt(7),90,origin[2]+16+3+input.random.NextInt(6)},{origin[0]+16+1+input.random.NextInt(7),90,origin[2]+input.random.NextInt(7)},{origin[0]+16+1+input.random.NextInt(7),90,origin[2]-16+4+input.random.NextInt(5)}}
		parentBox:=modernTemplateBounds(&javastructure.Template{Size:[3]int32{16,1,16}},origin,rotation)
		for range 4+input.random.NextInt(5){if len(positions)==0{break};idx:=input.random.NextInt(int32(len(positions)));pos:=positions[idx];positions=append(positions[:idx],positions[idx+1:]...);r:=javastructure.Rotation(input.random.NextInt(4));corner:=javastructure.TransformPosition([3]int32{5,0,6},javastructure.NoMirror,r);candidate:=javastructure.Box{Min:[3]int32{min(pos[0],pos[0]+corner[0]),90,min(pos[2],pos[2]+corner[2])},Max:[3]int32{max(pos[0],pos[0]+corner[0]),90,max(pos[2],pos[2]+corner[2])}};if modernBoxesIntersectXZ(parentBox,candidate){continue};if err:=appendPiece(pos,r,false,.8);err!=nil{return modernStructureStart{},false,err}}
	}
	start:=modernStructureStart{name:input.name,pieces:pieces,box:emptyStructureBox()};for _,piece:=range pieces{start.box=unionStructureBox(start.box,piece.Bounds())};return start,true,nil
}
func modernJavaPositionSeed(pos [3]int32)int64{seed:=int64(pos[0]*3129871)^int64(pos[2])*116129781^int64(pos[1]);seed=seed*seed*42317861+seed*11;return seed>>16}
func modernTemplateBounds(t *javastructure.Template,origin [3]int32,r javastructure.Rotation)javastructure.Box{b:=javastructure.Box{Min:[3]int32{math.MaxInt32,math.MaxInt32,math.MaxInt32},Max:[3]int32{math.MinInt32,math.MinInt32,math.MinInt32}};for _,x:=range [2]int32{0,t.Size[0]-1}{for _,y:=range [2]int32{0,t.Size[1]-1}{for _,z:=range [2]int32{0,t.Size[2]-1}{p:=javastructure.TransformPosition([3]int32{x,y,z},javastructure.NoMirror,r);for a:=range 3{p[a]+=origin[a];b.Min[a]=min(b.Min[a],p[a]);b.Max[a]=max(b.Max[a],p[a])}}}};return b}
func modernBoxesIntersectXZ(a,b javastructure.Box)bool{return a.Min[0]<=b.Max[0]&&a.Max[0]>=b.Min[0]&&a.Min[2]<=b.Max[2]&&a.Max[2]>=b.Min[2]}
func modernOceanRuinCappedBlocks(t *javastructure.Template,origin [3]int32,rotation javastructure.Rotation,integrity float32,worldSeed int64,warm bool)map[int]struct{}{
	palette:=t.Palettes[0];indices:=make([]int,0,len(palette))
	for index,block:=range palette{
		pos:=javastructure.TransformPosition(block.Pos,javastructure.NoMirror,rotation);for axis:=range 3{pos[axis]+=origin[axis]}
		r:=java26.NewLegacyRandom(modernJavaPositionSeed(pos));if r.NextFloat()>integrity{continue}
		if block.State.Name=="minecraft:air"||block.State.Name=="minecraft:structure_block"{continue}
		indices=append(indices,index)
	}
	xoroshiro:=java26.NewXoroshiroRandom(worldSeed)
	r:=xoroshiro.ForkPositional().At(origin[0],origin[1],origin[2])
	for n:=range len(indices)-1{i:=len(indices)-1-n;j:=int(r.NextInt(int32(i+1)));indices[i],indices[j]=indices[j],indices[i]}
	selected:=make(map[int]struct{},5);replaced:=0
	for _,index:=range indices{state:=palette[index].State.Name;if warm&&state!="minecraft:sand"||!warm&&state!="minecraft:gravel"{continue};selected[index]=struct{}{};replaced++;if replaced==5{break}}
	return selected
}
