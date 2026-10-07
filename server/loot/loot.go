package loot

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

//go:embed assets/minecraft/loot_table assets/minecraft/tags/item assets/minecraft/tags/enchantment
var assets embed.FS
type table struct { Pools []pool `json:"pools"`; RandomSequence string `json:"random_sequence"`; Modifier modifierList `json:"modifier"` }
type pool struct { Rolls any `json:"rolls"`; BonusRolls any `json:"bonus_rolls"`; Entries []entry `json:"entries"`; Condition *condition `json:"condition"`; Conditions []condition `json:"conditions"` }
type entry struct { Type string `json:"type"`; Name string `json:"name"`; Value any `json:"value"`; Inline *table `json:"-"`; Weight int `json:"weight"`; Quality int `json:"quality"`; Children []entry `json:"children"`; Entries []entry `json:"entries"`; Entry *entry `json:"entry"`; Condition *condition `json:"condition"`; Conditions []condition `json:"conditions"`; Functions []modifier `json:"functions"`; Modifier modifierList `json:"modifier"` }
func (e *entry) UnmarshalJSON(b []byte) error {
	type alias entry
	var decoded alias
	if err:=json.Unmarshal(b,&decoded);err!=nil{return err}
	var fields map[string]json.RawMessage
	if err:=json.Unmarshal(b,&fields);err!=nil{return err}
	if _,ok:=fields["weight"];!ok{decoded.Weight=1}
	if raw,ok:=fields["value"];ok && len(raw)>0 && raw[0]=='{' {
		var inline table
		if err:=json.Unmarshal(raw,&inline);err!=nil{return fmt.Errorf("decode inline loot table: %w",err)}
		decoded.Inline=&inline
	}
	*e=entry(decoded)
	return nil
}
type modifier struct { Type string `json:"type"`; Count any `json:"count"`; Min any `json:"min"`; Max any `json:"max"`; Add any `json:"add"`; Multiplier any `json:"multiplier"`; Condition *condition `json:"condition"`; Conditions []condition `json:"conditions"`; Enchantments map[string]any `json:"enchantments"`; Options any `json:"options"`; OnlyCompatible *bool `json:"only_compatible"`; Enchantment string `json:"enchantment"`; Formula string `json:"formula"`; Levels any `json:"levels"`; Damage any `json:"damage"`; Potion string `json:"id"`; Predicate json.RawMessage `json:"predicate"`; Components map[string]any `json:"components"`; Name any `json:"name"`; Raw map[string]any `json:"-"` }
type modifierList []modifier
func matchItemComponents(raw any,stack item.Stack)(bool,error){
	matchers,ok:=raw.(map[string]any);if !ok{return false,fmt.Errorf("invalid item predicate component matchers %T",raw)}
	if exact,ok:=matchers["components"];ok {
		components,ok:=exact.(map[string]any);if !ok{return false,fmt.Errorf("invalid exact item components %T",exact)}
		for id,value:=range components {
			if id!="minecraft:enchantments"{return false,fmt.Errorf("unsupported exact item component %q",id)}
			required,ok:=value.(map[string]any);if !ok{return false,fmt.Errorf("invalid exact enchantments component %T",value)}
			actual:=stack.Enchantments()
			if len(actual)!=len(required){return false,nil}
			for enchantment,level:=range required {
				want,ok:=level.(float64);if !ok{return false,fmt.Errorf("invalid exact enchantment level %T",level)}
				if lootEnchantmentLevel(stack,enchantment)!=int(want){return false,nil}
			}
		}
	}
	if predicates,ok:=matchers["predicates"];ok {
		partial,ok:=predicates.(map[string]any);if !ok{return false,fmt.Errorf("invalid partial item predicates %T",predicates)}
		for id,value:=range partial {
			if id!="minecraft:enchantments"{return false,fmt.Errorf("unsupported item predicate component %q",id)}
			list,ok:=value.([]any);if !ok{return false,fmt.Errorf("invalid enchantment subpredicate list %T",value)}
			for _,rawPredicate:=range list {
				predicate,ok:=rawPredicate.(map[string]any);if !ok{return false,fmt.Errorf("invalid enchantment subpredicate %T",rawPredicate)}
				matches,err:=matchesEnchantmentSubpredicate(predicate,stack);if err!=nil||!matches{return matches,err}
			}
		}
	}
	for key:=range matchers{if key!="components"&&key!="predicates"{return false,fmt.Errorf("unsupported item predicate matcher %q",key)}}
	return true,nil
}
func matchesEnchantmentSubpredicate(predicate map[string]any,stack item.Stack)(bool,error){
	var allowed []string
	if raw,ok:=predicate["enchantments"];ok {
		switch value:=raw.(type){
		case string:allowed=[]string{value}
		case []any:for _,v:=range value{name,ok:=v.(string);if !ok{return false,fmt.Errorf("invalid enchantment predicate item %T",v)};allowed=append(allowed,name)}
		default:return false,fmt.Errorf("invalid enchantment predicate item set %T",raw)
		}
	}
	var levelRange any
	if raw,ok:=predicate["levels"];ok{levelRange=raw}
	for key:=range predicate{if key!="enchantments"&&key!="levels"{return false,fmt.Errorf("unsupported enchantment subpredicate field %q",key)}}
	if len(allowed)==0&&levelRange==nil{return len(stack.Enchantments())!=0,nil}
	if len(allowed)==0 {
		for _,enchantment:=range stack.Enchantments(){match,err:=matchesIntRange(levelRange,enchantment.Level());if err!=nil{return false,err};if match{return true,nil}}
		return false,nil
	}
	for _,name:=range allowed {
		tag:=strings.HasPrefix(name,"#")
		var tagMembers map[string]struct{}
		if tag {
			var err error
			tagMembers,err=enchantmentTagMembers(strings.TrimPrefix(name,"#"));if err!=nil{return false,err}
		} else if !enchantmentKnown(name) { return false,fmt.Errorf("unknown registered enchantment %q",name) }
		for _,enchanted:=range stack.Enchantments() {
			id:=enchantmentID(enchanted.Type())
			matches:=false
			if tag { _,matches=tagMembers[id] } else { matches=normalize(name)==id }
			if !matches { continue }
			if levelRange==nil{return true,nil}
			matches,err:=matchesIntRange(levelRange,enchanted.Level());if err!=nil{return false,err};if matches{return true,nil}
		}
	}
	return false,nil
}
func (m *modifierList) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" { return nil }
	var list []modifier
	if b[0] == '[' { if err := json.Unmarshal(b, &list); err != nil { return err } } else { var one modifier; if err := json.Unmarshal(b, &one); err != nil { return err }; list = []modifier{one} }
	*m = list
	return nil
}
func (m *modifier) UnmarshalJSON(b []byte) error {
	type alias modifier
	var compiled alias
	if err := json.Unmarshal(b, &compiled); err != nil { return err }
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil { return err }
	*m = modifier(compiled)
	m.Raw = raw
	return nil
}
type condition struct { Type string `json:"type"`; Condition string `json:"condition"`; Chance float64 `json:"chance"`; Predicate any `json:"predicate"`; Terms []condition `json:"terms"`; Inverted *condition `json:"term"`; Block string `json:"block"`; Entity string `json:"entity"`; PredicateType string `json:"predicate_type"`; Raw map[string]any `json:"-"` }
func (c *condition) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' { return json.Unmarshal(b, &c.Condition) }
	type alias condition
	var compiled alias
	if err := json.Unmarshal(b, &compiled); err != nil { return err }
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil { return err }
	*c = condition(compiled)
	c.Raw = raw
	return nil
}

