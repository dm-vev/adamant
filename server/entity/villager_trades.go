package entity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)
//go:embed villager_trades.json
var villagerTradeJSON []byte
 
type villagerTradeAssets struct {
	TradeSets       map[string]villagerTradeSet `json:"trade_sets"`
	TradeTags       map[string]villagerTradeTag `json:"trade_tags"`
	Trades          map[string]villagerTrade   `json:"trades"`
	EnchantmentTags map[string]villagerTradeTag `json:"enchantment_tags"`
	PotionTags      map[string]villagerTradeTag `json:"potion_tags"`
}
type villagerTradeSet struct {
	Amount         int    `json:"amount"`
	AllowDuplicates bool   `json:"allow_duplicates"`
	RandomSequence string `json:"random_sequence"`
	Trades          string `json:"trades"`
}
type villagerTradeTag struct { Values []string `json:"values"` }
type villagerTradeCost struct {
	ID         string         `json:"id"`
	Count      int            `json:"count"`
	Components map[string]any `json:"components"`
}
type villagerTrade struct {
	Wants               villagerTradeCost `json:"wants"`
	AdditionalWants     *villagerTradeCost `json:"additional_wants"`
	Gives               map[string]any     `json:"gives"`
	MaxUses             *int               `json:"max_uses"`
	XP                  *int               `json:"xp"`
	ReputationDiscount  float32            `json:"reputation_discount"`
	MerchantPredicate   map[string]any     `json:"merchant_predicate"`
	GivenItemModifier   json.RawMessage    `json:"given_item_modifier"`
	DoubleTradeEnchants string             `json:"double_trade_price_enchantments"`
}

var (
	villagerTradeAssetsOnce sync.Once
	villagerTrades villagerTradeAssets
)

func loadVillagerTradeAssets() *villagerTradeAssets {
	villagerTradeAssetsOnce.Do(func() {
		if err := json.Unmarshal(villagerTradeJSON, &villagerTrades); err != nil {
			panic(fmt.Errorf("decode embedded villager trade assets: %w", err))
		}
	})
	return &villagerTrades
}

func (v *Villager) ensureVillagerOffers() error {
	b := v.behaviour()
	if b.offersGenerated { return nil }
	if b.profession == "" || b.profession == "minecraft:none" || b.profession == "minecraft:nitwit" || b.level < 1 || b.level > 5 {
		b.offersGenerated = true
		return nil
	}
	if v.tx==nil{return fmt.Errorf("villager trade generation requires an active world transaction")}
	start:=len(b.offers)
	if err:=v.addVillagerTradeLevel(b.level);err!=nil { b.offers=b.offers[:start];return err }
	b.offersGenerated = true
	return nil
}

func (v *Villager) addVillagerTradeLevel(level int) error {
	b := v.behaviour()
	if level < 1 || level > 5 || b.profession == "" || b.profession == "minecraft:none" || b.profession == "minecraft:nitwit" { return nil }
	assets := loadVillagerTradeAssets()
	setName := fmt.Sprintf("%s/level_%d", b.profession, level)
	set, ok := assets.TradeSets[setName]
	if !ok || set.Amount <= 0 { return nil }
	tagName := strings.TrimPrefix(set.Trades, "#")
	if _,ok:=assets.TradeTags[tagName];!ok{return nil}
	choices := villagerTagValues(assets.TradeTags,tagName,make(map[string]bool))
	if len(choices)==0{return nil}
	start:=len(b.offers)
	random := java26.NewXoroshiroRandom(villagerTradeSeed(v, set.RandomSequence))
	for made := 0; made < set.Amount && len(choices) != 0; {
		index := int(random.NextInt(int32(len(choices))))
		tradeName := choices[index]
		definition, exists := assets.Trades[tradeName]
		offer, valid := MerchantOffer{}, false
		if exists {
			var err error
			offer,valid,err=v.makeVillagerOffer(definition,level,assets,&random)
			if err!=nil { b.offers=b.offers[:start];return err }
		}
		if valid {
			b.offers = append(b.offers, offer)
			made++
			if !set.AllowDuplicates { choices = append(choices[:index], choices[index+1:]...) }
		} else {
			choices = append(choices[:index], choices[index+1:]...)
		}
	}
	return nil
}
func villagerTagValues(tags map[string]villagerTradeTag,name string,visiting map[string]bool) []string {
	if visiting[name]{return nil}
	tag,ok:=tags[name];if !ok{return nil}
	visiting[name]=true
	values:=make([]string,0,len(tag.Values))
	for _,value:=range tag.Values {
		if strings.HasPrefix(value,"#"){values=append(values,villagerTagValues(tags,strings.TrimPrefix(value,"#"),visiting)...);continue}
		values=append(values,value)
	}
	delete(visiting,name)
	return values
}

