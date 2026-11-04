package world

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The Java block-state codec as data. propertyRules maps the properties of
// Java blocks to native palette properties and back, and blockRules names
// the native block of a Java state and the Java block of a native state.
//
// Forward, each Java property takes the first row that selects its block and
// property and does not pass; a property no row takes keeps its name if the
// native block has it, and is unsupported otherwise. Rows with set then fill
// native properties. In reverse, each native property takes the first row
// with back that selects its block and key; a key no row takes keeps its name
// unless it is namespaced. Rows with restore then fill the Java properties
// the forward direction consumed.

// javaSel selects blocks by name.
type javaSel func(name string) bool

// blocks selects the named vanilla blocks.
func blocks(names ...string) javaSel {
	qualified := make([]string, len(names))
	for i, name := range names {
		qualified[i] = "minecraft:" + name
	}
	return func(name string) bool { return slices.Contains(qualified, name) }
}

// blocksWithPrefix selects the vanilla blocks whose name starts with p.
func blocksWithPrefix(p string) javaSel {
	p = "minecraft:" + p
	return func(name string) bool { return strings.HasPrefix(name, p) }
}

// blocksWithSuffix selects the blocks whose name ends with one of suffixes.
func blocksWithSuffix(suffixes ...string) javaSel {
	return func(name string) bool {
		for _, s := range suffixes {
			if strings.HasSuffix(name, s) {
				return true
			}
		}
		return false
	}
}

// blocksContaining selects the blocks whose name contains s.
func blocksContaining(s string) javaSel {
	return func(name string) bool { return strings.Contains(name, s) }
}

// or selects the blocks either selects.
func (s javaSel) or(other javaSel) javaSel {
	return func(name string) bool { return s(name) || other(name) }
}

// ruleCtx is what a row sees of the state it maps. Forward, javaProps is the
// Java state and nativeProps the native block's default state; in reverse,
// nativeProps is the native state. out is the map being built.
type ruleCtx struct {
	java, native string
	// prop is the Java property and target the native one.
	prop, target string
	javaProps    map[string]any
	nativeProps  map[string]any
	out          map[string]any
}

// fail formats a row's message. {v} is the value, {q} the value quoted,
// {block} the Java block, {native} the native block, {prop} the Java property
// and {key} the native property.
func (c ruleCtx) fail(msg string, value any) error {
	r := strings.NewReplacer("{v}", fmt.Sprint(value), "{q}", fmt.Sprintf("%q", value),
		"{block}", c.java, "{native}", c.native, "{prop}", c.prop, "{key}", c.target)
	return errors.New("java state codec: " + r.Replace(msg))
}

// ruleGate says whether a row needs its native property in the native block.
type ruleGate uint8

const (
	// gateNone maps to the native property whether the block has it or not.
	gateNone ruleGate = iota
	// gateBefore applies the row only when the native block has the property.
	gateBefore
	// gateAfter checks the value first, then applies the row only when the
	// native block has the property.
	gateAfter
)

// ruleSignal is what a forward row returns instead of a native value.
type ruleSignal uint8

const (
	// consumed drops the Java property: the native state does not keep it.
	consumed ruleSignal = iota + 1
	// passed leaves the property to the rows after.
	passed
)

// propertyRule is one row of propertyRules.
type propertyRule struct {
	// java and onNative select the row by Java and native block, both ways.
	java, onNative javaSel
	// when further restricts the row forward.
	when func(c ruleCtx) bool
	// prop is the Java property, matched by prefix when it ends in '*', or
	// props lists several.
	prop  string
	props []string
	// native is the native property, natives the candidates of which the
	// native block's first is taken, and nativePrefix+prop names it.
	native       string
	natives      []string
	nativePrefix string
	gate         ruleGate

	// do maps the Java value forward. It returns the native property when
	// it is not the row's, and the native value, consumed or passed.
	do func(c ruleCtx, value any) (string, any, error)
	// set fills native properties after the Java properties are mapped.
	set func(c ruleCtx) error

	// backKeys lists the native properties back reads, if not native.
	backKeys []string
	// back maps a native value to Java properties.
	back func(c ruleCtx, value any) error
	// restore fills Java properties after the native properties are mapped.
	restore func(c ruleCtx) error
}

func (r *propertyRule) selects(c ruleCtx) bool {
	return (r.java == nil || r.java(c.java)) && (r.onNative == nil || r.onNative(c.native))
}

// matchesProp reports whether the row maps the Java property key forward.
func (r *propertyRule) matchesProp(key string) bool {
	if p, ok := strings.CutSuffix(r.prop, "*"); ok {
		return strings.HasPrefix(key, p)
	}
	return r.prop == key || slices.Contains(r.props, key)
}

// blockRule is one row of blockRules.
type blockRule struct {
	// java selects the row forward, native in reverse.
	java, native javaSel
	// name picks the native block of a Java state, given the name so far.
	name func(registry chunk.BlockRegistry, java string, props map[string]any, name string) (string, error)
	// typ picks the Java block of a native state; "" leaves it to the rows
	// after and to the name maps.
	typ func(name string, props map[string]any) string
	// check rejects native states with no Java form before their properties
	// are mapped.
	check func(name string, props map[string]any) error
	// props maps all properties of the native state in place of the rows.
	props func(name string, props map[string]any) (map[string]any, error)
	// fix adjusts the Java properties of the native state after the rows.
	fix func(name string, props map[string]any, out map[string]any) error
}

var (
	// forwardRules lists by Java property the rows that map it, in order.
	forwardRules map[string][]*propertyRule
	// forwardPrefixRules are the rows matching Java properties by prefix.
	forwardPrefixRules []*propertyRule
	// setRules, backRules (by native property), backPrefixRules and
	// restoreRules list the rows of each phase, in order.
	setRules, backPrefixRules, restoreRules []*propertyRule
	backRules                               map[string][]*propertyRule
)

func init() {
	forwardRules = map[string][]*propertyRule{}
	backRules = map[string][]*propertyRule{}
	for i := range propertyRules {
		r := &propertyRules[i]
		if r.do != nil {
			if strings.HasSuffix(r.prop, "*") {
				forwardPrefixRules = append(forwardPrefixRules, r)
			}
			for _, key := range append([]string{r.prop}, r.props...) {
				if key != "" && !strings.HasSuffix(key, "*") && !slices.Contains(forwardRules[key], r) {
					forwardRules[key] = append(forwardRules[key], r)
				}
			}
		}
		if r.set != nil {
			setRules = append(setRules, r)
		}
		if r.restore != nil {
			restoreRules = append(restoreRules, r)
		}
		if r.back == nil {
			continue
		}
		switch {
		case r.backKeys != nil:
			for _, key := range r.backKeys {
				backRules[key] = append(backRules[key], r)
			}
		case r.native != "":
			backRules[r.native] = append(backRules[r.native], r)
		case r.nativePrefix != "":
			backPrefixRules = append(backPrefixRules, r)
		default:
			panic(fmt.Sprintf("java state rule %d has back but no native property", i))
		}
	}
	// Prefix rows join the rows of the literal properties they match, in
	// table order, so a literal property is looked up once.
	for key, rules := range forwardRules {
		for _, r := range forwardPrefixRules {
			if r.matchesProp(key) {
				rules = append(rules, r)
			}
		}
		slices.SortStableFunc(rules, func(a, b *propertyRule) int { return ruleIndex(a) - ruleIndex(b) })
		forwardRules[key] = rules
	}
}

func ruleIndex(r *propertyRule) int {
	for i := range propertyRules {
		if &propertyRules[i] == r {
			return i
		}
	}
	return -1
}

// resolveJavaState maps a Java state to the native palette by the rules.
func resolveJavaState(registry chunk.BlockRegistry, state BlockState) (NativeState, error) {
	if registry == nil {
		return NativeState{}, fmt.Errorf("java state codec: nil block registry")
	}
	javaName := NormaliseBlockName(state.Name)
	if javaName == "" {
		return NativeState{}, fmt.Errorf("java state codec: empty block name")
	}
	properties := gamedata.BlockPropertiesWithDefaults(javaName, state.Properties)
	paletteName, paletteProperties := javaActorPaletteState(javaName, properties)
	name, props, err := nativeBlockState(registry, paletteName, paletteProperties)
	if err != nil {
		return NativeState{}, err
	}
	block, ok := registry.StateToRuntimeID(name, props)
	if !ok {
		return NativeState{}, fmt.Errorf("java state codec: no native state for %s%v (mapped to %s%v)", javaName, state.Properties, name, props)
	}
	liquid := registry.AirRuntimeID()
	if needsWaterLayer(javaName, properties) {
		liquid, ok = registry.StateToRuntimeID("minecraft:water", waterSourceProperties)
		if !ok {
			return NativeState{}, fmt.Errorf("java state codec: native water source state is unavailable")
		}
	}
	return NativeState{Block: block, Liquid: liquid}, nil
}

// nativeBlockState names the native block of a Java state and maps its
// properties.
func nativeBlockState(registry chunk.BlockRegistry, javaName string, javaProps map[string]any) (string, map[string]any, error) {
	name, err := nativeBlockNameOf(registry, javaName, javaProps)
	if err != nil {
		return "", nil, err
	}
	defaultRID, ok := registry.StateToRuntimeID(name, nil)
	if !ok {
		return "", nil, fmt.Errorf("java state codec: Java block %q has no native block alias (%q)", javaName, name)
	}
	_, schema, ok := registry.RuntimeIDToState(defaultRID)
	if !ok {
		return "", nil, fmt.Errorf("java state codec: registry cannot decode native default state %q", name)
	}
	out := make(map[string]any, len(javaProps)+len(schema))
	c := ruleCtx{java: javaName, native: name, javaProps: javaProps, nativeProps: schema, out: out}
	for key, value := range javaProps {
		target, mapped, err := c.forward(key, value)
		if err != nil {
			return "", nil, err
		}
		if mapped == consumed {
			continue
		}
		if target == "" {
			return "", nil, fmt.Errorf("java state codec: unsupported property %q=%v on %s", key, value, javaName)
		}
		out[target] = mapped
	}
	for _, r := range setRules {
		if r.selects(c) && (r.when == nil || r.when(c)) {
			c.prop, c.target = r.prop, r.native
			if err := r.set(c); err != nil {
				return "", nil, err
			}
		}
	}
	return name, out, nil
}

// nativeBlockNameOf names the native block of a Java state.
func nativeBlockNameOf(registry chunk.BlockRegistry, javaName string, javaProps map[string]any) (string, error) {
	// Available aliases also disambiguate identifiers shared by both palettes:
	// native stone_stairs are Java cobblestone_stairs, not Java stone_stairs.
	name := javaName
	if alias := javaToNativeBlockNames[javaName]; alias != "" && hasNativeBlock(registry, alias) {
		name = alias
	}
	for i := range blockRules {
		r := &blockRules[i]
		if r.name != nil && r.java(javaName) {
			var err error
			if name, err = r.name(registry, javaName, javaProps, name); err != nil {
				return "", err
			}
		}
	}
	return name, nil
}