var tables map[string]table
var itemTags map[string]map[string]struct{}
var enchantmentTags map[string]map[string]struct{}
var registeredEnchantments map[string]struct{}

func init() {
	tables = make(map[string]table)
	err := fs.WalkDir(assets, "assets/minecraft/loot_table", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		var t table
		if err = json.Unmarshal(b, &t); err != nil {
			return fmt.Errorf("decode builtin loot table %s: %w", path, err)
		}
		id := "minecraft:" + strings.TrimSuffix(strings.TrimPrefix(path, "assets/minecraft/loot_table/"), ".json")
		tables[id] = t
		return nil
	})
	if err != nil {
		panic(err)
	}
	itemTags, err = loadItemTags()
	if err != nil { panic(err) }
	enchantmentTags, registeredEnchantments, err = loadEnchantmentTags()
	if err != nil { panic(err) }
}

 
// Context supplies the source properties used by loot predicates and functions.
// Zero-valued fields are valid, and Random may be supplied by a gameplay caller.
type Context struct {
	Random *java26.Random
	Tool world.Item
	ToolStack item.Stack
	Luck float64
	Looting int
	PlayerKilled bool
	EnchantmentLevel int
	ExplosionRadius float64
	Block string
	Entity string
	OpenWater bool
	Player world.Entity
	Origin mgl64.Vec3
	ThisEntity world.Entity
	SourceEntity world.Entity
	EntityProperties map[string]any
	EntityNBT map[string]any
	SourceEntityType string
	Contents []item.Stack
	Tx *world.Tx
	BlockState world.Block
	Damage world.DamageSource
	RandomSequence func(sequence string, seed int64) *java26.Random
	Biome string
}