func villagerTradeSeed(v *Villager, sequence string) int64 {
	seed := uint64(v.tx.World().Seed())
	id := v.H().UUID()
	for _, value := range id { seed = (seed ^ uint64(value)) * 0x100000001b3 }
	for _, value := range []byte(sequence) { seed = (seed ^ uint64(value)) * 0x100000001b3 }
	pos := v.Position()
	seed ^= uint64(int64(math.Floor(pos[0]))) * 0x9e3779b97f4a7c15
	seed ^= uint64(int64(math.Floor(pos[2]))) * 0xc2b2ae3d27d4eb4f
	return int64(seed)
}

func (v *Villager) makeVillagerOffer(def villagerTrade, tier int, assets *villagerTradeAssets, random *java26.Random) (MerchantOffer, bool, error) {
	if !villagerTradePredicate(def.MerchantPredicate, v.Variant()) { return MerchantOffer{}, false, nil }
	output, err := villagerTradeOutputStack(def.Gives)
	if err != nil || output.Empty() { return MerchantOffer{}, false, nil }
	additionalCost := 0
	if len(def.GivenItemModifier) != 0 {
		var modifiers []map[string]any
		if err := json.Unmarshal(def.GivenItemModifier, &modifiers); err != nil {
			var modifier map[string]any
			if json.Unmarshal(def.GivenItemModifier, &modifier) != nil { return MerchantOffer{}, false, nil }
			modifiers = []map[string]any{modifier}
		}
		var valid bool
		for _, modifier := range modifiers {
			output, additionalCost, valid, err = applyVillagerItemModifier(v,v.tx,output,additionalCost,modifier,assets,random)
			if err != nil { return MerchantOffer{},false,err }
			if !valid { return MerchantOffer{}, false, nil }
		}
	}
	if output.Empty() { return MerchantOffer{}, false, nil }
	if def.DoubleTradeEnchants != "" && villagerOfferHasTaggedEnchantment(output, strings.TrimPrefix(def.DoubleTradeEnchants, "#"), assets) { additionalCost *= 2 }
	input, valid := villagerTradeCostStack(def.Wants, additionalCost)
	if !valid { return MerchantOffer{}, false, nil }
	var secondary item.Stack
	if def.AdditionalWants != nil {
		secondary, valid = villagerTradeCostStack(*def.AdditionalWants, 0)
		if !valid { return MerchantOffer{}, false, nil }
	}
	maxUses, xp := 4, 1
	if def.MaxUses != nil { maxUses = *def.MaxUses }
	if def.XP != nil { xp = *def.XP }
	if maxUses < 1 { maxUses = 1 }
	if xp < 0 { xp = 0 }
	return MerchantOffer{Input: input, SecondaryInput: secondary, Output: output, MaxUses: maxUses, XP: xp,
		RewardXP: true, PriceMultiplier: max(0, def.ReputationDiscount), Tier: tier}, true,nil
}

func villagerTradeCostStack(cost villagerTradeCost, addition int) (item.Stack, bool) {
	count:=cost.Count
	if count==0 { count=1 }
	it, ok := worldItemForVillagerTrade(cost.ID)
	if !ok { return item.Stack{}, false }
	count = max(0, min(count+addition, item.NewStack(it, 1).MaxCount()))
	if count == 0 { return item.Stack{}, false }
	data := map[string]any{"id": cost.ID, "count": count}
	if cost.Components != nil { data["components"] = cost.Components }
	stack, err := item.StackFromJavaNBT(data)
	return stack, err == nil && !stack.Empty()
}