// forward maps one Java property by the first row that takes it.
func (c ruleCtx) forward(key string, value any) (string, any, error) {
	c.prop = key
	rules, literal := forwardRules[key]
	if !literal {
		rules = forwardPrefixRules
	}
	for _, r := range rules {
		if !literal && !r.matchesProp(key) || !r.selects(c) || r.when != nil && !r.when(c) {
			continue
		}
		c.target = r.native
		switch {
		case r.natives != nil:
			if c.target = firstNativeProperty(c.nativeProps, r.natives); c.target == "" {
				continue
			}
		case r.nativePrefix != "":
			c.target = r.nativePrefix + key
		}
		if r.gate == gateBefore && !hasProperty(c.nativeProps, c.target) {
			continue
		}
		target, mapped, err := r.do(c, value)
		if err != nil {
			return "", nil, err
		}
		if mapped == passed {
			continue
		}
		if target == "" {
			target = c.target
		}
		if r.gate == gateAfter && mapped != consumed && !hasProperty(c.nativeProps, target) {
			continue
		}
		return target, mapped, nil
	}
	if schemaValue, ok := c.nativeProps[key]; ok {
		return key, valueForSchema(value, schemaValue), nil
	}
	return "", nil, nil
}

func firstNativeProperty(schema map[string]any, candidates []string) string {
	for _, candidate := range candidates {
		if _, ok := schema[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func hasProperty(props map[string]any, key string) bool {
	_, ok := props[key]
	return ok
}

// reverseNativeState maps a native state to Java by the rules.
func reverseNativeState(registry chunk.BlockRegistry, state NativeState) (BlockState, error) {
	if registry == nil {
		return BlockState{}, fmt.Errorf("java state codec: nil block registry")
	}
	if state.Block == registry.AirRuntimeID() && state.Liquid != registry.AirRuntimeID() {
		liquidName, _, ok := registry.RuntimeIDToState(state.Liquid)
		if !ok {
			return BlockState{}, fmt.Errorf("java state codec: invalid native liquid runtime ID %d", state.Liquid)
		}
		if isFreeFluid(liquidName) {
			// Free fluid may occupy the secondary layer without a waterlogged block.
			state.Block, state.Liquid = state.Liquid, registry.AirRuntimeID()
		}
	}
	name, properties, ok := registry.RuntimeIDToState(state.Block)
	if !ok {
		return BlockState{}, fmt.Errorf("java state codec: invalid native block runtime ID %d", state.Block)
	}
	javaName := javaBlockOfNative(name, properties)
	if name == "minecraft:melon_stem" || name == "minecraft:pumpkin_stem" {
		// A grown stem facing sideways is attached, a Java block of its own.
		// Scheduled ticks name the stem by its type alone.
		javaName = nativePlantStemJavaType(name, properties)
	}
	var whole func(string, map[string]any) (map[string]any, error)
	for i := range blockRules {
		r := &blockRules[i]
		if r.native == nil || !r.native(name) {
			continue
		}
		if r.check != nil {
			if err := r.check(name, properties); err != nil {
				return BlockState{}, err
			}
		}
		if r.props != nil && whole == nil {
			whole = r.props
		}
	}
	var javaProps map[string]any
	var err error
	if whole != nil {
		javaProps, err = whole(name, properties)
	} else {
		javaProps, err = javaProperties(name, javaName, properties)
	}
	if err != nil {
		return BlockState{}, err
	}
	liquidName, liquidProps, ok := registry.RuntimeIDToState(state.Liquid)
	if !ok {
		return BlockState{}, fmt.Errorf("java state codec: invalid native liquid runtime ID %d", state.Liquid)
	}
	liquidClear := state.Liquid == registry.AirRuntimeID()
	if !liquidClear && liquidName != "minecraft:water" && liquidName != "minecraft:flowing_water" {
		return BlockState{}, fmt.Errorf("java state codec: secondary layer %s%v is not clear or water", liquidName, liquidProps)
	}
	switch waterloggable := hasJavaWaterloggedProperty(javaName); {
	case waterloggable:
		javaProps["waterlogged"] = !liquidClear
	case !liquidClear && !isIntrinsicWaterBlock(javaName):
		return BlockState{}, fmt.Errorf("java state codec: secondary water layer is not valid for native block %s", name)
	}
	for i := range blockRules {
		r := &blockRules[i]
		if r.fix != nil && r.native(name) {
			if err := r.fix(name, properties, javaProps); err != nil {
				return BlockState{}, err
			}
		}
	}
	if javaName == "" {
		return BlockState{}, fmt.Errorf("java state codec: no Java alias for native block %q", name)
	}
	return BlockState{Name: javaName, Properties: javaProps}, nil
}

func isFreeFluid(name string) bool {
	return name == "minecraft:water" || name == "minecraft:flowing_water" || name == "minecraft:lava" || name == "minecraft:flowing_lava"
}

// javaBlockOfNative names the Java block type of a native state.
func javaBlockOfNative(name string, properties map[string]any) string {
	for i := range blockRules {
		r := &blockRules[i]
		if r.typ != nil && r.native(name) {
			if javaName := r.typ(name, properties); javaName != "" {
				return javaName
			}
		}
	}
	return javaBaseNameOfNative(name)
}

// javaBaseNameOfNative names the Java block of a native block by its name
// alone.
func javaBaseNameOfNative(name string) string {
	if javaName := nativeToJavaBlockNames[name]; javaName != "" {
		return javaName
	}
	if wood, ok := strings.CutSuffix(name, "_standing_sign"); ok {
		if wood == "minecraft:darkoak" {
			wood = "minecraft:dark_oak"
		}
		return wood + "_sign"
	}
	if base, ok := strings.CutSuffix(name, "double_cut_copper_slab"); ok {
		return base + "cut_copper_slab"
	}
	if base, ok := strings.CutSuffix(name, "_double_slab"); ok {
		if javaName := nativeToJavaBlockNames[base+"_slab"]; javaName != "" {
			return javaName
		}
		return base + "_slab"
	}
	if strings.HasPrefix(name, "minecraft:light_block_") {
		return "minecraft:light"
	}
	return name
}

// javaProperties maps the properties of a native state to Java.
func javaProperties(nativeName, javaName string, nativeProps map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(nativeProps)+2)
	c := ruleCtx{java: javaName, native: nativeName, nativeProps: nativeProps, out: out}
	for key, value := range nativeProps {
		if err := c.reverse(key, value); err != nil {
			return nil, err
		}
	}
	for _, r := range restoreRules {
		if r.selects(c) {
			c.prop = r.prop
			if err := r.restore(c); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// reverse maps one native property by the first row that takes it.
func (c ruleCtx) reverse(key string, value any) error {
	c.target = key
	for _, r := range backRules[key] {
		if r.selects(c) {
			c.prop = r.prop
			return r.back(c, value)
		}
	}
	for _, r := range backPrefixRules {
		if side, ok := strings.CutPrefix(key, r.nativePrefix); ok && r.selects(c) {
			c.prop = side
			return r.back(c, value)
		}
	}
	if strings.HasPrefix(key, "minecraft:") {
		// Namespaced native metadata is never discarded silently: each key
		// needs a row.
		return fmt.Errorf("java state codec: unhandled native property %q=%v on %s", key, value, c.native)
	}
	c.out[key] = value
	return nil
}

// Forward transforms.

// schemaValue passes the value through as the native schema types it.
func schemaValue(c ruleCtx, value any) (string, any, error) {
	return "", valueForSchema(value, c.nativeProps[c.target]), nil
}

// consume drops the property.
func consume(ruleCtx, any) (string, any, error) { return "", consumed, nil }

// offset checks that the value is an integer from lo to hi and adds delta.
func offset(delta, lo, hi int, msg string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		n, ok := intProperty(value)
		if !ok || n < lo || n > hi {
			return "", nil, c.fail(msg, value)
		}
		return "", int32(n + delta), nil
	}
}

// checkInt checks that the value is an integer from lo to hi and drops it.
func checkInt(lo, hi int, msg string) func(ruleCtx, any) (string, any, error) {
	check := offset(0, lo, hi, msg)
	return func(c ruleCtx, value any) (string, any, error) {
		if _, _, err := check(c, value); err != nil {
			return "", nil, err
		}
		return "", consumed, nil
	}
}

// boolTo maps a boolean, negated if invert, to the native boolean type.
func boolTo(invert bool, msg string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		b, ok := boolProperty(value)
		if !ok {
			return "", nil, c.fail(msg, value)
		}
		return "", nativeBool(b != invert, c.nativeProps[c.target]), nil
	}
}

// checkBool checks that the value is a boolean and drops it.
func checkBool(msg string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		if _, ok := boolProperty(value); !ok {
			return "", nil, c.fail(msg, value)
		}
		return "", consumed, nil
	}
}

// flagOf maps one of values to the native boolean type, true for yes.
func flagOf(msg string, values []string, yes ...string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		text, ok := stringProperty(value)
		if !ok || !slices.Contains(values, text) {
			return "", nil, c.fail(msg, value)
		}
		return "", nativeBool(slices.Contains(yes, text), c.nativeProps[c.target]), nil
	}
}

// checkOneOf checks that the value is one of values and drops it.
func checkOneOf(msg string, values ...string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		text, ok := stringProperty(value)
		if !ok || !slices.Contains(values, text) {
			return "", nil, c.fail(msg, value)
		}
		return "", consumed, nil
	}
}

// javaEnum pairs Java values with native ones. A Java value may appear more
// than once, its first pair mapping it forward.
type javaEnum []struct{ java, native any }

// toEnum maps a Java string by the enum: invalid is the message for a value
// that is no string, unsupported for one the enum lacks.
func toEnum(e javaEnum, invalid, unsupported string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		text, ok := stringProperty(value)
		if !ok {
			return "", nil, c.fail(invalid, value)
		}
		for _, pair := range e {
			if pair.java == text {
				return "", pair.native, nil
			}
		}
		return "", nil, c.fail(unsupported, value)
	}
}

// Reverse transforms.

// drop discards the native property.
func drop(ruleCtx, any) error { return nil }

// toBool writes the native value as a boolean.
func toBool(c ruleCtx, value any) error {
	c.out[c.prop] = nativeBoolValue(value)
	return nil
}

// toBoolIf writes the native value as a boolean if it is one.
func toBoolIf(c ruleCtx, value any) error {
	if b, ok := boolProperty(value); ok {
		c.out[c.prop] = b
	}
	return nil
}

// notBool writes the negated native value as a boolean.
func notBool(c ruleCtx, value any) error {
	c.out[c.prop] = !nativeBoolValue(value)
	return nil
}

// toSame writes the native value as it is.
func toSame(c ruleCtx, value any) error {
	c.out[c.prop] = value
	return nil
}

// plus writes the native integer plus n.
func plus(n int32) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		c.out[c.prop] = intPropertyValue(value) + n
		return nil
	}
}

// toInt writes the native integer.
var toInt = plus(0)