// Generate rolls a builtin Java loot table using its native loot operators.
// Unsupported tables/operators are errors; output is never silently discarded.
func Generate(id string, seed int64, ctx Context) ([]item.Stack, error) {
	t, ok := tables[normalize(id)]
	if !ok { return nil, fmt.Errorf("unknown Java loot table %q", id) }
	if ctx.Random == nil {
		switch {
		case seed != 0:
			r := java26.NewXoroshiroRandom(seed)
			ctx.Random = &r
		case ctx.RandomSequence != nil && t.RandomSequence != "":
			ctx.Random = ctx.RandomSequence(t.RandomSequence, seed)
		default:
			return nil, fmt.Errorf("loot table %q needs its Java random sequence stream", id)
		}
	}
	if ctx.Random == nil { return nil, fmt.Errorf("loot table %q random sequence %q returned nil", id, t.RandomSequence) }
	var out []item.Stack
	for _, p := range t.Pools {
		pass, err := testPoolConditions(p, &ctx)
		if err != nil { return nil, err }
		if !pass { continue }
		rolls, err := integerNumber(p.Rolls, &ctx)
		if err != nil { return nil, err }
		bonus, err := number(p.BonusRolls, &ctx)
		if err != nil { return nil, err }
		n := rolls + int(math.Floor(bonus*ctx.Luck))
		for range max(0, n) {
			selected, err := choose(p.Entries, &ctx)
			if err != nil { return nil, err }
			for _, e := range selected {
				s, err := makeStack(e, &ctx)
				if err != nil { return nil, err }
				if !s.Empty() { out = append(out, s) }
			}
		}
	}
	return out, nil
}
func normalize(s string) string { if !strings.Contains(s, ":") { return "minecraft:"+s }; return s }
func integerNumber(v any, c *Context) (int, error) {
	switch n := v.(type) {
	case nil: return 0, nil
	case float64: return int(math.Floor(n)), nil
	case int: return n, nil
	case int32: return int(n), nil
	case json.Number: x,e:=n.Int64();return int(x),e
	case map[string]any:
		switch n["type"] {
		case "minecraft:uniform":
			lo,e:=integerNumber(n["min"],c);if e!=nil{return 0,e}
			hi,e:=integerNumber(n["max"],c);if e!=nil{return 0,e}
			if hi < lo { return lo,nil }
			return lo+int(c.Random.NextInt(int32(hi-lo+1))),nil
		case "minecraft:constant":
			return integerNumber(n["value"],c)
		default: return 0,fmt.Errorf("unsupported integer loot number provider %v",n["type"])
		}
	}
	return 0,fmt.Errorf("invalid integer loot number %T",v)
}
func number(v any, c *Context) (float64,error) {
	switch n := v.(type) {
	case nil: return 0,nil
	case float64: return n,nil
	case int: return float64(n),nil
	case json.Number: x,e:=n.Float64();return x,e
	case map[string]any:
		t:=n["type"]
		if t=="minecraft:uniform" { lo,e:=number(n["min"],c);if e!=nil{return 0,e};hi,e:=number(n["max"],c);if e!=nil{return 0,e};return lo+float64(c.Random.NextFloat())*(hi-lo),nil }
		if t=="minecraft:binomial" { trials,e:=number(n["n"],c);if e!=nil{return 0,e};prob,e:=number(n["p"],c);if e!=nil{return 0,e};x:=0.0;for range max(0,int(trials)){if c.Random.NextFloat()<float32(prob){x++}};return x,nil }
		if t=="minecraft:linear" {
			base, err := number(n["base"], c); if err != nil { return 0, err }
			per, err := number(n["per_level_above_first"], c); if err != nil { return 0, err }
			level := c.Looting
			if enchantment, ok := n["enchantment"].(string); ok { level = lootEnchantmentLevel(c.ToolStack, enchantment) }
			return base + float64(max(0, level-1))*per, nil
		}
		if t=="minecraft:enchantment_level" {
			return levelBasedValue(n["amount"],c.EnchantmentLevel)
		}
		return 0,fmt.Errorf("unsupported loot number provider %v",t)
	}
	return 0,fmt.Errorf("invalid loot number %T",v)
}
func levelBasedValue(v any,level int)(float64,error){
	switch value:=v.(type){
	case float64:return value,nil
	case int:return float64(value),nil
	case map[string]any:
		switch value["type"]{
		case "minecraft:linear":
			base,ok:=value["base"].(float64);if !ok{return 0,fmt.Errorf("invalid level value base %T",value["base"])}
			per,ok:=value["per_level_above_first"].(float64);if !ok{return 0,fmt.Errorf("invalid level value increment %T",value["per_level_above_first"])}
			return base+float64(max(0,level-1))*per,nil
		case "minecraft:levels_squared":
			added,ok:=value["added"].(float64);if !ok{return 0,fmt.Errorf("invalid squared-level addition %T",value["added"])}
			return float64(level*level)+added,nil
		default:return 0,fmt.Errorf("unsupported enchantment level value %v",value["type"])
		}
	default:return 0,fmt.Errorf("invalid enchantment level value %T",v)
	}
}