func villagerTradeOutputStack(data map[string]any) (item.Stack,error) {
	id:=stringValue(data["id"])
	if id=="minecraft:tipped_arrow" {
		count:=number(data["count"]);if count<1{count=1}
		return item.NewStack(item.Arrow{Tip:potion.Water()},count),nil
	}
	return item.StackFromJavaNBT(data)
}
func worldItemForVillagerTrade(id string) (world.Item, bool) {
	return world.ItemByName(id, 0)
}

func villagerTradePredicate(predicate map[string]any, variant string) bool {
	if len(predicate) == 0 { return true }
	condition, _ := predicate["predicate"].(map[string]any)
	predicates, _ := condition["minecraft:predicates"].(map[string]any)
	wanted, exists := predicates["minecraft:villager/variant"]
	if !exists { return false }
	switch value := wanted.(type) {
	case string:
		return value == variant
	case []any:
		for _, item := range value { if item == variant { return true } }
	}
	return false
}

func applyVillagerItemModifier(v *Villager, tx *world.Tx, stack item.Stack, additionalCost int, modifier map[string]any, assets *villagerTradeAssets, random *java26.Random) (item.Stack, int, bool, error) {
	kind, _ := modifier["type"].(string)
	switch kind {
	case "minecraft:enchant_randomly":
		options, _ := modifier["options"].(string)
		candidates := villagerEnchantmentsForTag(strings.TrimPrefix(options, "#"), assets)
		if len(candidates) == 0 { return stack, additionalCost, false,nil }
		enchantment := candidates[random.NextInt(int32(len(candidates)))]
		if enchantment.MaxLevel()<1 { return stack, additionalCost, false,nil }
		level := 1 + int(random.NextInt(int32(enchantment.MaxLevel())))
		stack = stack.WithEnchantments(item.NewEnchantment(enchantment, level))
		if boolValue(modifier["include_additional_cost_component"]) { additionalCost += 2 + int(random.NextInt(int32(5+level*10))) + 3*level }
		return stack, additionalCost, len(stack.Enchantments()) != 0,nil
	case "minecraft:enchant_with_levels":
		levels, _ := modifier["levels"].(map[string]any)
		minLevel, maxLevel := number(levels["min"]), number(levels["max"])
		if maxLevel < minLevel { return stack, additionalCost, false,nil }
		baseLevel := minLevel + int(random.NextInt(int32(maxLevel-minLevel+1)))
		stack, ok := enchantVillagerEquipment(stack, baseLevel, strings.TrimPrefix(stringValue(modifier["options"]), "#"), assets, random)
		if !ok { return stack, additionalCost, false,nil }
		if boolValue(modifier["include_additional_cost_component"]) && baseLevel > 0 { additionalCost += baseLevel }
		return stack, additionalCost, true,nil
	case "minecraft:exploration_map":
		mapStack,valid,err:=villagerExplorationMap(v,tx,stack,modifier)
		if err!=nil{return stack,additionalCost,false,err}
		return mapStack,additionalCost,valid,nil
	case "minecraft:filtered":
		return stack, additionalCost, villagerItemFilter(stack, modifier["item_filter"]),nil
	case "minecraft:set_random_dyes":
		return villagerSetRandomDyes(stack, random), additionalCost, true,nil
	case "minecraft:set_stew_effect":
		return villagerSetStewEffect(modifier["effects"], random), additionalCost, true,nil
	case "minecraft:set_potion":
		return villagerSetPotion(stack,stringValue(modifier["id"])),additionalCost,true,nil
	case "minecraft:set_random_potion":
		tag:=strings.TrimPrefix(stringValue(modifier["options"]),"#")
		options, ok:=assets.PotionTags[tag]
		if !ok||len(options.Values)==0{return item.Stack{},additionalCost,false,nil}
		potionID:=options.Values[random.NextInt(int32(len(options.Values)))]
		return villagerSetPotion(stack,potionID),additionalCost,true,nil
	default:
		return stack, additionalCost, false,nil
	}
}
func villagerExplorationMap(v *Villager, tx *world.Tx, stack item.Stack, modifier map[string]any) (item.Stack,bool,error) {
	mapItem,ok:=stack.Item().(item.FilledMap)
	if !ok{return stack,false,nil}
	if tx==nil{return stack,false,fmt.Errorf("villager exploration map requires an active world transaction")}
	destination:=strings.TrimPrefix(stringValue(modifier["destination"]),"#")
	if destination==""{return stack,false,fmt.Errorf("villager exploration map has no destination structure tag")}
	decorationName:=stringValue(modifier["decoration"])
	if decorationName==""{decorationName="minecraft:woodland_mansion"}
	decoration,ok:=villagerMapDecorationType(decorationName)
	if !ok{return stack,false,fmt.Errorf("villager exploration map has unsupported decoration %q",decorationName)}
	zoom:=2
	if _,exists:=modifier["zoom"];exists{zoom=number(modifier["zoom"])}
	if zoom<0||zoom>4{return stack,false,fmt.Errorf("villager exploration map zoom %d is outside Java's range",zoom)}
	searchRadius:=50
	if _,exists:=modifier["search_radius"];exists{searchRadius=number(modifier["search_radius"])}
	skipExisting:=true
	if configured,exists:=modifier["skip_existing_chunks"].(bool);exists{skipExisting=configured}
	origin:=cube.PosFromVec3(v.Position())
	if skipExisting {
		if _,err:=tx.World().RecordStructureAt(destination,origin);err!=nil{return stack,false,err}
	}
	location,found,err:=tx.World().FindNearestMapFeature(destination,origin,searchRadius,skipExisting)
	if err!=nil{return stack,false,err}
	if !found{return stack,false,nil}
	data,err:=tx.CreateMap(location.Position,uint8(zoom))
	if err!=nil{return stack,false,err}
	data.UnlimitedTracking=true
	if err:=tx.World().StoreMapData(data);err!=nil{return stack,false,err}
	if _,err:=tx.RenderMapBiomePreview(data.ID);err!=nil{return stack,false,err}
	if err:=tx.AddMapDecorationAt(data.ID,decoration,8,1,location.Position,"+");err!=nil{return stack,false,err}
	if err:=tx.World().RecordStructureReference(location);err!=nil{return stack,false,err}
	mapItem.NativeID,mapItem.HasNativeID=data.ID,true
	mapItem.JavaID,mapItem.HasJavaID=data.JavaID,data.HasJavaID
	return stack.WithItem(mapItem),true,nil
}
func villagerMapDecorationType(name string)(byte,bool) {
	switch strings.TrimPrefix(name,"minecraft:") {
	case "woodland_mansion":return 14,true
	case "monument":return 15,true
	case "village_desert":return 17,true
	case "village_plains":return 18,true
	case "village_savanna":return 19,true
	case "village_snowy":return 20,true
	case "village_taiga":return 21,true
	case "jungle_temple":return 22,true
	case "swamp_hut":return 23,true
	case "trial_chambers":return 24,true
	default:return 0,false
	}
}