// flagTo writes yes for a true native boolean and no otherwise.
func flagTo(yes, no any) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		if nativeBoolValue(value) {
			c.out[c.prop] = yes
		} else {
			c.out[c.prop] = no
		}
		return nil
	}
}

// flagToIf writes yes or no for a native boolean, if it is one.
func flagToIf(yes, no any) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		if b, ok := boolProperty(value); ok {
			if b {
				c.out[c.prop] = yes
			} else {
				c.out[c.prop] = no
			}
		}
		return nil
	}
}

// fromInts maps a native integer back by the enum, failing with msg for one
// it lacks; with msg empty, such a value maps to nothing.
func fromInts(e javaEnum, msg string) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		n := intPropertyValue(value)
		for _, pair := range e {
			if pair.native == n {
				c.out[c.prop] = pair.java
				return nil
			}
		}
		if msg == "" {
			return nil
		}
		return c.fail(msg, value)
	}
}

// fromStrings maps a native string back by the enum. invalid is the message
// for a value that is no string, unsupported for one the enum lacks; with
// unsupported empty, such a value stays as it is.
func fromStrings(e javaEnum, invalid, unsupported string) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		text, ok := stringProperty(value)
		if !ok {
			return c.fail(invalid, value)
		}
		for _, pair := range e {
			if pair.native == text {
				c.out[c.prop] = pair.java
				return nil
			}
		}
		if unsupported != "" {
			return c.fail(unsupported, value)
		}
		c.out[c.prop] = text
		return nil
	}
}

// fixed restores the row's Java properties to v.
func fixed(v any) func(ruleCtx) error {
	return func(c ruleCtx) error {
		c.out[c.prop] = v
		return nil
	}
}

// unlessSet restores the row's Java property to v if no row wrote it.
func unlessSet(v any) func(ruleCtx) error {
	return func(c ruleCtx) error {
		if _, exists := c.out[c.prop]; !exists {
			c.out[c.prop] = v
		}
		return nil
	}
}

// setNative sets the row's native property to v.
func setNative(v any) func(ruleCtx) error {
	return func(c ruleCtx) error {
		c.out[c.target] = v
		return nil
	}
}

// Families of Java blocks.
var (
	dripstones      = blocks("pointed_dripstone", "sulfur_spike")
	sculkSensors    = blocks("sculk_sensor", "calibrated_sculk_sensor")
	candles         = blocks("candle").or(blocksWithSuffix("_candle"))
	facingKeys      = []string{"minecraft:cardinal_direction", "facing_direction", "minecraft:facing_direction"}
	horizontalSides = []string{"north", "south", "east", "west"}
)

// Enums shared by both directions.
var (
	shelfSideChains   = javaEnum{{"unconnected", int32(0)}, {"right", int32(1)}, {"center", int32(2)}, {"left", int32(3)}}
	bambooLeaves      = javaEnum{{"none", "no_leaves"}, {"small", "small_leaves"}, {"large", "large_leaves"}}
	dripleafTilts     = javaEnum{{"partial", "partial_tilt"}, {"full", "full_tilt"}, {"none", "none"}, {"unstable", "unstable"}}
	grindstoneFaces   = javaEnum{{"floor", "standing"}, {"ceiling", "hanging"}, {"wall", "side"}, {"wall", "multiple"}}
	bellAttachments   = javaEnum{{"floor", "standing"}, {"ceiling", "hanging"}, {"single_wall", "side"}, {"double_wall", "multiple"}}
	doorFacings       = javaEnum{{"north", "east"}, {"south", "west"}, {"west", "north"}, {"east", "south"}}
	coralWallFacings  = javaEnum{{"west", int32(0)}, {"east", int32(1)}, {"north", int32(2)}, {"south", int32(3)}}
	sculkPhases       = javaEnum{{"inactive", int32(0)}, {"active", int32(1)}, {"cooldown", int32(2)}}
	trialSpawnerState = javaEnum{{"inactive", int32(0)}, {"waiting_for_players", int32(1)}, {"active", int32(2)},
		{"waiting_for_reward_ejection", int32(3)}, {"ejecting_reward", int32(4)}, {"cooldown", int32(5)}}
	turtleEggCounts = javaEnum{{int32(1), "one_egg"}, {int32(2), "two_egg"}, {int32(3), "three_egg"}, {int32(4), "four_egg"}}
	eggHatches      = javaEnum{{int32(0), "no_cracks"}, {int32(1), "cracked"}, {int32(2), "max_cracked"}}
	torchFacings    = javaEnum{{"south", "north"}, {"north", "south"}, {"west", "east"}, {"east", "west"}}
	// horizontalFacings are the native direction of most blocks,
	// trapdoorFacings that of trapdoors and stairFacings weirdo_direction.
	horizontalFacings = javaEnum{{"south", int32(0)}, {"west", int32(1)}, {"north", int32(2)}, {"east", int32(3)}}
	trapdoorFacings   = javaEnum{{"east", int32(0)}, {"west", int32(1)}, {"south", int32(2)}, {"north", int32(3)}}
	stairFacings      = trapdoorFacings
	railShapes        = javaEnum{{"north_south", int32(0)}, {"east_west", int32(1)}, {"ascending_east", int32(2)}, {"ascending_west", int32(3)},
		{"ascending_north", int32(4)}, {"ascending_south", int32(5)}, {"south_east", int32(6)}, {"south_west", int32(7)},
		{"north_west", int32(8)}, {"north_east", int32(9)}}
)