func testPoolConditions(p pool, c *Context) (bool, error) {
	if p.Condition != nil { ok, err := testCondition(*p.Condition, c); if err != nil || !ok { return ok, err } }
	return testConditions(p.Conditions, c)
}
func testConditions(cs []condition,c *Context)(bool,error){ for _,x:=range cs { ok,e:=testCondition(x,c);if e!=nil||!ok{return ok,e} };return true,nil }
func testCondition(x condition,c *Context)(bool,error){ kind:=x.Type;if kind==""{kind=x.Condition};switch kind {
case "": return true,nil
case "minecraft:survives_explosion":
	if c.ExplosionRadius <= 0 { return true,nil }
	return c.Random.NextFloat() <= float32(1/c.ExplosionRadius),nil
case "minecraft:killed_by_player": return c.PlayerKilled,nil
case "minecraft:random_chance": return c.Random.NextFloat()<float32(x.Chance),nil
case "minecraft:entity_properties": return testEntityPredicate(x,c)
case "minecraft:match_tool": return matchToolPredicate(x.Predicate,c)
case "minecraft:all_of": return testConditions(x.Terms,c)
case "minecraft:any_of": for _,t:=range x.Terms{ok,e:=testCondition(t,c);if e!=nil{return false,e};if ok{return true,nil}};return false,nil
case "minecraft:inverted": if x.Inverted==nil{return false,fmt.Errorf("loot inverted predicate missing term")};ok,e:=testCondition(*x.Inverted,c);return !ok,e
case "minecraft:tool/can_silk_touch": return lootEnchantmentLevel(c.ToolStack, "minecraft:silk_touch") > 0,nil
case "minecraft:tool/can_shear": _,ok:=c.Tool.(item.Shears);if !ok&&!c.ToolStack.Empty(){_,ok=c.ToolStack.Item().(item.Shears)};return ok,nil
default: return false,fmt.Errorf("unsupported loot condition %q",kind)
} }