func villagerItemFilter(stack item.Stack, raw any) bool {
	filter, _ := raw.(map[string]any)
	if items, ok := filter["items"].(string); ok { name, _ := stack.Item().EncodeItem(); if items != name { return false } }
	predicates, _ := filter["predicates"].(map[string]any)
	for name := range predicates {
		switch name {
		case "minecraft:enchantments", "minecraft:stored_enchantments":
			if len(stack.Enchantments()) == 0 { return false }
		case "minecraft:dyed_color":
			if !villagerStackHasDye(stack) { return false }
		case "minecraft:map_id":
			mapItem,ok:=stack.Item().(item.FilledMap)
			if !ok||!mapItem.HasJavaID{return false}
		default:
			return false
		}
	}
	return true
}

func villagerSetRandomDyes(stack item.Stack, random *java26.Random) item.Stack {
	colors := item.Colours()
	count := 1
	for range 2 { if random.NextFloat() < .75 { count++ } }
	r, g, b, intensity := 0, 0, 0, 0
	for range count {
		c := colors[random.NextInt(int32(len(colors)))].RGBA()
		r += int(c.R); g += int(c.G); b += int(c.B); intensity += max(int(c.R), int(c.G), int(c.B))
	}
	r /= count; g /= count; b /= count; intensity /= count
	maxChannel := max(r, g, b)
	if maxChannel == 0 { return stack }
	colour := color.RGBA{R:uint8(r*intensity/maxChannel),G:uint8(g*intensity/maxChannel),B:uint8(b*intensity/maxChannel),A:0xff}
	switch current := stack.Item().(type) {
	case item.Helmet:
		if _, ok := current.Tier.(item.ArmourTierLeather); ok { current.Tier = item.ArmourTierLeather{Colour:colour}; return stack.WithItem(current) }
	case item.Chestplate:
		if _, ok := current.Tier.(item.ArmourTierLeather); ok { current.Tier = item.ArmourTierLeather{Colour:colour}; return stack.WithItem(current) }
	case item.Leggings:
		if _, ok := current.Tier.(item.ArmourTierLeather); ok { current.Tier = item.ArmourTierLeather{Colour:colour}; return stack.WithItem(current) }
	case item.Boots:
		if _, ok := current.Tier.(item.ArmourTierLeather); ok { current.Tier = item.ArmourTierLeather{Colour:colour}; return stack.WithItem(current) }
	case item.LeatherHorseArmour:
		components:=map[string]any{}
		if raw,ok:=stack.Value("java_components_json");ok { if encoded,ok:=raw.(string);ok { _=json.Unmarshal([]byte(encoded),&components) } }
		components["minecraft:dyed_color"]=map[string]any{"rgb":int32(colour.R)<<16|int32(colour.G)<<8|int32(colour.B),"show_in_tooltip":true}
		if encoded,err:=json.Marshal(components);err==nil { return stack.WithValue("java_components_json",string(encoded)) }
	}
	return stack
}