// propertyRules maps the Java properties to the native palette and back. The
// order of the rows that take the same Java property, or the same native
// property in reverse, is their precedence.
var propertyRules = []propertyRule{
	{java: blocks("nether_portal"), prop: "axis", native: "portal_axis", do: schemaValue,
		back: fromStrings(javaEnum{{"x", "x"}, {"z", "z"}}, "native portal axis {v} has no Java block-state representation", "native portal axis {v} has no Java block-state representation")},
	{java: blocks("sea_pickle"), prop: "pickles", native: "cluster_count", do: offset(-1, 1, 4, "invalid sea pickle count {v}"), back: plus(1)},
	// Jigsaw orientations are a front and a top; Bedrock keeps them as a
	// facing and a rotation.
	{java: blocks("jigsaw"), prop: "orientation", do: jigsawOrientation, backKeys: []string{"rotation"}, back: drop, restore: jigsawOrientationBack},
	{java: dripstones, prop: "vertical_direction", do: dripstoneDirection, backKeys: []string{"hanging"},
		back: flagTo("down", "up")},
	{java: dripstones, prop: "thickness", do: dripstoneThickness, backKeys: []string{"dripstone_thickness"}, back: dripstoneThicknessBack},
	{java: blocks("pink_petals", "wildflowers"), prop: "flower_amount", native: "growth", gate: gateAfter,
		do: offset(-1, 1, 4, "invalid flower amount {v} on {block}"), back: plus(1)},
	{java: blocks("farmland"), prop: "moisture", native: "moisturized_amount", gate: gateAfter,
		do: offset(0, 0, 7, "invalid farmland moisture {v}"), back: toInt},
	{java: blocks("leaf_litter"), prop: "segment_amount", native: "growth", do: offset(-1, 1, 4, "invalid leaf litter segment amount {v}"),
		back: leafLitterSegments},
	{java: blocksWhere(isSnowyGroundBlock), prop: "snowy", do: consume, restore: fixed(false)},
	// Bedrock tells an inverted daylight detector by its name.
	{java: blocks("daylight_detector"), prop: "inverted", do: consume, restore: func(c ruleCtx) error {
		c.out["inverted"] = c.native == "minecraft:daylight_detector_inverted"
		return nil
	}},
	{java: blocks("target"), prop: "power", do: consume, restore: fixed(int32(0))},
	// Bedrock tells a sticky piston head by its name.
	{java: blocks("piston_head"), prop: "type", do: checkOneOf("invalid piston head type {v}", "normal", "sticky"), restore: func(c ruleCtx) error {
		c.out["type"] = "normal"
		if c.native == "minecraft:sticky_piston_arm_collision" {
			c.out["type"] = "sticky"
		}
		return nil
	}},
	// The native moving-block palette has no properties. Java facing/type
	// remain in the retained canonical state, independently of its actor.
	{java: blocks("moving_piston"), prop: "facing", do: func(c ruleCtx, value any) (string, any, error) {
		_, err := convertJavaFacing("facing_direction", value, c.java)
		return "", consumed, err
	}},
	{java: blocks("moving_piston"), prop: "type", do: checkOneOf("invalid moving piston type {v}", "normal", "sticky")},
	// Repeater/skull activation is neighbor-derived in Java, while piston extension
	// is represented by a separate head block rather than a native base-state bit.
	{java: blocks("repeater"), prop: "locked", when: lacks("locked"), do: consume, restore: fixed(false)},
	{java: blocks("piston", "sticky_piston"), prop: "extended", when: lacks("extended_bit"), do: consume, restore: fixed(false)},
	{java: blocks("piston_head"), prop: "short", when: lacks("short"), do: consume, restore: fixed(false)},
	{java: blocksWhere(isSkull), prop: "powered", when: lacks("powered"), do: consume, restore: fixed(false)},
	// Native open_bit carries openness, not Java's independent power latch.
	{java: blocksWithSuffix("_door", "_trapdoor", "_fence_gate"), prop: "powered", do: checkBool("invalid powered property {v}")},
	// Native chorus connections are derived from neighbouring blocks.
	{java: blocks("chorus_plant"), props: []string{"up", "down", "north", "south", "east", "west"}, do: checkBool("invalid chorus connection {prop}={v}")},
	// NativeBlockForState carries pose in the block actor, not the palette.
	{java: blocksWithSuffix("copper_golem_statue"), prop: "copper_golem_pose", do: copperGolemPose},
	// The tree-growth latch is server-side; native propagule_stage carries age.
	{java: blocks("mangrove_propagule"), prop: "stage", do: checkInt(0, 1, "invalid propagule stage {v}")},
	{java: candles, prop: "candles", native: "candles", gate: gateAfter, do: offset(-1, 1, 4, "invalid candle count {v}"), back: plus(1)},
	// Power lives in the source actor sidecar, not the native palette.
	{java: sculkSensors, prop: "power", when: func(c ruleCtx) bool { return c.nativeProps["redstone_signal"] == nil },
		do: checkInt(0, 15, "invalid sculk sensor power {v}"), restore: fixed(int32(0))},
	{java: blocks("structure_block"), prop: "mode", native: "structure_block_type", gate: gateBefore, do: schemaValue,
		back: fromStrings(javaEnum{{"data", "data"}, {"save", "save"}, {"load", "load"}, {"corner", "corner"}},
			"invalid native structure block type {v}", "unsupported native structure block type {q}")},
	{java: blocks("hopper"), prop: "enabled", native: "toggle_bit", do: boolTo(true, "invalid hopper enabled property {v}"), back: notBool},
	{java: blocks("bell"), native: "toggle_bit", prop: "powered", back: toBool},
	// A huge mushroom's six faces are one native variant.
	{java: blocksWhere(isMushroomBlock), props: horizontalSides, native: "huge_mushroom_bits", do: mushroomBits, back: mushroomFacesBack},
	// The side keys already encoded the variant, which reads all six faces.
	{java: blocksWhere(isMushroomBlock), props: []string{"up", "down"}, do: consume},
	{java: blocks("chiseled_bookshelf"), prop: "slot_*", native: "books_stored", do: bookshelfSlots, back: bookshelfSlotsBack},
	// Bedrock's 0..3 shelf type represents Java's side-chain part, not inventory occupancy.
	{java: blocksWhere(isShelf), prop: "side_chain", native: "powered_shelf_type", gate: gateBefore,
		do:   toEnum(shelfSideChains, "invalid shelf side chain {v}", "unsupported shelf side chain {q}"),
		back: fromInts(shelfSideChains, "invalid native shelf type {v}")},
	{java: blocks("brewing_stand"), prop: "has_bottle_*", do: brewingStandSlot,
		backKeys: brewingStandSlots[:], back: brewingStandSlotBack},
	{java: blocks("comparator"), prop: "mode", native: "output_subtract_bit",
		do: flagOf("invalid comparator mode {v}", []string{"compare", "subtract"}, "subtract"), back: flagTo("subtract", "compare")},
	{java: blocks("repeater"), prop: "delay", native: "repeater_delay", do: offset(-1, 1, 4, "invalid repeater delay {v}"), back: repeaterDelayBack},
	// Bedrock tells cave vines with berries by their name.
	{java: blocksWithPrefix("cave_vines"), prop: "berries", do: consume, restore: func(c ruleCtx) error {
		c.out["berries"] = strings.HasSuffix(c.native, "with_berries")
		return nil
	}},
	{java: blocksWhere(isCampfire), prop: "lit", native: "extinguished", do: boolTo(true, "invalid campfire lit property {v}"), back: notBool},
	// Java derives this state from the hay block below; Bedrock recalculates it.
	{java: blocksWhere(isCampfire), prop: "signal_fire", do: consume, restore: fixed(false)},
	// Fire geometry is neighbor-derived in Java and Bedrock.
	{java: blocksWhere(isFire), props: []string{"north", "south", "east", "west", "up"}, do: consume},
	{java: blocks("vine"), props: horizontalSides, native: "vine_direction_bits", do: vineBits, back: vineBitsBack},
	// Java's top attachment has no Bedrock vine_direction_bits equivalent.
	{java: blocks("vine"), prop: "up", do: consume},
	{java: blocksWhere(isFire), prop: "age", native: "age", gate: gateBefore, do: schemaValue},
	// Bedrock tells lit furnaces, lamps, ores and torches by their name.
	{java: blocksContaining("furnace").or(blocks("smoker")).or(isRedstoneLitBlock), prop: "lit", do: consume},
	// Pressure plates are powered by a signal above zero.
	{java: blocksWithSuffix("_pressure_plate"), native: "redstone_signal", prop: "powered", back: func(c ruleCtx, value any) error {
		c.out["powered"] = intPropertyValue(value) > 0
		return nil
	}},
	{prop: "power", native: "redstone_signal", when: func(c ruleCtx) bool { return c.nativeProps["redstone_signal"] != nil },
		do: offset(0, 0, 15, "invalid redstone power {v}"), back: toInt},
	{prop: "layers", native: "height", gate: gateBefore, do: offset(-1, 1, int(^uint(0)>>1), "invalid snow layer count {v}"), back: plus(1)},
	{java: blocks("repeater", "comparator"), prop: "powered", do: consume},
	{prop: "waterlogged", do: waterlogged},
	{java: blocksWhere(isMultiFaceBlock), props: []string{"down", "up", "north", "south", "west", "east"}, native: "multi_face_direction_bits",
		do: multiFaceBits, back: multiFaceBitsBack},
	// Java leaf distance is derived from nearby logs. The Bedrock palette stores only
	// persistent_bit/update_bit; recomputing distance requires the neighborhood, so it
	// is intentionally reconstructed as the conservative distance 7.
	{java: blocksWhere(isLeafBlock), prop: "distance", do: consume, restore: unlessSet(int32(7))},
	// A double slab is a block of its own in Bedrock.
	{java: blocksWithSuffix("_slab"), prop: "type", native: "minecraft:vertical_half", gate: gateAfter, do: slabType, back: slabTypeBack},
	{java: blocks("bamboo"), prop: "leaves", native: "bamboo_leaf_size", gate: gateBefore,
		do:   toEnum(bambooLeaves, "invalid bamboo leaves {v}", "unsupported bamboo leaves {q}"),
		back: fromStrings(bambooLeaves, "invalid native bamboo leaf size {v}", "unsupported bamboo leaf size {q}")},
	{java: blocks("bamboo"), prop: "age", do: consume},
	{java: blocks("tall_seagrass"), onNative: blocks("seagrass"), prop: "half", do: consume},
	{java: blocksWhere(isRail), prop: "shape", native: "rail_direction",
		do: toEnum(railShapes, "invalid rail shape {v}", "unsupported rail shape {q}"), back: fromInts(railShapes, "")},
	{java: blocksWithSuffix("_stairs"), prop: "shape", native: "minecraft:corner", gate: gateBefore, do: stairShape, back: stairShapeBack},
	{java: blocksWhere(isGrindstone), prop: "face", native: "attachment", gate: gateBefore,
		do:   toEnum(grindstoneFaces, "invalid grindstone face {v}", "unsupported grindstone face {q}"),
		back: fromStrings(grindstoneFaces, "unsupported native grindstone attachment {v}", "unsupported native grindstone attachment {v}")},
	{java: blocks("bee_nest", "beehive"), prop: "facing", native: "direction", gate: gateBefore, do: horizontalFacing,
		back: fromInts(horizontalFacings, "unsupported native bee direction {v}")},
	{java: blocksWhere(isBell), prop: "attachment", native: "attachment", gate: gateBefore,
		do:   toEnum(bellAttachments, "invalid bell attachment {v}", "unsupported bell attachment {q}"),
		back: fromStrings(bellAttachments, "unsupported native bell attachment {v}", "unsupported native bell attachment {v}")},
	{java: blocksWhere(isButton), props: []string{"face", "facing"}, native: "facing_direction", gate: gateBefore, do: buttonFacingRule,
		restore: buttonFaceBack},
	{java: blocksWhere(isLever), props: []string{"face", "facing"}, native: "lever_direction", gate: gateBefore, do: leverDirectionRule,
		back: leverDirectionBack},
	{java: blocks("big_dripleaf_stem"), native: "big_dripleaf_tilt", back: drop},
	{java: blocks("big_dripleaf"), prop: "tilt", native: "big_dripleaf_tilt", gate: gateBefore,
		do:   toEnum(dripleafTilts, "invalid big dripleaf tilt {v}", "unsupported big dripleaf tilt {q}"),
		back: fromStrings(dripleafTilts, "invalid native big dripleaf tilt {v}", "unsupported native big dripleaf tilt {q}")},
	{java: blocksWhere(isTrapdoor), prop: "facing", native: "direction", gate: gateBefore,
		do: toEnum(trapdoorFacings, "invalid trapdoor facing {v}", "unsupported trapdoor facing {q}"), back: fromInts(trapdoorFacings, "")},
	{onNative: blocksWhere(isSign).or(isRail), native: "direction", back: drop},
	{java: blocksWhere(isBell).or(isGrindstone), prop: "facing", native: "direction", gate: gateBefore, do: horizontalFacing},
	{native: "direction", prop: "facing", back: fromInts(horizontalFacings, "")},
	// Unattached stems point up; the attached ones are their own Java blocks.
	{java: blocks("melon_stem", "pumpkin_stem"), native: "facing_direction", set: stemFacing, backKeys: facingKeys, back: drop},
	{java: blocks("jigsaw"), backKeys: facingKeys, back: facingBack(nil)},
	// Bedrock's piston facing_direction numbers the horizontal directions the
	// other way round.
	{java: blocksWhere(isPistonFamily), backKeys: facingKeys, back: facingBack(func(c ruleCtx, facing string) {
		c.out["facing"] = pistonNativeHorizontal(facing)
	})},
	{java: blocksWhere(isSkull), prop: "facing", native: "facing_direction", gate: gateBefore, do: facingTo, backKeys: facingKeys,
		back: facingBack(func(c ruleCtx, facing string) {
			if facing == "up" {
				c.out["rotation"] = int32(0)
			} else {
				c.out["facing"] = facing
			}
		})},
	{java: blocksWithSuffix("_door"), prop: "facing", native: "minecraft:cardinal_direction", gate: gateBefore,
		do: toEnum(doorFacings, "invalid door facing {v}", "unsupported door facing {q}"), backKeys: facingKeys,
		back: facingBack(func(c ruleCtx, facing string) {
			for _, pair := range doorFacings {
				if pair.native == facing {
					facing = pair.java.(string)
					break
				}
			}
			c.out["facing"] = facing
		})},
	{java: blocksWithSuffix("_coral_wall_fan"), prop: "facing", native: "coral_direction", gate: gateBefore,
		do:   toEnum(coralWallFacings, "invalid coral wall fan facing {v}", "unsupported coral wall fan facing {q}"),
		back: fromInts(coralWallFacings, "unsupported coral wall fan direction {v}")},
	{prop: "facing", natives: facingPropertyCandidates[:], do: facingTo},
	{backKeys: facingKeys, back: facingBack(func(c ruleCtx, facing string) { c.out["facing"] = facing })},
	{native: "weirdo_direction", prop: "facing", back: fromInts(stairFacings, "")},
	{native: "minecraft:block_face", prop: "facing", back: toSame},
	{prop: "rotation", natives: rotationPropertyCandidates[:], do: schemaValue},
	{onNative: blocksWhere(isStandingSign).or(isBanner).or(isSkull), native: "ground_sign_direction", prop: "rotation", back: toInt},
	{native: "ground_sign_direction", back: drop},
	// Bedrock's skull palette has only six facing states; the Java 16-step
	// floor rotation is retained by its block-entity payload, not block state.
	{java: blocksWhere(isSkull), prop: "rotation", native: "facing_direction", gate: gateBefore, do: func(ruleCtx, any) (string, any, error) {
		return "", int32(1), nil
	}},
	{java: blocks("pale_moss_carpet"), prop: "bottom", native: "upper_block_bit", gate: gateBefore,
		do: boolTo(true, "invalid pale moss bottom property {v}"), back: notBool},
	{prop: "half", native: "minecraft:vertical_half", gate: gateBefore, do: schemaValue, back: toSame},
	{prop: "half", native: "upside_down_bit", gate: gateBefore,
		do: flagOf("invalid half {v} on {block}", []string{"top", "bottom"}, "top"), back: flagToIf("top", "bottom")},
	{prop: "half", native: "upper_block_bit", gate: gateBefore,
		do: flagOf("invalid half {v} on {block}", []string{"upper", "lower", "top", "bottom"}, "upper", "top"), back: flagToIf("upper", "lower")},
	{java: blocks("bed", "straw_bed"), prop: "part", native: "head_piece_bit", gate: gateBefore,
		do: flagOf("invalid bed part {v}", []string{"head", "foot"}, "head"), back: flagTo("head", "foot")},
	{prop: "in_wall", native: "in_wall_bit", gate: gateBefore, do: schemaValue, back: toBool},
	// Bedrock stores note-block sound, pitch and power in its block actor, not palette state.
	{java: blocks("note_block"), props: []string{"instrument", "note", "powered"}, do: consume},
	// Record presence is represented by the jukebox block actor.
	{java: blocks("jukebox"), prop: "has_record", do: consume},
	// Book contents and presence are represented by the lectern block actor.
	{java: blocks("lectern"), prop: "has_book", do: consume},
	// Pot sherds and cracking are carried by the decorated-pot block actor.
	{java: blocks("decorated_pot"), prop: "cracked", do: consume},
	{prop: "hinge", native: "door_hinge_bit", gate: gateBefore,
		do: flagOf("invalid door hinge {v}", []string{"left", "right"}, "right"), back: flagToIf("right", "left")},
	{java: blocksWhere(isLeafBlock), prop: "persistent", native: "persistent_bit", gate: gateBefore,
		do: boolTo(false, "invalid persistent leaf property {v}"), back: toBoolIf, restore: unlessSet(false)},
	{java: blocksWhere(isWall), props: horizontalSides, nativePrefix: "wall_connection_type_", gate: gateBefore, do: wallSide,
		back: func(c ruleCtx, value any) error {
			if text, ok := stringProperty(value); ok {
				if text == "short" {
					text = "low"
				}
				c.out[c.prop] = text
			}
			return nil
		}},
	{nativePrefix: "wall_connection_type_", back: func(c ruleCtx, value any) error {
		if text, ok := stringProperty(value); ok {
			c.out[c.prop] = text != "none"
		}
		return nil
	}},
	{java: blocks("pale_moss_carpet"), props: horizontalSides, nativePrefix: "pale_moss_carpet_side_", gate: gateBefore,
		do: paleMossSide, back: paleMossSideBack},
	// Wire shape is recalculated from adjacent blocks; Bedrock stores only signal power.
	{java: blocksWhere(isRedstoneWire), props: horizontalSides, do: consume, restore: func(c ruleCtx) error {
		for _, side := range horizontalSides {
			c.out[side] = "none"
		}
		return nil
	}},
	{java: blocksWhere(isConnectionBlock), props: horizontalSides, nativePrefix: "minecraft:connection_", gate: gateBefore,
		do: boolTo(false, "invalid connection {prop}={v}"), back: toBool},
	{java: blocksWhere(isWall), prop: "up", native: "wall_post_bit", gate: gateBefore, do: boolTo(false, "invalid wall post property {v}")},
	{native: "wall_post_bit", prop: "up", back: toBool},
	{java: blocksWhere(isLever), native: "open_bit", prop: "powered", back: toBool},
	{prop: "powered", natives: poweredPropertyCandidates[:], do: boolTo(false, "invalid powered property {v}")},
	{backKeys: []string{"button_pressed_bit", "powered_bit", "output_lit_bit"}, prop: "powered", back: toBool},
	{native: "rail_data_bit", prop: "powered", back: toBoolIf},
	{prop: "powered", native: "redstone_signal", gate: gateBefore, do: poweredSignal},
	{java: sculkSensors, prop: "sculk_sensor_phase", native: "sculk_sensor_phase", gate: gateBefore,
		do:   toEnum(sculkPhases, "invalid sculk sensor phase {v}", "unsupported sculk sensor phase {q}"),
		back: fromInts(sculkPhases, "unsupported native sculk sensor phase {v}")},
	{java: blocks("sculk_shrieker"), prop: "shrieking", native: "active", gate: gateBefore, do: boolTo(false, "invalid shrieking property {v}"), back: toBool},
	{java: blocks("turtle_egg"), prop: "eggs", native: "turtle_egg_count", do: turtleEggs,
		back: fromStrings(turtleEggCounts, "invalid native turtle egg count {v}", "unsupported native turtle egg count {q}")},
	{java: blocks("turtle_egg", "sniffer_egg"), prop: "hatch", native: "cracked_state", do: eggHatch,
		back: fromStrings(eggHatches, "invalid native turtle egg crack state {v}", "unsupported native turtle egg crack state {q}")},
	{java: blocks("scaffolding"), prop: "distance", native: "stability", gate: gateBefore, do: schemaValue, back: toInt},
	{java: blocks("scaffolding"), prop: "bottom", do: consume, restore: fixed(false)},
	// Bedrock distinguishes native fan orientation; Java coral fans do not.
	{java: blocksWithSuffix("_coral_fan"), prop: "coral_fan_direction", native: "coral_fan_direction", do: consume, back: drop},
	{java: blocks("dried_ghast"), prop: "hydration", native: "rehydration_level", gate: gateBefore, do: schemaValue, back: toInt},
	// Bedrock stores this Java enum as an integer palette property.
	{java: blocks("trial_spawner"), prop: "trial_spawner_state", native: "trial_spawner_state",
		do:   toEnum(trialSpawnerState, "invalid trial spawner state {v}", "unsupported trial spawner state {q}"),
		back: fromInts(trialSpawnerState, "unsupported native trial spawner state {v}")},
	// Attached stems are their own Java blocks, fully grown.
	{java: blocks("attached_melon_stem", "attached_pumpkin_stem"), native: "growth", set: func(c ruleCtx) error {
		c.out["growth"] = valueForSchema(int32(7), c.nativeProps["growth"])
		return nil
	}, back: drop},
	{native: "growth", prop: "age", back: toInt},
	{prop: "age", natives: agePropertyCandidates[:], do: schemaValue},
	{backKeys: []string{"twisting_vines_age", "weeping_vines_age", "propagule_stage"}, prop: "age", back: toInt},
	// The age of cave vines with berries is their name.
	{onNative: blocksWithPrefix("cave_vines"), native: "growing_plant_age", set: func(c ruleCtx) error {
		if _, exists := c.out["growing_plant_age"]; !exists {
			c.out["growing_plant_age"] = int32(0)
		}
		return nil
	}, back: func(c ruleCtx, value any) error {
		if c.native != "minecraft:cave_vines_body_with_berries" {
			c.out["age"] = intPropertyValue(value)
		}
		return nil
	}},
	{native: "growing_plant_age", back: func(c ruleCtx, _ any) error {
		return fmt.Errorf("java state codec: unsupported native growing_plant_age on %s", c.native)
	}},
	{java: blocks("bamboo"), prop: "stage", native: "age_bit", gate: gateBefore,
		do: flagOfInt("invalid bamboo stage {v}"), back: boolIntIf},
	{java: blocksContaining("sapling"), native: "age_bit", prop: "stage", back: boolIntIf},
	{prop: "stage", native: "age_bit", gate: gateBefore, do: flagOfInt("invalid plant stage {v}")},
	{native: "age_bit", prop: "age", back: boolIntIf},
	{java: blocks("light"), prop: "level", do: consume},
	{java: blocks("water_cauldron", "powder_snow_cauldron"), prop: "level", native: "fill_level", do: cauldronLevel, back: func(c ruleCtx, value any) error {
		c.out["level"] = intPropertyValue(value)/2 + intPropertyValue(value)%2
		return nil
	}},
	{native: "fill_level", back: drop},
	{java: blocks("composter"), prop: "level", native: "composter_fill_level", gate: gateBefore, do: schemaValue, back: toInt},
	{prop: "level", natives: levelPropertyCandidates[:], do: schemaValue},
	{java: blocks("water", "lava"), prop: "level", native: "liquid_depth", set: func(c ruleCtx) error {
		if level, ok := intProperty(c.javaProps["level"]); ok {
			c.out["liquid_depth"] = int32(level)
		}
		return nil
	}, back: toInt},
	// Chest left/right/single is neighbor-derived; Bedrock infers the pair from adjacent chests.
	{java: blocksWhere(isChest), prop: "type", do: func(_ ruleCtx, value any) (string, any, error) {
		if _, ok := stringProperty(value); ok {
			return "", consumed, nil
		}
		return "", passed, nil
	}},
	// Pane/fence/cable connections are Java neighbor-derived and represented by their four side properties.
	{java: blocksWhere(isConnectionBlock), prop: "shape", do: consume},
	{java: blocksWhere(isTorchLike), prop: "face", native: "torch_facing_direction", gate: gateBefore, do: func(c ruleCtx, _ any) (string, any, error) {
		text, err := nativeTorchFacing(c.javaProps)
		return "", text, err
	}, back: func(c ruleCtx, value any) error {
		text, ok := stringProperty(value)
		if !ok || text == "top" {
			return nil
		}
		for _, pair := range torchFacings {
			if pair.native == text {
				c.out["facing"] = pair.java
				return nil
			}
		}
		return fmt.Errorf("java state codec: unsupported native torch facing %q", text)
	}},
	// Fixed native properties.
	{java: blocks("torch", "soul_torch", "redstone_torch", "copper_torch"), native: "torch_facing_direction", set: setNative("top")},
	{java: blocksWithSuffix("_hanging_sign"), set: hangingSign},
	// Bedrock sea-pickle state is not the Java waterlogged property; liquid uses the secondary layer.
	{java: blocks("sea_pickle"), native: "dead_bit", set: func(c ruleCtx) error {
		waterlogged, _ := boolProperty(c.javaProps["waterlogged"])
		c.out["dead_bit"] = nativeBool(!waterlogged, c.nativeProps["dead_bit"])
		return nil
	}, back: drop},
	{native: "dead_bit", back: func(c ruleCtx, _ any) error {
		return fmt.Errorf("java state codec: unsupported dead_bit on %s", c.native)
	}},
	// The native liquid variant selects the Java cauldron block name.
	{java: blocks("cauldron", "water_cauldron", "powder_snow_cauldron", "lava_cauldron"), onNative: blocks("cauldron"),
		native: "cauldron_liquid", set: cauldronLiquid, back: drop},
	{java: blocks("comparator"), native: "output_lit_bit", set: func(c ruleCtx) error {
		if _, exists := c.nativeProps["output_lit_bit"]; exists {
			powered, ok := boolProperty(c.javaProps["powered"])
			if !ok {
				return fmt.Errorf("java state codec: comparator has invalid powered property %v", c.javaProps["powered"])
			}
			c.out["output_lit_bit"] = nativeBool(powered, c.nativeProps["output_lit_bit"])
		}
		return nil
	}},
	{java: blocksWithSuffix("_slab"), set: func(c ruleCtx) error {
		if slabType, ok := stringProperty(c.javaProps["type"]); ok && slabType == "double" {
			// A double slab is represented by its distinct native block name.
			delete(c.out, "type")
		}
		return nil
	}},
	// Native head/stem variants are converted to Java block names after property decoding.
	{onNative: blocks("big_dripleaf"), native: "big_dripleaf_head", set: func(c ruleCtx) error {
		if head, ok := c.nativeProps["big_dripleaf_head"]; ok {
			c.out["big_dripleaf_head"] = nativeBool(c.java == "minecraft:big_dripleaf", head)
		}
		return nil
	}, back: drop},
	// Old-style Bedrock flower aliases encode species in this palette property.
	{onNative: blocks("red_flower"), set: species(redFlowerNativeTypes, "flower alias", "red flower", "red_flower_type", "flower_type", "type")},
	{onNative: blocks("double_plant"), set: species(doublePlantNativeTypes, "double-plant alias", "double plant", "double_plant_type", "tall_grass_type", "type")},
	{backKeys: []string{"red_flower_type", "flower_type", "double_plant_type", "tall_grass_type"}, back: drop},
	// Converted to Java seagrass/tall_seagrass by the block-family adapter.
	{java: blocks("tall_seagrass"), onNative: blocks("seagrass"), native: "sea_grass_type", set: tallSeagrass},
	{native: "sea_grass_type", back: drop},
	{java: blocks("kelp_plant"), onNative: blocks("kelp"), native: "kelp_age", set: setNative(int32(0))},
	{native: "kelp_age", prop: "age", back: toInt},
	// Java names for native properties.
	{prop: "open", native: "open_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "extended", native: "extended_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "triggered", native: "triggered_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "locked", native: "open_bit", gate: gateBefore, do: schemaValue},
	{prop: "bites", native: "bite_counter", gate: gateBefore, do: schemaValue, back: toInt},
	{prop: "berries", native: "berry", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "attached", native: "attached_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "mode", native: "output_subtract_bit", gate: gateBefore, do: schemaValue},
	{prop: "conditional", native: "conditional_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "enabled", native: "enabled_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "occupied", native: "occupied_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "part", native: "head_piece_bit", gate: gateBefore, do: schemaValue},
	{prop: "disarmed", native: "disarmed_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "axis", native: "pillar_axis", gate: gateBefore, do: schemaValue, back: toSame},
	{prop: "eye", native: "end_portal_eye_bit", gate: gateBefore, do: schemaValue, back: toBool},
	{prop: "charges", native: "respawn_anchor_charge", gate: gateBefore, do: schemaValue, back: toInt},
	{java: blocks("tnt"), prop: "unstable", native: "explode_bit", back: toBool},
	{native: "explode_bit", back: drop},
	{prop: "unstable", native: "explode_bit", gate: gateBefore, do: schemaValue},
	{prop: "dusted", native: "brushed_progress", gate: gateBefore, do: schemaValue, back: toInt},
	{prop: "drag", native: "drag_down", gate: gateBefore, do: schemaValue, back: toBool},
	{java: blocks("suspicious_sand", "suspicious_gravel"), native: "hanging", back: drop},
	{native: "hanging", prop: "hanging", back: toBool},
	{native: "lit", prop: "lit", back: toBool},
	{backKeys: []string{"can_summon", "natural", "ominous"}, back: func(c ruleCtx, value any) error {
		c.out[c.target] = nativeBoolValue(value)
		return nil
	}},
	// Native metadata with no Java counterpart: Bedrock's update scheduling and
	// flower-pot refresh flags, snow cover, transient growth and attachment,
	// support checks, bloom animation, stalk thickness and combustion.
	{backKeys: []string{"update_bit", "covered_bit", "suspended_bit", "stability_check", "bloom", "bamboo_stalk_thickness", "infiniburn_bit"}, back: drop},
}

// lacks reports whether the native block lacks a property.
func lacks(key string) func(ruleCtx) bool {
	return func(c ruleCtx) bool {
		_, ok := c.nativeProps[key]
		return !ok
	}
}

// blocksWhere selects blocks by a predicate.
func blocksWhere(f func(string) bool) javaSel { return f }

func jigsawOrientation(c ruleCtx, value any) (string, any, error) {
	orientation, ok := stringProperty(value)
	if !ok {
		return "", nil, c.fail("invalid jigsaw orientation {v}", value)
	}
	front, rotation, ok := jigsawOrientationParts(orientation)
	if !ok {
		return "", nil, fmt.Errorf("java state codec: unsupported jigsaw orientation %q", orientation)
	}
	if schema, ok := c.nativeProps["orientation"]; ok {
		return "orientation", valueForSchema(value, schema), nil
	}
	if _, ok := c.nativeProps["facing_direction"]; !ok {
		return "", nil, fmt.Errorf("java state codec: native jigsaw has no facing_direction property")
	}
	if _, ok := c.nativeProps["rotation"]; !ok {
		return "", nil, fmt.Errorf("java state codec: native jigsaw has no rotation property")
	}
	mapped, err := convertJavaFacing("facing_direction", front, c.java)
	if err != nil {
		return "", nil, err
	}
	c.out["rotation"] = valueForSchema(rotation, c.nativeProps["rotation"])
	return "facing_direction", mapped, nil
}

func jigsawOrientationBack(c ruleCtx) error {
	orientation, err := nativeJigsawOrientation(c.nativeProps)
	if err != nil {
		return err
	}
	c.out["orientation"] = orientation
	return nil
}

func dripstoneDirection(c ruleCtx, value any) (string, any, error) {
	direction, ok := stringProperty(value)
	if !ok {
		return "", nil, c.fail("invalid dripstone vertical_direction {v}", value)
	}
	if direction != "up" && direction != "down" {
		return "", nil, fmt.Errorf("java state codec: unsupported dripstone vertical_direction %q", direction)
	}
	if schemaValue, ok := c.nativeProps["hanging"]; ok {
		return "hanging", nativeBool(direction == "down", schemaValue), nil
	}
	if schemaValue, ok := c.nativeProps["vertical_direction"]; ok {
		return "vertical_direction", valueForSchema(value, schemaValue), nil
	}
	return "", nil, c.fail("native {block} has neither hanging nor vertical_direction", value)
}

func dripstoneThickness(c ruleCtx, value any) (string, any, error) {
	thickness, ok := stringProperty(value)
	if !ok {
		return "", nil, c.fail("invalid dripstone thickness {v}", value)
	}
	if _, ok := c.nativeProps["dripstone_thickness"]; ok {
		if thickness == "tip_merge" {
			thickness = "merge"
		}
		return "dripstone_thickness", thickness, nil
	}
	if schemaValue, ok := c.nativeProps["thickness"]; ok {
		return "thickness", valueForSchema(value, schemaValue), nil
	}
	return "", nil, c.fail("native {block} has no dripstone thickness property", value)
}

func dripstoneThicknessBack(c ruleCtx, value any) error {
	thickness, ok := stringProperty(value)
	if !ok {
		return c.fail("invalid native dripstone thickness {v}", value)
	}
	if thickness == "merge" {
		thickness = "tip_merge"
	}
	c.out["thickness"] = thickness
	return nil
}

func leafLitterSegments(c ruleCtx, value any) error {
	growth := intPropertyValue(value)
	if growth < 0 || growth > 3 {
		return fmt.Errorf("java state codec: native leaf litter growth %d exceeds Java segment count", growth)
	}
	c.out["segment_amount"] = growth + 1
	return nil
}

func copperGolemPose(c ruleCtx, value any) (string, any, error) {
	switch value {
	case "standing", "sitting", "running", "star":
		return "", consumed, nil
	}
	return "", nil, c.fail("invalid copper golem pose {v}", value)
}

func mushroomBits(c ruleCtx, _ any) (string, any, error) {
	variant, err := mushroomVariant(c.java, c.javaProps)
	return "", int32(variant), err
}

func mushroomFacesBack(c ruleCtx, value any) error {
	variant := intPropertyValue(value)
	if variant < 0 || variant > 15 {
		return c.fail("invalid native mushroom variant {v}", value)
	}
	for side, outside := range mushroomVariantFaces(int(variant)) {
		c.out[side] = outside
	}
	return nil
}

func bookshelfSlots(c ruleCtx, _ any) (string, any, error) {
	index, ok := occupiedSlotIndex(c.prop)
	if !ok || index >= 6 {
		return "", nil, fmt.Errorf("java state codec: invalid chiseled bookshelf slot property %q", c.prop)
	}
	mask, err := occupiedSlotMask(c.javaProps, 6)
	return "", int32(mask), err
}

func bookshelfSlotsBack(c ruleCtx, value any) error {
	mask, ok := intProperty(value)
	if !ok || mask < 0 || mask > 63 {
		return c.fail("invalid native bookshelf mask {v}", value)
	}
	for slot := range 6 {
		c.out["slot_"+strconv.Itoa(slot)+"_occupied"] = mask&(1<<slot) != 0
	}
	return nil
}

// brewingStandSlots are the native bottle slots, by Java index.
var brewingStandSlots = [...]string{"brewing_stand_slot_a_bit", "brewing_stand_slot_b_bit", "brewing_stand_slot_c_bit"}

func brewingStandSlot(c ruleCtx, value any) (string, any, error) {
	index, err := strconv.Atoi(strings.TrimPrefix(c.prop, "has_bottle_"))
	if err != nil || index < 0 || index > 2 {
		return "", nil, fmt.Errorf("java state codec: invalid brewing stand slot property %q", c.prop)
	}
	key := brewingStandSlots[index]
	return key, valueForSchema(value, c.nativeProps[key]), nil
}

func brewingStandSlotBack(c ruleCtx, value any) error {
	c.out["has_bottle_"+strconv.Itoa(slices.Index(brewingStandSlots[:], c.target))] = nativeBoolValue(value)
	return nil
}

func repeaterDelayBack(c ruleCtx, value any) error {
	delay := intPropertyValue(value)
	if delay < 0 || delay > 3 {
		return c.fail("invalid native repeater delay {v}", value)
	}
	c.out["delay"] = delay + 1
	return nil
}

func vineBits(c ruleCtx, _ any) (string, any, error) {
	bits, err := vineDirectionBits(c.javaProps)
	return "", int32(bits), err
}

func vineBitsBack(c ruleCtx, value any) error {
	variant := intPropertyValue(value)
	if variant < 0 || variant > 15 {
		return c.fail("invalid native vine direction bits {v}", value)
	}
	for bit, side := range [...]string{"south", "west", "north", "east"} {
		c.out[side] = variant&(1<<bit) != 0
	}
	c.out["up"] = false
	return nil
}

func waterlogged(c ruleCtx, value any) (string, any, error) {
	waterlogged, ok := boolProperty(value)
	if !ok {
		return "", nil, c.fail("non-boolean waterlogged property on {block}", value)
	}
	if waterlogged && !hasJavaWaterloggedProperty(c.java) {
		return "", nil, c.fail("{block} has no Java waterlogged state", value)
	}
	return "", consumed, nil
}

func multiFaceBits(c ruleCtx, _ any) (string, any, error) {
	bits, err := javaMultiFaceBits(c.javaProps)
	return "", int32(bits), err
}

func multiFaceBitsBack(c ruleCtx, value any) error {
	bits, ok := intProperty(value)
	if !ok || bits < 0 || bits > 63 {
		return c.fail("invalid native multiface mask {v}", value)
	}
	for bit, side := range [...]string{"down", "up", "south", "west", "north", "east"} {
		c.out[side] = bits&(1<<bit) != 0
	}
	return nil
}

func slabType(c ruleCtx, value any) (string, any, error) {
	text, ok := stringProperty(value)
	if !ok {
		return "", nil, c.fail("invalid slab type {v}", value)
	}
	if text == "double" {
		return "", consumed, nil
	}
	return "", text, nil
}

func slabTypeBack(c ruleCtx, value any) error {
	if isNativeDoubleSlab(c.native) {
		c.out["type"] = "double"
	} else {
		c.out["type"] = value
	}
	return nil
}

func stairShape(c ruleCtx, value any) (string, any, error) {
	shape, ok := stringProperty(value)
	if !ok {
		return "", nil, c.fail("invalid stair shape {v}", value)
	}
	if shape == "straight" {
		shape = "none"
	}
	return "", shape, nil
}

func stairShapeBack(c ruleCtx, value any) error {
	if text, ok := stringProperty(value); ok {
		if text == "none" {
			text = "straight"
		}
		c.out["shape"] = text
	}
	return nil
}

// horizontalFacing maps a horizontal Java facing to a native direction.
var horizontalFacing = toEnum(horizontalFacings, "invalid horizontal facing {v}", "unsupported horizontal facing {q}")

func buttonFacingRule(c ruleCtx, _ any) (string, any, error) {
	direction, err := buttonFacing(c.javaProps)
	return "", direction, err
}

func buttonFaceBack(c ruleCtx) error {
	switch c.out["facing"] {
	case "up":
		c.out["face"], c.out["facing"] = "floor", "north"
	case "down":
		c.out["face"], c.out["facing"] = "ceiling", "north"
	case "north", "south", "east", "west":
		c.out["face"] = "wall"
	}
	return nil
}

func leverDirectionRule(c ruleCtx, _ any) (string, any, error) {
	direction, err := leverDirection(c.javaProps)
	return "", direction, err
}

func leverDirectionBack(c ruleCtx, value any) error {
	face, facing, ok := nativeLeverDirection(value)
	if !ok {
		return c.fail("unsupported native lever direction {v}", value)
	}
	c.out["face"], c.out["facing"] = face, facing
	return nil
}

func facingTo(c ruleCtx, value any) (string, any, error) {
	mapped, err := convertJavaFacing(c.target, value, c.java)
	return "", mapped, err
}

// facingBack reads a native facing, then lets write place it.
func facingBack(write func(c ruleCtx, facing string)) func(ruleCtx, any) error {
	return func(c ruleCtx, value any) error {
		facing, ok := nativeFacing(c.target, value)
		if !ok {
			return fmt.Errorf("java state codec: cannot reverse native facing %s=%v on %s", c.target, value, c.native)
		}
		if write != nil {
			write(c, facing)
		}
		return nil
	}
}

func stemFacing(c ruleCtx) error {
	if _, exists := c.out["facing_direction"]; !exists {
		facing, err := convertJavaFacing("facing_direction", "up", c.java)
		if err != nil {
			return err
		}
		c.out["facing_direction"] = facing
	}
	return nil
}

func wallSide(_ ruleCtx, value any) (string, any, error) {
	side, err := javaWallSide(value)
	return "", side, err
}

func paleMossSide(c ruleCtx, value any) (string, any, error) {
	side, ok := stringProperty(value)
	if !ok || (side != "none" && side != "low" && side != "tall") {
		return "", nil, c.fail("invalid pale moss side {prop}={v}", value)
	}
	if side == "low" {
		side = "short"
	}
	return "", side, nil
}

func paleMossSideBack(c ruleCtx, value any) error {
	side, ok := stringProperty(value)
	if !ok {
		return fmt.Errorf("java state codec: invalid pale moss carpet side %s=%v", c.target, value)
	}
	if side == "short" {
		side = "low"
	}
	c.out[c.prop] = side
	return nil
}

func poweredSignal(c ruleCtx, value any) (string, any, error) {
	b, ok := boolProperty(value)
	if !ok {
		return "", nil, c.fail("invalid powered property {v}", value)
	}
	if b {
		return "", int32(15), nil
	}
	return "", int32(0), nil
}

func turtleEggs(c ruleCtx, value any) (string, any, error) {
	n, ok := intProperty(value)
	if !ok {
		return "", nil, c.fail("invalid {block} property {prop}={v}", value)
	}
	if n < 1 || n > 4 {
		return "", nil, fmt.Errorf("java state codec: unsupported turtle egg count %d", n)
	}
	return "", turtleEggCounts[n-1].native, nil
}

func eggHatch(c ruleCtx, value any) (string, any, error) {
	n, ok := intProperty(value)
	if !ok {
		return "", nil, c.fail("invalid {block} property {prop}={v}", value)
	}
	if n < 0 || n > 2 {
		return "", nil, fmt.Errorf("java state codec: unsupported %s hatch %d", c.java, n)
	}
	return "", eggHatches[n].native, nil
}

// flagOfInt maps a 0 or 1 to the native boolean type.
func flagOfInt(msg string) func(ruleCtx, any) (string, any, error) {
	return func(c ruleCtx, value any) (string, any, error) {
		n, ok := intProperty(value)
		if !ok || (n != 0 && n != 1) {
			return "", nil, c.fail(msg, value)
		}
		return "", nativeBool(n == 1, c.nativeProps[c.target]), nil
	}
}

// boolIntIf writes a native boolean as 0 or 1, if it is one.
func boolIntIf(c ruleCtx, value any) error {
	if b, ok := boolProperty(value); ok {
		c.out[c.prop] = int32(javaStateBoolInt(b))
	}
	return nil
}

func cauldronLevel(c ruleCtx, value any) (string, any, error) {
	level, ok := intProperty(value)
	if !ok || level < 1 || level > 3 {
		return "", nil, c.fail("invalid cauldron level {v}", value)
	}
	return "", int32(level*2 - 1), nil
}

func cauldronLiquid(c ruleCtx) error {
	switch c.java {
	case "minecraft:water_cauldron":
		c.out["cauldron_liquid"] = "water"
	case "minecraft:powder_snow_cauldron":
		c.out["cauldron_liquid"] = "powder_snow"
	case "minecraft:lava_cauldron":
		c.out["cauldron_liquid"] = "lava"
		c.out["fill_level"] = int32(5)
	}
	return nil
}

func hangingSign(c ruleCtx) error {
	wall := strings.HasSuffix(c.java, "_wall_hanging_sign")
	c.out["hanging"] = nativeBool(!wall, c.nativeProps["hanging"])
	if wall {
		c.out["attached_bit"] = nativeBool(false, c.nativeProps["attached_bit"])
		c.out["ground_sign_direction"] = int32(0)
		return nil
	}
	rotation, ok := intProperty(c.javaProps["rotation"])
	if !ok || rotation < 0 || rotation > 15 {
		return fmt.Errorf("java state codec: invalid hanging sign rotation %v", c.javaProps["rotation"])
	}
	attached, _ := boolProperty(c.javaProps["attached"])
	c.out["facing_direction"] = int32(0)
	c.out["ground_sign_direction"] = int32(rotation)
	if !attached && rotation%4 == 0 {
		c.out["facing_direction"] = [...]int32{3, 4, 2, 5}[rotation/4]
		c.out["ground_sign_direction"] = int32(0)
	}
	return nil
}

// species writes the species of a native block that stands for several Java
// blocks into the first of keys the native block has.
func species(types map[string]string, alias, family string, keys ...string) func(ruleCtx) error {
	return func(c ruleCtx) error {
		kind, ok := types[c.java]
		if !ok {
			return fmt.Errorf("java state codec: unsupported %s %s", alias, c.java)
		}
		key := firstNativeProperty(c.nativeProps, keys)
		if key == "" {
			return fmt.Errorf("java state codec: %s native palette has no species property", family)
		}
		c.out[key] = kind
		return nil
	}
}

func tallSeagrass(c ruleCtx) error {
	half, ok := stringProperty(c.javaProps["half"])
	if !ok || (half != "upper" && half != "lower") {
		return fmt.Errorf("java state codec: tall seagrass needs upper/lower half, got %v", c.javaProps["half"])
	}
	c.out["sea_grass_type"] = "double_bot"
	if half == "upper" {
		c.out["sea_grass_type"] = "double_top"
	}
	return nil
}

// blockRules names native blocks for Java states and Java blocks for native
// states. Forward, the name of each row that selects the Java block applies
// in order, after the alias of javaToNativeBlockNames. In reverse, the first
// row whose typ names a Java block wins, and nativeToJavaBlockNames and the
// native suffixes name the rest.
var blockRules = []blockRule{
	{java: blocks("cave_vines", "cave_vines_plant"), name: caveVinesName,
		native: blocksWithPrefix("cave_vines"), typ: func(name string, _ map[string]any) string {
			if strings.HasSuffix(name, "body_with_berries") {
				return "minecraft:cave_vines_plant"
			}
			return "minecraft:cave_vines"
		}},
	{java: blocks("water_cauldron", "lava_cauldron", "powder_snow_cauldron"), name: renamed("minecraft:cauldron"),
		native: blocks("cauldron"), typ: cauldronType, check: func(_ string, props map[string]any) error {
			switch liquid := nativePropertyText(props, "cauldron_liquid"); liquid {
			case "water", "powder_snow", "lava", "empty", "":
				return nil
			default:
				return fmt.Errorf("java state codec: unsupported native cauldron liquid %q", liquid)
			}
		}},
	{java: blocks("daylight_detector"), name: ifTrue("inverted", "minecraft:daylight_detector_inverted")},
	{java: blocksWithPrefix("potted_"), name: unlessNative("minecraft:flower_pot")},
	{java: blocksWithSuffix("_wall_hanging_sign"), name: func(_ chunk.BlockRegistry, java string, _ map[string]any, _ string) (string, error) {
		return strings.TrimSuffix(java, "_wall_hanging_sign") + "_hanging_sign", nil
	}, native: blocksWithSuffix("_hanging_sign"), typ: func(name string, props map[string]any) string {
		if !nativeBoolValue(props["hanging"]) {
			return strings.TrimSuffix(name, "_hanging_sign") + "_wall_hanging_sign"
		}
		return ""
	}, props: javaHangingSignProperties},
	{java: blocks("oak_sign"), name: unlessNative("")},
	{java: blocksWithSuffix("_sign").and(not(blocks("oak_sign").or(blocksWithSuffix("_wall_sign", "_hanging_sign")))), name: woodSignName},
	{java: blocksWithSuffix("_wall_sign"), name: unlessNative("minecraft:wall_sign")},
	{java: blocks("repeater", "comparator"), name: poweredName,
		native: blocksWithPrefix("powered_").or(blocksWithPrefix("unpowered_")), fix: func(name string, _, out map[string]any) error {
			out["powered"] = strings.HasPrefix(name, "minecraft:powered_")
			return nil
		}},
	// Bedrock tells lit furnaces, lamps, ores and torches by their name.
	{java: blocks("furnace", "smoker", "blast_furnace", "redstone_lamp", "redstone_ore", "deepslate_redstone_ore"), name: ifTrue("lit", ""),
		native: blocks("furnace", "smoker", "blast_furnace", "redstone_lamp", "redstone_ore", "deepslate_redstone_ore",
			"lit_furnace", "lit_smoker", "lit_blast_furnace", "lit_redstone_lamp", "lit_redstone_ore", "lit_deepslate_redstone_ore"),
		fix: func(name string, _, out map[string]any) error {
			out["lit"] = strings.HasPrefix(name, "minecraft:lit_")
			return nil
		}},
	{java: blocks("redstone_torch", "redstone_wall_torch"), name: redstoneTorchName,
		native: blocks("redstone_torch", "lit_redstone_torch", "unlit_redstone_torch"), fix: func(name string, _, out map[string]any) error {
			out["lit"] = name != "minecraft:unlit_redstone_torch"
			return nil
		}},
	{java: blocks("big_dripleaf_stem"), name: unlessNative("minecraft:big_dripleaf"),
		native: blocks("big_dripleaf"), typ: func(_ string, props map[string]any) string {
			if head, _ := boolProperty(props["big_dripleaf_head"]); head {
				return "minecraft:big_dripleaf"
			}
			return "minecraft:big_dripleaf_stem"
		}, fix: func(_ string, _, out map[string]any) error {
			delete(out, "big_dripleaf_head")
			return nil
		}},
	{java: blocks("small_dripleaf"), name: unlessNative("minecraft:small_dripleaf_block")},
	{java: blocks("light"), name: lightName,
		native: blocksWithPrefix("light_block_"), typ: func(string, map[string]any) string { return "minecraft:light" },
		fix: func(name string, _, out map[string]any) error {
			level, err := strconv.Atoi(strings.TrimPrefix(name, "minecraft:light_block_"))
			if err != nil || level < 0 || level > 15 {
				return fmt.Errorf("java state codec: invalid native light block name %q", name)
			}
			out["level"] = int32(level)
			return nil
		}},
	{java: blocks("double_plant"), name: func(_ chunk.BlockRegistry, java string, _ map[string]any, _ string) (string, error) {
		return "", fmt.Errorf("java state codec: unsupported synthetic Java block name %q", java)
	}, native: blocks("double_plant"), typ: func(_ string, props map[string]any) string {
		return nativeDoublePlantJavaTypes[nativePropertyText(props, "double_plant_type", "tall_grass_type", "type")]
	}, fix: func(_ string, props, out map[string]any) error {
		typ := nativePropertyText(props, "double_plant_type", "tall_grass_type", "type")
		if _, found := nativeDoublePlantJavaTypes[typ]; !found {
			return fmt.Errorf("java state codec: unsupported native double plant type %q", typ)
		}
		out["half"] = "lower"
		if upper, _ := boolProperty(props["upper_block_bit"]); upper {
			out["half"] = "upper"
		}
		return nil
	}},
	{java: blocksWithSuffix("_slab"), name: doubleSlabName,
		native: blocksWhere(isNativeDoubleSlab), fix: func(_ string, _, out map[string]any) error {
			out["type"] = "double"
			delete(out, "half")
			return nil
		}},
	{java: blocks("tall_seagrass"), name: unlessNative("minecraft:seagrass"),
		native: blocks("seagrass"), typ: func(_ string, props map[string]any) string {
			switch nativePropertyText(props, "sea_grass_type") {
			case "double_top", "double_bot":
				return "minecraft:tall_seagrass"
			}
			return ""
		}, fix: func(_ string, props, out map[string]any) error {
			switch nativePropertyText(props, "sea_grass_type") {
			case "double_top":
				out["half"] = "upper"
			case "double_bot":
				out["half"] = "lower"
			}
			delete(out, "sea_grass_type")
			return nil
		}},
	{java: blocks("kelp_plant"), name: unlessNative("minecraft:kelp")},
	{java: blocks("piston_head"), name: pistonHeadName},
	{native: blocks("red_flower"), typ: func(_ string, props map[string]any) string {
		return nativeRedFlowerJavaTypes[nativePropertyText(props, "red_flower_type", "flower_type", "type")]
	}, fix: func(_ string, props, _ map[string]any) error {
		flowerType := nativePropertyText(props, "red_flower_type", "flower_type", "type")
		if _, found := nativeRedFlowerJavaTypes[flowerType]; !found {
			return fmt.Errorf("java state codec: unsupported native red flower type %q", flowerType)
		}
		return nil
	}},
	// A native skull on a wall is a Java wall skull or head; Java has no
	// ceiling skull.
	{native: blocksWhere(isSkull), typ: func(name string, props map[string]any) string {
		if facing := intPropertyValue(props["facing_direction"]); facing >= 2 && facing <= 5 {
			if base, ok := strings.CutSuffix(name, "_skull"); ok {
				return base + "_wall_skull"
			}
			if base, ok := strings.CutSuffix(name, "_head"); ok {
				return base + "_wall_head"
			}
		}
		return name
	}, check: func(_ string, props map[string]any) error {
		if intPropertyValue(props["facing_direction"]) == 0 {
			return fmt.Errorf("java state codec: native ceiling skull has no Java block-state representation")
		}
		return nil
	}},
	{native: blocks("torch", "soul_torch", "copper_torch", "redstone_torch", "unlit_redstone_torch", "lit_redstone_torch"),
		typ: wallTorchType, check: func(_ string, props map[string]any) error {
			switch facing := nativePropertyText(props, "torch_facing_direction"); facing {
			case "north", "south", "east", "west", "top":
				return nil
			case "unknown":
				return fmt.Errorf("java state codec: native torch orientation %q has no Java block-state representation", "unknown")
			default:
				return fmt.Errorf("java state codec: invalid native torch orientation %q", facing)
			}
		}},
}

func (s javaSel) and(other javaSel) javaSel {
	return func(name string) bool { return s(name) && other(name) }
}

func not(s javaSel) javaSel { return func(name string) bool { return !s(name) } }

func caveVinesName(_ chunk.BlockRegistry, java string, props map[string]any, _ string) (string, error) {
	berries, _ := boolProperty(props["berries"])
	switch {
	case berries && java == "minecraft:cave_vines":
		return "minecraft:cave_vines_head_with_berries", nil
	case berries:
		return "minecraft:cave_vines_body_with_berries", nil
	}
	return "minecraft:cave_vines", nil
}

// renamed names the native block to.
func renamed(to string) func(chunk.BlockRegistry, string, map[string]any, string) (string, error) {
	return func(chunk.BlockRegistry, string, map[string]any, string) (string, error) { return to, nil }
}

// unlessNative names the native block to, if the Java name is no native
// block. An empty to is a sign: standing with a rotation, else on a wall.
func unlessNative(to string) func(chunk.BlockRegistry, string, map[string]any, string) (string, error) {
	return func(registry chunk.BlockRegistry, java string, props map[string]any, name string) (string, error) {
		switch {
		case hasNativeBlock(registry, java):
			return name, nil
		case to != "":
			return to, nil
		case props["rotation"] != nil:
			return "minecraft:standing_sign", nil
		}
		return "minecraft:wall_sign", nil
	}
}

func woodSignName(registry chunk.BlockRegistry, java string, props map[string]any, name string) (string, error) {
	if hasNativeBlock(registry, java) {
		return name, nil
	}
	wood := strings.TrimSuffix(java, "_sign")
	if wood == "minecraft:dark_oak" {
		wood = "minecraft:darkoak"
	}
	if props["rotation"] != nil {
		return wood + "_standing_sign", nil
	}
	return wood + "_wall_sign", nil
}

// ifTrue names the native block to, or lit_ before the Java name with to
// empty, when the boolean property key is true.
func ifTrue(key, to string) func(chunk.BlockRegistry, string, map[string]any, string) (string, error) {
	return func(_ chunk.BlockRegistry, java string, props map[string]any, name string) (string, error) {
		if on, _ := boolProperty(props[key]); !on {
			return name, nil
		}
		if to == "" {
			return "minecraft:lit_" + strings.TrimPrefix(java, "minecraft:"), nil
		}
		return to, nil
	}
}

func poweredName(_ chunk.BlockRegistry, java string, props map[string]any, _ string) (string, error) {
	prefix := "unpowered_"
	if powered, _ := boolProperty(props["powered"]); powered {
		prefix = "powered_"
	}
	return "minecraft:" + prefix + strings.TrimPrefix(java, "minecraft:"), nil
}

func redstoneTorchName(_ chunk.BlockRegistry, _ string, props map[string]any, name string) (string, error) {
	if lit, present := boolProperty(props["lit"]); present && !lit {
		return "minecraft:unlit_redstone_torch", nil
	}
	if name != "minecraft:unlit_redstone_torch" {
		return "minecraft:redstone_torch", nil
	}
	return name, nil
}

func lightName(registry chunk.BlockRegistry, java string, props map[string]any, name string) (string, error) {
	if hasNativeBlock(registry, java) {
		return name, nil
	}
	level, ok := intProperty(props["level"])
	if !ok {
		level = 0
	}
	if level < 0 || level > 15 {
		return "", fmt.Errorf("java state codec: invalid light level %d", level)
	}
	return "minecraft:light_block_" + strconv.Itoa(level), nil
}

func doubleSlabName(registry chunk.BlockRegistry, java string, props map[string]any, name string) (string, error) {
	if slabType, ok := stringProperty(props["type"]); !ok || slabType != "double" {
		return name, nil
	}
	if base, ok := strings.CutSuffix(name, "cut_copper_slab"); ok {
		name = base + "double_cut_copper_slab"
	} else {
		name = strings.TrimSuffix(name, "_slab") + "_double_slab"
	}
	if !hasNativeBlock(registry, name) {
		return "", fmt.Errorf("java state codec: no native double-slab block for %q", java)
	}
	return name, nil
}

func pistonHeadName(_ chunk.BlockRegistry, _ string, props map[string]any, name string) (string, error) {
	headType, ok := stringProperty(props["type"])
	if !ok || (headType != "normal" && headType != "sticky") {
		return "", fmt.Errorf("java state codec: invalid piston head type %v", props["type"])
	}
	if headType == "sticky" {
		return "minecraft:sticky_piston_arm_collision", nil
	}
	return name, nil
}

func cauldronType(_ string, props map[string]any) string {
	if intPropertyValue(props["fill_level"]) <= 0 {
		return ""
	}
	switch nativePropertyText(props, "cauldron_liquid") {
	case "water":
		return "minecraft:water_cauldron"
	case "powder_snow":
		return "minecraft:powder_snow_cauldron"
	case "lava":
		return "minecraft:lava_cauldron"
	}
	return ""
}

func wallTorchType(name string, props map[string]any) string {
	switch nativePropertyText(props, "torch_facing_direction") {
	case "north", "south", "east", "west":
	default:
		return ""
	}
	switch {
	case strings.Contains(name, "redstone"):
		return "minecraft:redstone_wall_torch"
	case name == "minecraft:soul_torch":
		return "minecraft:soul_wall_torch"
	case name == "minecraft:copper_torch":
		return "minecraft:copper_wall_torch"
	}
	return "minecraft:wall_torch"
}
