package javastructure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
)

// Projection is the Java StructureTemplatePool projection mode.
type Projection uint8

const (
	Rigid Projection = iota
	TerrainMatching
)

// JigsawOptions describes one Java JigsawStructure start.
type JigsawOptions struct {
	Pool string
	// PoolAliases contains resolved, one-hop pool replacements for this start.
	PoolAliases map[string]string
	StartJigsaw string
	Origin [3]int32
	MaxDepth int
	MaxDistance int32
	MaxVerticalDistance int32
	// HeightRange is the Java level-height accessor, not the native write clip.
	HeightRange cube.Range
	PaddingBottom, PaddingTop int32
	// CheckBounds distinguishes an explicit zero padding from Java's default ZERO.
	CheckBounds bool
	ExpansionHack bool
	Seed int64
	Terrain TerrainAdjustment
	HeightAt func(x,z int32) int32
	ProjectStartToHeightmap bool
	LiquidSettings LiquidSettings
}

// JigsawPiece is one selected pool element with its Java placement parameters.
type JigsawPiece struct {
	Template *Template
	parts []jigsawPart
	Origin [3]int32
	Rotation Rotation
	Mirror Mirror
	Projection Projection
	GroundLevelDelta int32
	Processors []string
	ProcessorSpecs []ProcessorSpec
	Bounds Box
	Junctions []Junction
	LiquidSettings LiquidSettings
	legacySingle bool
}
type jigsawPart struct {
	template *Template
	feature any
	processors []string
	processorSpecs []ProcessorSpec
	liquidSettings LiquidSettings
	overrideLiquidSettings bool
	legacySingle bool
}
// Place clips this piece to one world-space box and applies caller-supplied processors in order.
func (p JigsawPiece) Place(palette int, clip Bounds, seed int64, processors ...Processor) (Placement,error) {
	if len(p.parts)>0 {
		out:=Placement{}
		for _,part:=range p.parts {
			if part.feature!=nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element requires a placed-feature executor")}
			settings := p.LiquidSettings
			if part.overrideLiquidSettings { settings = part.liquidSettings }
			placed,err:=part.place(palette,p.Origin,p.Rotation,p.Mirror,clip,seed,processors,settings)
			if err!=nil{return Placement{},err}
			out.Blocks=append(out.Blocks,placed.Blocks...)
			out.Entities=append(out.Entities,placed.Entities...)
		}
		return out,nil
	}
	placed, err := p.Template.Place(palette,p.Origin,p.Rotation,p.Mirror,clip,seed,processors...)
	markTemplateLiquidPass(&placed, p.LiquidSettings)
	return placed, err
}
func (part jigsawPart) place(palette int,origin [3]int32,rotation Rotation,mirror Mirror,clip Bounds,seed int64,processors []Processor,settings LiquidSettings)(Placement,error){
	if part.template==nil{return Placement{},nil}
	placed, err := part.template.Place(palette,origin,rotation,mirror,clip,seed,processors...)
	markTemplateLiquidPass(&placed, settings)
	return placed, err
}