func matchToolPredicate(raw any, c *Context) (bool,error) {
	if c.ToolStack.Empty() && c.Tool == nil { return false,nil }
	if raw == nil { return true,nil }
	var id string
	count:=1
	stack:=c.ToolStack
	if !stack.Empty() { id,_=stack.Item().EncodeItem(); count=stack.Count() } else { id,_=c.Tool.EncodeItem() }
	predicate,ok:=raw.(map[string]any)
	if !ok { return false,fmt.Errorf("invalid item predicate %T",raw) }
	if items,ok:=predicate["items"];ok {
		matches:=false
		switch value:=items.(type) {
		case string:
			if strings.HasPrefix(value,"#") { matches=itemInTag(id,value[1:]) } else { matches=normalize(id)==normalize(value) }
		case []any:
			for _,v:=range value { name,ok:=v.(string);if !ok { return false,fmt.Errorf("invalid item predicate item %T",v) };if strings.HasPrefix(name,"#"){matches=matches||itemInTag(id,name[1:])}else{matches=matches||normalize(id)==normalize(name)} }
		default: return false,fmt.Errorf("invalid item predicate items %T",items)
		}
		if !matches{return false,nil}
	}
	if countRange,ok:=predicate["count"];ok { matches,err:=matchesIntRange(countRange,count);if err!=nil{return false,err};if !matches{return false,nil} }
	if components,ok:=predicate["components"];ok {
		if stack.Empty(){return false,nil}
		match,err:=matchItemComponents(components,stack);if err!=nil||!match{return match,err}
	}
	return true,nil
}
func matchesIntRange(raw any,value int)(bool,error){
	switch r:=raw.(type){
	case float64:return value==int(r),nil
	case map[string]any:
		if min,ok:=r["min"];ok { n,ok:=min.(float64);if !ok{return false,fmt.Errorf("invalid integer range minimum %T",min)};if value<int(n){return false,nil} }
		if max,ok:=r["max"];ok { n,ok:=max.(float64);if !ok{return false,fmt.Errorf("invalid integer range maximum %T",max)};if value>int(n){return false,nil} }
		return true,nil
	default:return false,fmt.Errorf("invalid integer range %T",raw)
	}
}
func testEntityPredicate(x condition,c *Context)(bool,error){
	entityPresent:=false
	switch x.Entity{
	case "this": entityPresent=c.Entity!=""||c.ThisEntity!=nil||c.EntityProperties!=nil||c.EntityNBT!=nil
	case "attacker": entityPresent=c.SourceEntity!=nil||c.SourceEntityType!=""
	case "attacking_player": entityPresent=c.Player!=nil
	default:return false,nil
	}
	if !entityPresent { return false,nil }
	predicate,ok:=x.Predicate.(map[string]any)
	if !ok { return false,fmt.Errorf("invalid entity predicate %T",x.Predicate) }
	for key,want:=range predicate {
		switch key {
		case "minecraft:type_specific":
			m,ok:=want.(map[string]any);if !ok{return false,fmt.Errorf("invalid type-specific entity predicate %T",want)}
			fishing,ok:=m["minecraft:fishing_hook"];if !ok{return false,fmt.Errorf("unsupported type-specific entity predicate")}
			actual:=c.SourceEntityType;if x.Entity=="this"&&c.Entity!=""{actual=c.Entity}
			if actual=="" { return false,fmt.Errorf("fishing-hook entity predicate requires entity type context") }
			if normalize(actual)!="minecraft:fishing_hook" { return false,nil }
			f,ok:=fishing.(map[string]any);if !ok{return false,fmt.Errorf("invalid fishing-hook predicate %T",fishing)}
			open,ok:=f["in_open_water"];if !ok{return false,fmt.Errorf("unsupported fishing-hook predicate")}
			expected,ok:=open.(bool);if !ok{return false,fmt.Errorf("invalid fishing-hook open-water value %T",open)}
			if c.OpenWater!=expected{return false,nil}
		case "minecraft:entity_type":
			name,ok:=want.(string);if !ok{return false,fmt.Errorf("unsupported entity type predicate %T",want)}
			actual:=c.SourceEntityType;if x.Entity=="this"&&c.Entity!=""{actual=c.Entity}
			if actual=="" { return false,fmt.Errorf("entity type predicate requires entity type context") }
			if strings.HasPrefix(name,"#"){return false,fmt.Errorf("entity type tag predicate %q requires entity registry tag support",name)}
			if normalize(actual)!=normalize(name){return false,nil}
		default:
			if c.EntityProperties==nil{return false,fmt.Errorf("entity predicate %q requires properties not provided by loot context",key)}
			actual,ok:=c.EntityProperties[key];if !ok||!equalJSON(want,actual){return false,nil}
		}
	}
	return true,nil
}
func equalJSON(a,b any)bool{
	switch av:=a.(type){case map[string]any: bv,ok:=b.(map[string]any);if !ok||len(av)!=len(bv){return false};for k,v:=range av{w,ok:=bv[k];if !ok||!equalJSON(v,w){return false}};return true
	case []any:bv,ok:=b.([]any);if !ok||len(av)!=len(bv){return false};for i,v:=range av{if !equalJSON(v,bv[i]){return false}};return true
	default:return fmt.Sprint(a)==fmt.Sprint(b)}
}
func testEntryConditions(e entry, c *Context) (bool, error) {
	if e.Condition != nil { ok, err := testCondition(*e.Condition, c); if err != nil || !ok { return ok, err } }
	return testConditions(e.Conditions, c)
}
func choose(es []entry,c *Context)([]entry,error){
	var opts []entry
	total:=int32(0)
	weights:=make([]int32,0,len(es))
	for _,e:=range es {
		ok,err:=testEntryConditions(e,c);if err!=nil{return nil,err};if !ok{continue}
		w:=int32(max(0,int(math.Floor(float64(e.Weight)+float64(e.Quality)*c.Luck))))
		if w==0{continue}
		opts=append(opts,e);weights=append(weights,w);total+=w
	}
	if len(opts)==0{return nil,nil}
	if len(opts)==1{return expand(opts[0],c)}
	v:=c.Random.NextInt(total)
	for i,w:=range weights{v-=w;if v<0{return expand(opts[i],c)}}
	return expand(opts[len(opts)-1],c)
}
func expand(e entry,c *Context)([]entry,error){
	children:=e.Children;if len(children)==0{children=e.Entries}
	switch e.Type {
	case "minecraft:item","minecraft:block":return []entry{e},nil
	case "minecraft:empty":return nil,nil
	case "minecraft:loot_table":
		var nested table
		if e.Inline != nil {
			nested = *e.Inline
		} else {
			id,ok:=e.Value.(string)
			if !ok || id=="" { id=e.Name }
			if id=="" { return nil,fmt.Errorf("loot table entry has invalid value %T",e.Value) }
			var exists bool
			nested,exists=tables[normalize(id)]
			if !exists{return nil,fmt.Errorf("loot table entry references missing table %q",id)}
		}
		var out []entry
		for _,p:=range nested.Pools{
			pass,err:=testPoolConditions(p,c);if err!=nil{return nil,err};if !pass{continue}
			n,err:=integerNumber(p.Rolls,c);if err!=nil{return nil,err}
			bonus,err:=number(p.BonusRolls,c);if err!=nil{return nil,err}
			n+=int(math.Floor(bonus*c.Luck))
			for range max(0,n){x,err:=choose(p.Entries,c);if err!=nil{return nil,err};out=append(out,x...)}
		}
		return out,nil
	case "minecraft:alternatives":for _,child:=range children{x,err:=choose([]entry{child},c);if err!=nil{return nil,err};if len(x)>0{return x,nil}};return nil,nil
	case "minecraft:sequence":var out []entry;for _,child:=range children{x,err:=choose([]entry{child},c);if err!=nil{return nil,err};if len(x)==0{return nil,nil};out=append(out,x...)};return out,nil
	case "minecraft:group":var out []entry;for _,child:=range children{x,err:=choose([]entry{child},c);if err!=nil{return nil,err};out=append(out,x...)};return out,nil
	case "minecraft:dynamic":return nil,fmt.Errorf("dynamic loot entry %q requires block entity context",e.Value)
	default:return nil,fmt.Errorf("unsupported loot entry type %q",e.Type)
	}
}
func makeStack(e entry, c *Context) (item.Stack, error) {
	id := e.Name
	if id == "" { id, _ = e.Value.(string) }
	if id == "" { return item.Stack{}, fmt.Errorf("loot item entry has no name") }
	count := 1
	components := make(map[string]any)
	setEnchantmentsApplied := false
	mods := append([]modifier(nil), e.Functions...)
	mods = append(mods, e.Modifier...)
	for _, m := range mods {
		if m.Condition != nil { ok, err := testCondition(*m.Condition, c); if err != nil { return item.Stack{}, err }; if !ok { continue } }
		ok, err := testConditions(m.Conditions, c); if err != nil { return item.Stack{}, err }; if !ok { continue }
		switch m.Type {
		case "minecraft:set_count":
			n, err := integerNumber(m.Count, c); if err != nil { return item.Stack{}, err }; count = n
		case "minecraft:limit_count":
			lo, err := integerNumber(m.Min, c); if err != nil { return item.Stack{}, err }; hi, err := integerNumber(m.Max, c); if err != nil { return item.Stack{}, err }
			if count < lo { count = lo }; if hi > 0 && count > hi { count = hi }
		case "minecraft:explosion_decay":
			if c.ExplosionRadius > 0 { kept := 0; probability:=float32(1/c.ExplosionRadius); for range max(0, count) { if c.Random.NextFloat() <= probability { kept++ } }; count = kept }
		case "minecraft:apply_bonus":
			count = applyBonus(count, m, c)
		case "minecraft:enchanted_count_increase":
			level := 0; if m.Enchantment == "minecraft:looting" || m.Enchantment == "minecraft:fortune" { level = lootEnchantmentLevel(c.ToolStack, m.Enchantment) }
			if level > 0 { count += int(c.Random.NextInt(int32(level+1))) }
		case "minecraft:set_damage":
			damage, err := number(m.Damage, c); if err != nil { return item.Stack{}, err }
			components["minecraft:damage"] = int32(damage)
		case "minecraft:set_components":
			for k, v := range m.Components {
				if k == "minecraft:enchantments" || k == "minecraft:stored_enchantments" {
					raw, ok := v.(map[string]any)
					if !ok { return item.Stack{}, fmt.Errorf("invalid enchantment component %T", v) }
					levels := make(map[string]any, len(raw))
					for id, value := range raw {
						level, err := integerNumber(value, c)
						if err != nil { return item.Stack{}, err }
						levels[id] = int32(level)
					}
					v = levels
				}
				components[k] = v
			}
		case "minecraft:set_enchantments":
			add := false
			if m.Add != nil { var ok bool; add, ok = m.Add.(bool); if !ok { return item.Stack{}, fmt.Errorf("invalid set-enchantments add value %T", m.Add) } }
			levels := map[string]any{}
			if existing, ok := components["minecraft:enchantments"].(map[string]any); ok {
				for id, level := range existing { levels[id] = level }
			}
			for id, provider := range m.Enchantments {
				if _, ok := registeredEnchantmentType(id); !ok { return item.Stack{}, fmt.Errorf("set_enchantments references unsupported enchantment %q", id) }
				level, err := integerNumber(provider, c); if err != nil { return item.Stack{}, err }
				if add {
					previous, err := integerNumber(levels[normalize(id)], c)
					if err != nil { return item.Stack{}, err }
					level += previous
				}
				level = max(0, min(255, level))
				if level == 0 { delete(levels, normalize(id)) } else { levels[normalize(id)] = int32(level) }
			}
			components["minecraft:enchantments"] = levels
			setEnchantmentsApplied = true
		case "minecraft:copy_components":
			return item.Stack{}, fmt.Errorf("loot modifier %q requires source entity/block components", m.Type)
		case "minecraft:set_potion":
			components["minecraft:potion_contents"] = map[string]any{"potion": m.Potion}
		case "minecraft:set_name":
			components["minecraft:custom_name"] = m.Name
		case "minecraft:enchant_randomly", "minecraft:enchant_with_levels":
			return item.Stack{}, fmt.Errorf("loot modifier %q requires enchantment registry support", m.Type)
		case "minecraft:furnace_smelt":
			return item.Stack{}, fmt.Errorf("loot modifier %q requires recipe registry support", m.Type)
		case "minecraft:exploration_map", "minecraft:copy_state", "minecraft:set_instrument", "minecraft:set_ominous_bottle_amplifier", "minecraft:set_stew_effect":
			return item.Stack{}, fmt.Errorf("loot modifier %q requires context-specific component support", m.Type)
		case "minecraft:filtered":
			return item.Stack{}, fmt.Errorf("loot modifier %q requires predicate evaluation", m.Type)
		default:
			return item.Stack{}, fmt.Errorf("unsupported loot modifier %q", m.Type)
		}
	}
	if count <= 0 { return item.Stack{}, nil }
	native, err := item.StackFromJavaNBT(map[string]any{"id": id, "count": int32(count), "components": components})
	if err != nil { return item.Stack{}, fmt.Errorf("decode loot item %q: %w", id, err) }
	if setEnchantmentsApplied && id == "minecraft:book" { native = native.WithItem(item.EnchantedBook{}) }
	return native, nil
}