func villagerStackHasDye(stack item.Stack) bool {
	if raw,ok:=stack.Value("java_components_json");ok {
		if encoded,ok:=raw.(string);ok { var components map[string]any;if json.Unmarshal([]byte(encoded),&components)==nil { if _,exists:=components["minecraft:dyed_color"];exists{return true} } }
	}
	switch current := stack.Item().(type) {
	case item.Helmet: _, ok := current.Tier.(item.ArmourTierLeather); return ok
	case item.Chestplate: _, ok := current.Tier.(item.ArmourTierLeather); return ok
	case item.Leggings: _, ok := current.Tier.(item.ArmourTierLeather); return ok
	case item.Boots: _, ok := current.Tier.(item.ArmourTierLeather); return ok
	case item.LeatherHorseArmour:return false
	default:return false
	}
}

func villagerSetStewEffect(effects any, random *java26.Random) item.Stack {
	name := ""
	var duration time.Duration
	if list, ok := effects.([]any); ok && len(list) > 0 {
		row, _ := list[random.NextInt(int32(len(list)))].(map[string]any)
		name = stringValue(row["type"])
		duration = time.Duration(number(row["duration"])) * time.Second
	}
	var stew item.StewType
	switch name {
	case "minecraft:night_vision": stew = item.NightVisionPoppyStew()
	case "minecraft:jump_boost": stew = item.JumpBoostStew()
	case "minecraft:weakness": stew = item.WeaknessStew()
	case "minecraft:blindness": stew = item.BlindnessBluetStew()
	case "minecraft:poison": stew = item.PoisonStew()
	case "minecraft:saturation": stew = item.SaturationDandelionStew()
	default: return item.Stack{}
	}
	return item.NewSuspiciousStewStack(stew.WithDuration(duration))
}

func villagerSetPotion(stack item.Stack, id string) item.Stack {
	value, ok := villagerPotionFromID(id)
	if !ok { return item.Stack{} }
	switch stack.Item().(type) {
	case item.Potion: return stack.WithItem(item.Potion{Type:value})
	case item.Arrow: return stack.WithItem(item.Arrow{Tip:value})
	default: return item.Stack{}
	}
}

var villagerPotionNames = []string{"water","mundane","long_mundane","thick","awkward","night_vision","long_night_vision","invisibility","long_invisibility","leaping","long_leaping","strong_leaping","fire_resistance","long_fire_resistance","swiftness","long_swiftness","strong_swiftness","slowness","long_slowness","water_breathing","long_water_breathing","healing","strong_healing","harming","strong_harming","poison","long_poison","strong_poison","regeneration","long_regeneration","strong_regeneration","strength","long_strength","strong_strength","weakness","long_weakness","wither","turtle_master","long_turtle_master","strong_turtle_master","slow_falling","long_slow_falling","strong_slowness"}