func markTemplateLiquidPass(placement *Placement, settings LiquidSettings) {
	if len(placement.Blocks) != 0 {
		placement.Blocks[0].TemplateStart = true
		placement.Blocks[0].LiquidSettings = settings
	}
}
// PlaceWithEnvironment runs the pool element's processors and Java terrain-matching gravity projection.
func (p JigsawPiece) PlaceWithEnvironment(palette int, clip Bounds, seed int64, env ProcessorEnvironment) (Placement, error) {
	if len(p.parts)>0 {
		out:=Placement{}
		for _,part:=range p.parts {
			if part.feature!=nil {
				if p.Origin[0]>=clip.Min[0]&&p.Origin[0]<=clip.Max[0]&&p.Origin[1]>=clip.Min[1]&&p.Origin[1]<=clip.Max[1]&&p.Origin[2]>=clip.Min[2]&&p.Origin[2]<=clip.Max[2] {
					if env.PlaceFeature==nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element has no placed-feature executor")}
					if env.Random==nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element has no placement random source")}
					env.PlaceFeature(part.feature,p.Origin,env.Random)
				}
				continue
			}
			if part.template==nil{continue}
			partPiece:=p
			partPiece.Template=part.template
			partPiece.parts=nil
			partPiece.Processors=part.processors
			partPiece.ProcessorSpecs=part.processorSpecs
			partPiece.legacySingle=part.legacySingle
			if part.overrideLiquidSettings { partPiece.LiquidSettings = part.liquidSettings }
			placed,err:=partPiece.placeWithEnvironment(palette,clip,seed,env)
			if err!=nil{return Placement{},err}
			out.Blocks=append(out.Blocks,placed.Blocks...)
			out.Entities=append(out.Entities,placed.Entities...)
		}
		return out,nil
	}
	return p.placeWithEnvironment(palette,clip,seed,env)
}
func (p JigsawPiece) placeWithEnvironment(palette int, clip Bounds, seed int64, env ProcessorEnvironment) (Placement,error) {
	env.WorldSeed = seed
	env.StartPos = p.Origin
	if p.Template != nil && palette >= 0 && palette < len(p.Template.Palettes) { env.BlockCount = len(p.Template.Palettes[palette]) }
	processors, err := LoadProcessors(p.Processors, p.ProcessorSpecs, env)
	if err != nil { return Placement{}, err }
	replacement, err := compileJigsawReplace()
	if err != nil { return Placement{}, err }
	defaults := []Processor{replacement}
	if !p.legacySingle {
		defaults = append([]Processor{processor(func(_ ProcessContext, block Block) (Block, bool, error) {
			return block, block.State.Name != "minecraft:structure_block", nil
		})}, defaults...)
	}
	processors = append(defaults, processors...)
	if p.Projection == TerrainMatching {
		gravity, err := compileGravity(map[string]any{"offset": int32(-1), "heightmap": "WORLD_SURFACE_WG"}, env)
		if err != nil { return Placement{}, err }
		processors = append(processors, gravity)
	}
	if p.legacySingle {
		processors = append(processors, processor(func(_ ProcessContext, block Block) (Block, bool, error) {
			return block, block.State.Name != "minecraft:air" && block.State.Name != "minecraft:structure_block", nil
		}))
	}
	if env.Random != nil {
		processors = append(processors, processor(func(_ ProcessContext, block Block) (Block, bool, error) {
			SeedRandomizableContainerLoot(&block, env.Random)
			return block, true, nil
		}))
	}
	if env.SkipEntities {
		placed, err := p.Template.PlaceBlocks(palette, p.Origin, p.Rotation, p.Mirror, clip, seed, processors...)
		markTemplateLiquidPass(&placed, p.LiquidSettings)
		return placed, err
	}
	return p.Place(palette, clip, seed, processors...)
}

func randomizableContainer(name string) bool {
	switch name {
	case "minecraft:chest", "minecraft:trapped_chest", "minecraft:barrel", "minecraft:dispenser", "minecraft:dropper":
		return true
	default:
		return strings.HasSuffix(name, "_shulker_box")
	}
}

// SeedRandomizableContainerLoot assigns Java's structure-placement loot seed to a block entity.
func SeedRandomizableContainerLoot(block *Block, random *java26.Random) {
	if block == nil || random == nil || len(block.NBT) == 0 || !randomizableContainer(block.State.Name) { return }
	block.NBT = mergeNBT(block.NBT, map[string]any{"LootTableSeed": random.NextLong()})
}

// JigsawResult contains the selected piece graph, rigid terrain contributions, and junctions.
type JigsawResult struct {
	Pieces []JigsawPiece
	RigidPieces []RigidPiece
	Junctions []Junction
	// StartPos is the source GenerationStub position, including a named anchor's Y.
	StartPos [3]int32
	HasStart bool
}

type poolElement struct {
	template string
	feature any
	processors []string
	processorSpecs []ProcessorSpec
	projection Projection
	weight int32
	empty bool
	children []poolElement
	liquidSettings LiquidSettings
	overrideLiquidSettings bool
	legacySingle bool
}