func lootEnchantmentLevel(stack item.Stack, id string) int {
	want := normalize(id)
	for _, e := range stack.Enchantments() {
		if enchantmentID(e.Type()) == want { return e.Level() }
	}
	return 0
}
func registeredEnchantmentType(id string) (item.EnchantmentType, bool) {
	want := normalize(id)
	for _, enchantment := range item.Enchantments() { if enchantmentID(enchantment) == want { return enchantment, true } }
	return nil, false
}


func applyBonus(count int, m modifier, c *Context) int {
	level := lootEnchantmentLevel(c.ToolStack, m.Enchantment)
	if level <= 0 { return count }
	switch m.Formula {
	case "minecraft:ore_drops":
		return count * (max(0, int(c.Random.NextInt(int32(level+2)))-1) + 1)
	case "minecraft:uniform_bonus_count":
		bonus, _ := number(m.Multiplier, c)
		return count + int(c.Random.NextInt(int32(level*int(bonus)+1)))
	case "minecraft:binomial_with_bonus_count":
		probability, _ := number(m.Multiplier, c)
		for range max(0, level) { if c.Random.NextFloat() < float32(probability) { count++ } }
		return count
	}
	return count
}
// RandomSequence returns the builtin random-sequence identifier for a loot table.
func RandomSequence(id string) (string, error) {
	t, ok := tables[normalize(id)]
	if !ok { return "", fmt.Errorf("unknown Java loot table %q", id) }
	if t.RandomSequence == "" { return "", fmt.Errorf("loot table %q has no random sequence", id) }
	return t.RandomSequence, nil
}