func villagerPotionFromID(id string) (potion.Potion,bool) {
	id=strings.TrimPrefix(id,"minecraft:")
	potions:=potion.All()
	for index,name:=range villagerPotionNames { if name==id&&index<len(potions){return potions[index],true} }
	return potion.Potion{},false
}
func villagerPotionID(value potion.Potion) string {
	index:=int(value.Uint8())
	if index>=0&&index<len(villagerPotionNames){return "minecraft:"+villagerPotionNames[index]}
	return "minecraft:water"
}

func enchantVillagerEquipment(stack item.Stack, level int, tag string, assets *villagerTradeAssets, random *java26.Random) (item.Stack, bool) {
	value, ok := stack.Item().(item.Enchantable)
	if !ok || value.EnchantmentValue() <= 0 { return stack, false }
	level += 1 + int(random.NextInt(int32(value.EnchantmentValue()/4+1))) + int(random.NextInt(int32(value.EnchantmentValue()/4+1)))
	span := (random.NextFloat()+random.NextFloat()-1)*.15
	level = max(1, int(math.Floor(float64(level)+float64(level)*float64(span)+.5)))
	candidates := villagerEnchantmentsForTag(tag, assets)
	type choice struct { enchant item.EnchantmentType; level, weight int }
	available := make([]choice, 0, len(candidates))
	for _, enchant := range candidates {
		if !enchant.CompatibleWithItem(stack.Item()) { continue }
		for enchantLevel := enchant.MaxLevel(); enchantLevel >= 1; enchantLevel-- {
			minCost, maxCost := enchant.Cost(enchantLevel)
			if level >= minCost && level <= maxCost { available = append(available, choice{enchant,enchantLevel,villagerEnchantmentWeight(enchant)}); break }
		}
	}
	selected := make([]item.Enchantment, 0, 2)
	for len(available) != 0 {
		weight := 0; for _, candidate := range available { weight += candidate.weight }
		pick := int(random.NextInt(int32(weight)))
		var chosen choice
		for _, candidate := range available { pick -= candidate.weight; if pick < 0 { chosen = candidate; break } }
		selected = append(selected, item.NewEnchantment(chosen.enchant, chosen.level))
		remaining := available[:0]
		for _, candidate := range available { if chosen.enchant.CompatibleWithEnchantment(candidate.enchant) && candidate.enchant.CompatibleWithEnchantment(chosen.enchant) { remaining = append(remaining,candidate) } }
		available = remaining
		if random.NextInt(50) > int32(level) { break }
		level /= 2
	}
	if len(selected) == 0 { return stack, false }
	return stack.WithEnchantments(selected...), true
}
func villagerEnchantmentWeight(enchant item.EnchantmentType) int {
	switch enchant.Rarity().Name() {
	case "Common":return 10
	case "Uncommon":return 5
	case "Rare":return 2
	case "Very Rare":return 1
	default:return 1
	}
}

func villagerEnchantmentsForTag(tag string, assets *villagerTradeAssets) []item.EnchantmentType {
	if tag == "" { return item.Enchantments() }
	values:=villagerTagValues(assets.EnchantmentTags,tag,make(map[string]bool))
	allowed := make(map[string]struct{},len(values))
	for _, name := range values { allowed[name] = struct{}{} }
	result := make([]item.EnchantmentType,0,len(allowed))
	for _, enchant := range item.Enchantments() { if _, ok := allowed["minecraft:"+strings.ToLower(strings.ReplaceAll(enchant.Name()," ","_"))]; ok { result = append(result,enchant) } }
	return result
}

func villagerOfferHasTaggedEnchantment(stack item.Stack, tag string, assets *villagerTradeAssets) bool {
	allowed := make(map[string]struct{})
	for _,name:=range villagerTagValues(assets.EnchantmentTags,tag,make(map[string]bool)){allowed[name]=struct{}{}}
	for _, enchant := range stack.Enchantments() { if _, ok := allowed["minecraft:"+strings.ToLower(strings.ReplaceAll(enchant.Type().Name()," ","_"))]; ok { return true } }
	return false
}

func number(value any) int { switch n := value.(type) { case float64: return int(n); case int: return n; case int32: return int(n) }; return 0 }
func stringValue(value any) string { text, _ := value.(string); return text }
func boolValue(value any) bool { result, _ := value.(bool); return result }