func decodePoolElements(raw any)([]poolElement,error){
	values,ok:=raw.([]any);if !ok{return nil,fmt.Errorf("elements has type %T",raw)}
	out:=make([]poolElement,0,len(values))
	for _,item:=range values{
		entry,ok:=item.(map[string]any);if !ok{return nil,fmt.Errorf("entry has type %T",item)}
		element,ok:=entry["element"].(map[string]any)
		if !ok{return nil,fmt.Errorf("element has type %T",entry["element"])}
		weight, valid := int32Value(entry["weight"])
		if !valid || weight < 1 || weight > 150 { return nil, fmt.Errorf("Java pool weight must be in [1,150]: %v", entry["weight"]) }
		decoded,err:=decodePoolElement(element,weight)
		if err!=nil{return nil,err}
		out=append(out,decoded)
	}
	return out,nil
}
func decodePoolElement(element map[string]any,weight int32)(poolElement,error){
	typeName,_:=element["element_type"].(string)
	projection:=TerrainMatching
	if element["projection"]=="rigid"{projection=Rigid}
	switch strings.TrimPrefix(typeName,"minecraft:"){
	case "empty_pool_element","empty":
		return poolElement{empty:true,weight:weight,projection:projection},nil
	case "single_pool_element","legacy_single_pool_element":
		location,_:=element["location"].(string)
		processors,specs,err:=decodeElementProcessors(element["processors"])
		if err!=nil{return poolElement{},err}
		if location==""{return poolElement{},fmt.Errorf("single pool element lacks location")}
		rawSettings, override := element["override_liquid_settings"]
		settings, err := ParseLiquidSettings(rawSettings)
		if err != nil { return poolElement{}, err }
		return poolElement{template:strings.TrimPrefix(location,"minecraft:"),processors:processors,processorSpecs:specs,projection:projection,weight:weight,liquidSettings:settings,overrideLiquidSettings:override,legacySingle:strings.TrimPrefix(typeName,"minecraft:")=="legacy_single_pool_element"},nil
	case "feature_pool_element":
		feature,ok:=element["feature"]
		if !ok{return poolElement{},fmt.Errorf("feature pool element lacks feature")}
		switch feature.(type) {
		case string,map[string]any:
		default:return poolElement{},fmt.Errorf("feature pool element has invalid feature holder %T",feature)
		}
		return poolElement{feature:feature,projection:projection,weight:weight},nil
	case "list_pool_element":
		raw,ok:=element["elements"].([]any)
		if !ok{return poolElement{},fmt.Errorf("list pool element elements has type %T",element["elements"])}
		if len(raw)==0{return poolElement{},fmt.Errorf("list pool element has no elements")}
		children:=make([]poolElement,0,len(raw))
		for _,child:=range raw{
			definition,ok:=child.(map[string]any)
			if !ok{return poolElement{},fmt.Errorf("list pool element child has type %T",child)}
			decoded,err:=decodePoolElement(definition,1)
			if err!=nil{return poolElement{},err}
			setPoolElementProjection(&decoded,projection)
			children=append(children,decoded)
		}
		return poolElement{projection:projection,weight:weight,children:children},nil
	default:
		return poolElement{},fmt.Errorf("unsupported pool element type %q",typeName)
	}
}
func setPoolElementProjection(element *poolElement,projection Projection){
	element.projection=projection
	for i:=range element.children{setPoolElementProjection(&element.children[i],projection)}
}
func decodeElementProcessors(raw any) ([]string, []ProcessorSpec, error) {
	if raw == nil { return nil,nil,nil }
	if name,ok:=raw.(string);ok { return []string{strings.TrimPrefix(name,"minecraft:")},nil,nil }
	container,ok:=raw.(map[string]any);if !ok{return nil,nil,fmt.Errorf("invalid processor list %T",raw)}
	values,ok:=container["processors"].([]any);if !ok{return nil,nil,fmt.Errorf("inline processors has type %T",container["processors"])}
	specs:=make([]ProcessorSpec,0,len(values))
	for _,value:=range values{
		config,ok:=value.(map[string]any);if !ok{return nil,nil,fmt.Errorf("inline processor has type %T",value)}
		name,ok:=config["processor_type"].(string);if !ok{return nil,nil,fmt.Errorf("inline processor has no processor_type")}
		specs=append(specs,ProcessorSpec{Type:strings.TrimPrefix(name,"minecraft:"),Config:config})
	}
	return nil,specs,nil
}

func choosePoolIndex(pool []poolElement, r *java26.Random) (int,error) {
	if len(pool)==0 { return 0,fmt.Errorf("Java jigsaw: empty pool") }
	total:=int32(0);for i:=range pool{total+=pool[i].weight}
	choice:=r.NextInt(total)
	for i:=range pool{choice-=pool[i].weight;if choice<0{return i,nil}}
	return len(pool)-1,nil
}
func appendShuffledPoolElements(out []*poolElement, pool []poolElement, random *java26.Random) []*poolElement {
	start := len(out)
	for i := range pool { for range pool[i].weight { out = append(out, &pool[i]) } }
	for i := len(out)-1; i > start; i-- {
		j := start + int(random.NextInt(int32(i-start+1)))
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func randomRotations(r *java26.Random)[4]Rotation{
	out:=[4]Rotation{None,Clockwise90,Clockwise180,CounterClockwise90}
	for i:=len(out)-1;i>0;i--{j:=int(r.NextInt(int32(i+1)));out[i],out[j]=out[j],out[i]}
	return out
}
func canAttach(source,target *connector)bool{
	if source.front==""||target.front==""||directionOpposite(source.front)!=target.front{return false}
	if source.joint!="rollable"&&source.top!=target.top{return false}
	return target.wildcard||strings.TrimPrefix(source.target,"minecraft:")==strings.TrimPrefix(target.name,"minecraft:")
}
func hasPoolPlacement(parts []jigsawPart)bool{
	for _,part:=range parts{if part.template!=nil||part.feature!=nil{return true}}
	return false
}
func makeJigsawPoolPiece(parts []jigsawPart,origin [3]int32,rotation Rotation,e *poolElement,settings LiquidSettings)JigsawPiece{
	if e.overrideLiquidSettings { settings = e.liquidSettings }
	piece:=JigsawPiece{Origin:origin,Rotation:rotation,Projection:e.projection,GroundLevelDelta:1,Bounds:Box{Min:origin,Max:origin},LiquidSettings:settings,legacySingle:e.legacySingle}
	hasFeature:=false
	for _,part:=range parts { if part.feature!=nil {hasFeature=true};if piece.Template==nil&&part.template!=nil{piece.Template=part.template} }
	if len(e.children)>0||hasFeature{piece.parts=parts}
	if piece.Template!=nil{
		piece.Processors=e.processors
		piece.ProcessorSpecs=e.processorSpecs
		piece.Bounds=templateBounds(piece.Template,origin,rotation)
	}
	if hasFeature&&piece.Template!=nil{
		for axis:=range 3{piece.Bounds.Min[axis]=min(piece.Bounds.Min[axis],origin[axis]);piece.Bounds.Max[axis]=max(piece.Bounds.Max[axis],origin[axis])}
	}
	for _,part:=range parts{
		if part.template==nil||part.template==piece.Template{continue}
		bounds:=templateBounds(part.template,origin,rotation)
		for axis:=range 3{piece.Bounds.Min[axis]=min(piece.Bounds.Min[axis],bounds.Min[axis]);piece.Bounds.Max[axis]=max(piece.Bounds.Max[axis],bounds.Max[axis])}
	}
	return piece
}
func templateBounds(t *Template,origin [3]int32,r Rotation)Box{
	first:=[3]int32{};last:=[3]int32{t.Size[0]-1,t.Size[1]-1,t.Size[2]-1}
	corners:=[2][3]int32{TransformPosition(first,NoMirror,r),TransformPosition(last,NoMirror,r)}
	return Box{Min:[3]int32{origin[0]+min(corners[0][0],corners[1][0]),origin[1]+min(corners[0][1],corners[1][1]),origin[2]+min(corners[0][2],corners[1][2])},Max:[3]int32{origin[0]+max(corners[0][0],corners[1][0]),origin[1]+max(corners[0][1],corners[1][1]),origin[2]+max(corners[0][2],corners[1][2])}}
}
func movePiece(p JigsawPiece,x,y,z int32)JigsawPiece{p.Origin[0]+=x;p.Origin[1]+=y;p.Origin[2]+=z;p.Bounds.Min[0]+=x;p.Bounds.Min[1]+=y;p.Bounds.Min[2]+=z;p.Bounds.Max[0]+=x;p.Bounds.Max[1]+=y;p.Bounds.Max[2]+=z;for i:=range p.Junctions{p.Junctions[i].X+=x;p.Junctions[i].Y+=y;p.Junctions[i].Z+=z};return p}
func directionStep(d string)[3]int32{switch d{case "north":return [3]int32{0,0,-1};case "south":return [3]int32{0,0,1};case "west":return [3]int32{-1,0,0};case "east":return [3]int32{1,0,0};case "up":return [3]int32{0,1,0};case "down":return [3]int32{0,-1,0};default:return [3]int32{}}}
func directionOpposite(d string)string{switch d{case "north":return "south";case "south":return "north";case "east":return "west";case "west":return "east";case "up":return "down";case "down":return "up";default:return ""}}
func asInt32(v any)int32{switch n:=v.(type){case float64:return int32(n);case int32:return n;case int:return int32(n)};return 1}
func cancelAnchor(origin,connector,requested [3]int32)[3]int32{for axis:=range 3{origin[axis]+=requested[axis]-connector[axis]};return origin}
