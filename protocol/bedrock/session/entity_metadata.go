package session

import (
	"cmp"
	"maps"
	"math"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/potion"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

var (
	boxedBytes  [256]any
	boxedInt16s [301]any
	boxedInt32s [302]any // index 0 is -1, index 1 is 0, ..., index 301 is 300

	metaFloat32Zero        any = float32(0)
	metaFloat32Quarter     any = float32(0.25)
	metaFloat32Five16      any = float32(0.3125)
	metaFloat32Half        any = float32(0.5)
	metaFloat32SixTenths   any = float32(0.6)
	metaFloat32NineEight   any = float32(0.98)
	metaFloat32One         any = float32(1)
	metaFloat32OneHalf     any = float32(1.5)
	metaFloat32OneEight    any = float32(1.8)
	metaFloat32OneNine     any = float32(1.9)
	metaFloat32Two         any = float32(2)
	metaFloat32Min         any = float32(math.SmallestNonzeroFloat32)
	metaInt32Max           any = int32(math.MaxInt32)
	metaInt32Heartbeat     any = int32(376)
	metaInt64Zero          any = int64(0)
	metaEmptyStr           any = ""
	metaBuoyancy           any = buoyancyData
	metaSleepWidth         any = float32(sleepColliderWidth)
	metaSleepHeight        any = float32(sleepColliderHeight)
	metaInteractContainer  any = "action.interact.opencontainer"
	metaInteractBoat       any = "action.interact.ride.boat"
	metaInteractMinecart   any = "action.interact.ride.minecart"
	metaSleepSoundInterval any = float32(8)
	metaSleepSoundSpread   any = float32(16)
	metaSleepSoundEvent    any = "sleep"

	witherTargetKeys = [3]uint32{mc.EntityDataKeyTargetA, mc.EntityDataKeyTargetB, mc.EntityDataKeyTargetC}
)

func init() {
	for i := range boxedBytes {
		boxedBytes[i] = byte(i)
	}
	for i := range boxedInt16s {
		boxedInt16s[i] = int16(i)
	}
	for i := range boxedInt32s {
		boxedInt32s[i] = int32(i - 1)
	}
	slots := make(map[uintptr]entityTypeSlot, 128)
	entityTypeSlots.Store(&slots)
	byVal := make(map[world.EntityType]*entityTypeMeta, 128)
	entityTypeByVal.Store(&byVal)
}

func boxByte(b byte) any     { return boxedBytes[b] }
func boxBoolByte(v bool) any { return boxedBytes[boolByte(v)] }
func boxString(s string) any {
	if s == "" {
		return metaEmptyStr
	}
	return s
}

func boxInt16(v int16) any {
	if v >= 0 && v <= 300 {
		return boxedInt16s[v]
	}
	return v
}

func boxInt32(v int32) any {
	if v >= -1 && v <= 300 {
		return boxedInt32s[v+1]
	}
	return v
}

func boxInt64(v int64) any {
	if v == 0 {
		return metaInt64Zero
	}
	return v
}

func boxFloat32(v float32) any {
	switch math.Float32bits(v) {
	case 0:
		return metaFloat32Zero
	case 0x3e800000: // 0.25
		return metaFloat32Quarter
	case 0x3ea00000: // 0.3125
		return metaFloat32Five16
	case 0x3f000000: // 0.5
		return metaFloat32Half
	case 0x3f19999a: // 0.6
		return metaFloat32SixTenths
	case 0x3f7ae148: // 0.98
		return metaFloat32NineEight
	case 0x3f800000: // 1.0
		return metaFloat32One
	case 0x3fc00000: // 1.5
		return metaFloat32OneHalf
	case 0x3fe66666: // 1.8
		return metaFloat32OneEight
	case 0x3ff33333: // 1.9
		return metaFloat32OneNine
	case 0x40000000: // 2.0
		return metaFloat32Two
	default:
		return v
	}
}

// actorFlags accumulates the two 64-bit Bedrock actor flag masks in registers
// so writers avoid repeated map lookups and int64 interface boxing.
type actorFlags struct {
	low, high   int64
	touchedLow  bool
	touchedHigh bool
}

func (f *actorFlags) set(flag uint8) {
	if flag < 64 {
		f.low |= 1 << flag
		f.touchedLow = true
		return
	}
	f.high |= 1 << (flag - 64)
	f.touchedHigh = true
}

func (f *actorFlags) unset(flag uint8) {
	if flag < 64 {
		f.low &^= 1 << flag
		f.touchedLow = true
		return
	}
	f.high &^= 1 << (flag - 64)
	f.touchedHigh = true
}

// entityTypeMeta caches the per-world.EntityType static metadata descriptors so
// parseEntityMetadata and entityProperties avoid repeated table lookups and
// value boxing on every entity state update.
type entityTypeMeta struct {
	netID                    string
	baseFlags0               int64
	baseFlags0Breathing      int64
	baseFlags1               int64
	baseFlags0Boxed          any
	baseFlags0BreathingBoxed any
	baseFlags1Boxed          any

	hasShape     bool
	hasBabyShape bool
	dynamicShape bool
	adultW       any
	adultH       any
	adultS       any
	babyW        any
	babyH        any
	babyS        any

	isPlayer       bool
	isFox          bool
	soundInterval  any
	soundSpread    any
	soundEvent     any
	fallMultiplier any

	props        []actorProperty
	defaultProps []mc.IntegerEntityProperty
	mapHint      atomic.Int32
}

type entityTypeSlot struct {
	comparable bool
	singleton  *entityTypeMeta // non-nil when concrete type has size 0 (unique value)
}

var (
	entityTypeMetaMu sync.Mutex
	entityTypeSlots  atomic.Pointer[map[uintptr]entityTypeSlot]
	entityTypeByVal  atomic.Pointer[map[world.EntityType]*entityTypeMeta]
)

func entityTypeMetaOf(t world.EntityType) *entityTypeMeta {
	if t == nil {
		return nil
	}
	tp := efaceType(t)
	if slot, ok := (*entityTypeSlots.Load())[tp]; ok {
		if slot.singleton != nil {
			return slot.singleton
		}
		if slot.comparable {
			if meta, ok := (*entityTypeByVal.Load())[t]; ok {
				return meta
			}
		} else {
			return buildEntityTypeMeta(t)
		}
	}
	return entityTypeMetaSlow(t, tp)
}

func entityTypeMetaSlow(t world.EntityType, tp uintptr) *entityTypeMeta {
	entityTypeMetaMu.Lock()
	defer entityTypeMetaMu.Unlock()

	slots := *entityTypeSlots.Load()
	slot, hasSlot := slots[tp]
	if hasSlot {
		if slot.singleton != nil {
			return slot.singleton
		}
		if !slot.comparable {
			return buildEntityTypeMeta(t)
		}
		if meta, ok := (*entityTypeByVal.Load())[t]; ok {
			return meta
		}
	}

	meta := buildEntityTypeMeta(t)
	if !hasSlot {
		rt := reflect.TypeOf(t)
		comp := rt.Comparable()
		slot = entityTypeSlot{comparable: comp}
		if comp && rt.Size() == 0 {
			slot.singleton = meta
		}
		nextSlots := make(map[uintptr]entityTypeSlot, len(slots)+1)
		maps.Copy(nextSlots, slots)
		nextSlots[tp] = slot
		entityTypeSlots.Store(&nextSlots)
	}
	if slot.comparable && slot.singleton == nil {
		oldByVal := *entityTypeByVal.Load()
		nextByVal := make(map[world.EntityType]*entityTypeMeta, len(oldByVal)+1)
		maps.Copy(nextByVal, oldByVal)
		nextByVal[t] = meta
		entityTypeByVal.Store(&nextByVal)
	}
	return meta
}

func buildEntityTypeMeta(t world.EntityType) *entityTypeMeta {
	id := networkID(t)
	props := actorPropertyTypes[id]
	var defaultProps []mc.IntegerEntityProperty
	if len(props) > 0 {
		defaultProps = make([]mc.IntegerEntityProperty, len(props))
		for i, p := range props {
			defaultProps[i] = mc.IntegerEntityProperty{Index: uint32(i), Value: p.def}
		}
	}
	meta := &entityTypeMeta{
		netID:        id,
		isPlayer:     id == "minecraft:player",
		isFox:        id == "minecraft:fox",
		props:        props,
		defaultProps: defaultProps,
	}
	var f actorFlags
	if flags, ok := bedrockActorFlags[id]; ok {
		for _, flag := range flags {
			f.set(flag)
		}
	} else {
		f.set(mc.EntityDataFlagHasGravity)
		f.set(mc.EntityDataFlagClimb)
	}
	if g, ok := t.(glint); ok && g.Glint() {
		f.set(mc.EntityDataFlagEnchanted)
	}
	if t == entity.LingeringPotionType {
		f.set(mc.EntityDataFlagLingering)
	}
	meta.baseFlags0 = f.low
	meta.baseFlags0Breathing = f.low | (1 << mc.EntityDataFlagBreathing)
	meta.baseFlags1 = f.high
	meta.baseFlags0Boxed = boxInt64(f.low)
	meta.baseFlags0BreathingBoxed = boxInt64(meta.baseFlags0Breathing)
	meta.baseFlags1Boxed = boxInt64(f.high)

	switch id {
	case "minecraft:slime", "minecraft:magma_cube", "minecraft:salmon", "minecraft:camel", "minecraft:camel_husk":
		meta.dynamicShape = true
	}
	if shapes, ok := bedrockActorShapes[id]; ok {
		meta.hasShape = true
		meta.adultW = boxFloat32(shapes[0].width)
		meta.adultH = boxFloat32(shapes[0].height)
		meta.adultS = boxFloat32(shapes[0].scale)
		if shapes[1] != (bedrockShape{}) {
			meta.hasBabyShape = true
			meta.babyW = boxFloat32(shapes[1].width)
			meta.babyH = boxFloat32(shapes[1].height)
			meta.babyS = boxFloat32(shapes[1].scale)
		}
	}
	if !meta.isPlayer {
		sound, ok := ambientSounds[id]
		if !ok {
			sound = defaultAmbientSound
		}
		meta.soundInterval = boxFloat32(sound.interval)
		meta.soundSpread = boxFloat32(sound.spread)
		meta.soundEvent = sound.event
		if noFallDamage[id] {
			meta.fallMultiplier = metaFloat32Zero
		} else {
			meta.fallMultiplier = metaFloat32One
		}
	}
	return meta
}

// parseEntityMetadata returns an entity metadata object with default values. It is equivalent to setting
// all properties to their default values and disabling all flags.
func (s *Session) parseEntityMetadata(e world.Entity) mc.Metadata {
	t := e.H().Type()
	if t == nil {
		return s.parseEntityMetadataSlow(e, t)
	}
	return s.parseEntityMetadataWith(e, entityTypeMetaOf(t), metaCapsOf(e))
}

func (s *Session) parseEntityMetadataWith(e world.Entity, meta *entityTypeMeta, caps metaCaps) mc.Metadata {
	if meta == nil {
		return s.parseEntityMetadataSlow(e, e.H().Type())
	}
	hint := int(meta.mapHint.Load())
	if hint == 0 {
		hint = 28
	}
	m := make(mc.Metadata, hint)

	m[mc.EntityDataKeyPlayerFlags] = boxedBytes[0]
	m[mc.EntityDataKeyEffectColor] = boxedInt32s[1]
	m[mc.EntityDataKeyEffectAmbience] = boxedBytes[0]
	m[mc.EntityDataKeyColorIndex] = boxedBytes[0]

	var beh entity.Behaviour
	var behCaps metaCaps
	if caps.hi&capBehaviour != 0 {
		beh = e.(behaviourHolder).Behaviour()
		behCaps = metaCapsOf(beh)
	}

	writeBaseShapeMetadata(e, meta, caps, beh, behCaps, m)
	writeBaseAmbientMetadata(e, meta, caps, m)

	if caps.hi&capRiderSeat != 0 {
		writeSeatMetadata(m, e.(riderSeat).RiderSeat())
	} else {
		m[mc.EntityDataKeySeatLockPassengerRotation] = boxedBytes[0]
		m[mc.EntityDataKeySeatLockPassengerRotationDegrees] = metaFloat32Zero
		m[mc.EntityDataKeySeatRotationOffset] = boxedBytes[0]
		m[mc.EntityDataKeySeatRotationOffsetDegrees] = metaFloat32Zero
	}

	flags := actorFlags{low: meta.baseFlags0, high: meta.baseFlags1}
	flags = s.addSpecificMetadataFlags(e, m, flags, caps)
	if beh != nil {
		flags = s.addSpecificMetadataFlags(beh, m, flags, behCaps)
	}
	if caps.hi&capAppearance != 0 {
		s.writeActorAppearance(e, m, &flags)
	}
	if mayHaveMetaPatch(e, caps, beh, behCaps) {
		if patch, ok := collectEntityMetaPatch(e, caps, beh, behCaps); ok {
			flags = applyActorMetaPatch(e, beh, m, flags, &patch)
		}
	}
	switch flags.low {
	case meta.baseFlags0:
		m[mc.EntityDataKeyFlags] = meta.baseFlags0Boxed
	case meta.baseFlags0Breathing:
		m[mc.EntityDataKeyFlags] = meta.baseFlags0BreathingBoxed
	default:
		m[mc.EntityDataKeyFlags] = boxInt64(flags.low)
	}
	if flags.high == meta.baseFlags1 {
		m[mc.EntityDataKeyFlagsTwo] = meta.baseFlags1Boxed
	} else {
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(flags.high)
	}
	if n := int32(len(m)); n > meta.mapHint.Load() {
		meta.mapHint.Store(n)
	}
	return m
}

func writeBaseShapeMetadata(e world.Entity, meta *entityTypeMeta, caps metaCaps, beh entity.Behaviour, behCaps metaCaps, m mc.Metadata) {
	switch {
	case meta.hasShape && !meta.dynamicShape:
		if meta.hasBabyShape && actorIsBabyWith(e, caps, beh, behCaps) {
			m[mc.EntityDataKeyWidth] = meta.babyW
			m[mc.EntityDataKeyHeight] = meta.babyH
			m[mc.EntityDataKeyScale] = meta.babyS
		} else {
			m[mc.EntityDataKeyWidth] = meta.adultW
			m[mc.EntityDataKeyHeight] = meta.adultH
			m[mc.EntityDataKeyScale] = meta.adultS
		}
	case meta.dynamicShape:
		writeDynamicShapeMetadata(e, meta, caps, beh, behCaps, m)
	default:
		bb := e.H().Type().BBox(e)
		m[mc.EntityDataKeyWidth] = boxFloat32(float32(bb.Width()))
		m[mc.EntityDataKeyHeight] = boxFloat32(float32(bb.Height()))
	}
}

func writeDynamicShapeMetadata(e world.Entity, meta *entityTypeMeta, caps metaCaps, beh entity.Behaviour, behCaps metaCaps, m mc.Metadata) {
	switch c := e.(type) {
	case *entity.Camel:
		if !c.Sitting() {
			if meta.hasBabyShape && actorIsBabyWith(e, caps, beh, behCaps) {
				m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight], m[mc.EntityDataKeyScale] = meta.babyW, meta.babyH, meta.babyS
			} else {
				m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight], m[mc.EntityDataKeyScale] = meta.adultW, meta.adultH, meta.adultS
			}
			return
		}
	case *entity.CamelHusk:
		if !c.Sitting() {
			m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight], m[mc.EntityDataKeyScale] = meta.adultW, meta.adultH, meta.adultS
			return
		}
	case *entity.Salmon:
		m[mc.EntityDataKeyWidth] = meta.adultW
		m[mc.EntityDataKeyHeight] = meta.adultH
		scale := [...]any{metaFloat32Half, metaFloat32One, metaFloat32OneHalf}[min(max(c.Variant(), 0), 2)]
		m[mc.EntityDataKeyScale] = scale
		return
	}
	if shape, ok := actorShape(e, meta.netID); ok {
		m[mc.EntityDataKeyWidth] = boxFloat32(shape.width)
		m[mc.EntityDataKeyHeight] = boxFloat32(shape.height)
		m[mc.EntityDataKeyScale] = boxFloat32(shape.scale)
		return
	}
	bb := e.H().Type().BBox(e)
	m[mc.EntityDataKeyWidth] = boxFloat32(float32(bb.Width()))
	m[mc.EntityDataKeyHeight] = boxFloat32(float32(bb.Height()))
}

func writeBaseAmbientMetadata(e world.Entity, meta *entityTypeMeta, caps metaCaps, m mc.Metadata) {
	if meta.isPlayer {
		return
	}
	if meta.isFox && caps.hi&capFoxSleeping != 0 && e.(foxSleeper).Sleeping() {
		m[mc.EntityDataKeyAmbientSoundInterval] = metaSleepSoundInterval
		m[mc.EntityDataKeyAmbientSoundIntervalRange] = metaSleepSoundSpread
		m[mc.EntityDataKeyAmbientSoundEventName] = metaSleepSoundEvent
	} else {
		m[mc.EntityDataKeyAmbientSoundInterval] = meta.soundInterval
		m[mc.EntityDataKeyAmbientSoundIntervalRange] = meta.soundSpread
		m[mc.EntityDataKeyAmbientSoundEventName] = meta.soundEvent
	}
	m[mc.EntityDataKeyFallDamageMultiplier] = meta.fallMultiplier
}

func (s *Session) parseEntityMetadataSlow(e world.Entity, t world.EntityType) mc.Metadata {
	bb := t.BBox(e)
	m := make(mc.Metadata, 28)
	m[mc.EntityDataKeyFlags] = metaInt64Zero
	m[mc.EntityDataKeyFlagsTwo] = metaInt64Zero
	m[mc.EntityDataKeyPlayerFlags] = boxedBytes[0]
	m[mc.EntityDataKeyWidth] = float32(bb.Width())
	m[mc.EntityDataKeyHeight] = float32(bb.Height())
	m[mc.EntityDataKeyEffectColor] = boxedInt32s[1]
	m[mc.EntityDataKeyEffectAmbience] = boxedBytes[0]
	m[mc.EntityDataKeyColorIndex] = boxedBytes[0]

	setActorFlag(m, mc.EntityDataFlagHasGravity)
	setActorFlag(m, mc.EntityDataFlagClimb)
	writeActorShape(e, networkID(t), m)
	if g, ok := t.(glint); ok && g.Glint() {
		setActorFlag(m, mc.EntityDataFlagEnchanted)
	}
	if t == entity.LingeringPotionType {
		setActorFlag(m, mc.EntityDataFlagLingering)
	}
	addAmbientSoundMetadata(e, m)
	var seat world.RiderSeat
	if r, ok := e.(riderSeat); ok {
		seat = r.RiderSeat()
	}
	writeSeatMetadata(m, seat)
	s.addSpecificMetadata(e, m)
	var behEntity any
	var behCaps metaCaps
	if bh, ok := e.(behaviourHolder); ok {
		behEntity = bh.Behaviour()
		behCaps = metaCapsOf(behEntity)
		s.addSpecificMetadata(behEntity, m)
	}
	f0, _ := m[mc.EntityDataKeyFlags].(int64)
	f1, _ := m[mc.EntityDataKeyFlagsTwo].(int64)
	f := actorFlags{low: f0, high: f1}
	s.writeActorAppearance(e, m, &f)
	caps := metaCapsOf(e)
	if mayHaveMetaPatch(e, caps, behEntity, behCaps) {
		if patch, ok := collectEntityMetaPatch(e, caps, behEntity, behCaps); ok {
			f = applyActorMetaPatch(e, behEntity, m, f, &patch)
		}
	}
	if f.touchedLow {
		m[mc.EntityDataKeyFlags] = boxInt64(f.low)
	}
	if f.touchedHigh {
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(f.high)
	}
	return m
}

// mayHaveMetaPatch reports whether e, its behaviour, or its ECS component bag
// could carry an ActorMetaPatch override.
func mayHaveMetaPatch(e world.Entity, caps metaCaps, beh any, behCaps metaCaps) bool {
	if caps.hi&capMetadataContributor != 0 || (beh != nil && behCaps.hi&capMetadataContributor != 0) {
		return true
	}
	h := e.H()
	return h != nil && !h.Components().Empty()
}

// collectEntityMetaPatch gathers and merges metadata overrides contributed by e,
// its behaviour, and its EntityHandle's MetaPatchKey component.
func collectEntityMetaPatch(e world.Entity, caps metaCaps, beh any, behCaps metaCaps) (world.ActorMetaPatch, bool) {
	var patch world.ActorMetaPatch
	var hasContrib bool
	if caps.hi&capMetadataContributor != 0 {
		e.(world.MetadataContributor).ContributeMetadata(&patch)
		hasContrib = true
	}
	if beh != nil && behCaps.hi&capMetadataContributor != 0 {
		beh.(world.MetadataContributor).ContributeMetadata(&patch)
		hasContrib = true
	}
	if h := e.H(); h != nil {
		if bag := h.Components(); !bag.Empty() {
			if ecsPatch, ok := ecs.Get(bag, world.MetaPatchKey); ok {
				if !hasContrib {
					return ecsPatch, !ecsPatch.IsZero()
				}
				mergeActorMetaPatch(&patch, &ecsPatch)
			}
		}
	}
	return patch, hasContrib && !patch.IsZero()
}

func mergeActorMetaPatch(dst, src *world.ActorMetaPatch) {
	if src.NameTag != nil {
		dst.NameTag = src.NameTag
	}
	if src.AlwaysShowNameTag != nil {
		dst.AlwaysShowNameTag = src.AlwaysShowNameTag
	}
	if src.ScoreTag != nil {
		dst.ScoreTag = src.ScoreTag
	}
	if src.Scale != nil {
		dst.Scale = src.Scale
	}
	if src.Width != nil {
		dst.Width = src.Width
	}
	if src.Height != nil {
		dst.Height = src.Height
	}
	if src.Variant != nil {
		dst.Variant = src.Variant
	}
	if src.MarkVariant != nil {
		dst.MarkVariant = src.MarkVariant
	}
	if src.Color != nil {
		dst.Color = src.Color
	}
	if src.Invisible != nil {
		dst.Invisible = src.Invisible
	}
	if src.OnFire != nil {
		dst.OnFire = src.OnFire
	}
	if src.NoAI != nil {
		dst.NoAI = src.NoAI
	}
	dst.FlagSet = (dst.FlagSet &^ src.FlagClear) | src.FlagSet
	dst.FlagClear = (dst.FlagClear &^ src.FlagSet) | src.FlagClear
	dst.Flag2Set = (dst.Flag2Set &^ src.Flag2Clear) | src.Flag2Set
	dst.Flag2Clear = (dst.Flag2Clear &^ src.Flag2Set) | src.Flag2Clear
	if len(src.CustomStrings) > 0 {
		if dst.CustomStrings == nil {
			dst.CustomStrings = make(map[string]string, len(src.CustomStrings))
		}
		maps.Copy(dst.CustomStrings, src.CustomStrings)
	}
	if len(src.CustomInts) > 0 {
		if dst.CustomInts == nil {
			dst.CustomInts = make(map[string]int32, len(src.CustomInts))
		}
		maps.Copy(dst.CustomInts, src.CustomInts)
	}
	if len(src.CustomFloats) > 0 {
		if dst.CustomFloats == nil {
			dst.CustomFloats = make(map[string]float32, len(src.CustomFloats))
		}
		maps.Copy(dst.CustomFloats, src.CustomFloats)
	}
}

func applyActorMetaPatch(e world.Entity, beh any, m mc.Metadata, f actorFlags, patch *world.ActorMetaPatch) actorFlags {
	if patch.NameTag != nil || patch.AlwaysShowNameTag != nil {
		nameTag, _ := m[mc.EntityDataKeyName].(string)
		alwaysShow := entityAlwaysShowNameTag(e, beh)
		if patch.NameTag != nil {
			nameTag = *patch.NameTag
		}
		if patch.AlwaysShowNameTag != nil {
			alwaysShow = *patch.AlwaysShowNameTag
		}
		f = writeNameTagMetadataFlags(m, f, nameTag, alwaysShow)
	}
	if patch.ScoreTag != nil {
		m[mc.EntityDataKeyScore] = boxString(*patch.ScoreTag)
	}
	if patch.Scale != nil {
		m[mc.EntityDataKeyScale] = boxFloat32(*patch.Scale)
	}
	if patch.Width != nil {
		m[mc.EntityDataKeyWidth] = boxFloat32(*patch.Width)
	}
	if patch.Height != nil {
		m[mc.EntityDataKeyHeight] = boxFloat32(*patch.Height)
	}
	if patch.Variant != nil {
		m[mc.EntityDataKeyVariant] = boxInt32(*patch.Variant)
	}
	if patch.MarkVariant != nil {
		m[mc.EntityDataKeyMarkVariant] = boxInt32(*patch.MarkVariant)
	}
	if patch.Color != nil {
		m[mc.EntityDataKeyColorIndex] = boxByte(*patch.Color)
	}
	if patch.Invisible != nil {
		setOrUnsetActorFlag(&f, mc.EntityDataFlagInvisible, *patch.Invisible)
	}
	if patch.OnFire != nil {
		setOrUnsetActorFlag(&f, mc.EntityDataFlagOnFire, *patch.OnFire)
	}
	if patch.NoAI != nil {
		setOrUnsetActorFlag(&f, mc.EntityDataFlagNoAI, *patch.NoAI)
	}
	if patch.FlagSet != 0 {
		f.low |= int64(patch.FlagSet)
		f.touchedLow = true
	}
	if patch.FlagClear != 0 {
		f.low &^= int64(patch.FlagClear)
		f.touchedLow = true
	}
	if patch.Flag2Set != 0 {
		f.high |= int64(patch.Flag2Set)
		f.touchedHigh = true
	}
	if patch.Flag2Clear != 0 {
		f.high &^= int64(patch.Flag2Clear)
		f.touchedHigh = true
	}
	return f
}

func entityAlwaysShowNameTag(e, beh any) bool {
	if a, ok := e.(alwaysShowNameTag); ok {
		return a.AlwaysShowNameTag()
	}
	if beh != nil {
		if a, ok := beh.(alwaysShowNameTag); ok {
			return a.AlwaysShowNameTag()
		}
	}
	return true
}

func setOrUnsetActorFlag(f *actorFlags, flag uint8, on bool) {
	if on {
		f.set(flag)
		return
	}
	f.unset(flag)
}

// addSpecificMetadata writes the metadata of e that depends on its dynamic type,
// gated by the cached per-type capability bitmask.
func (s *Session) addSpecificMetadata(e any, m mc.Metadata) {
	f0, has0 := m[mc.EntityDataKeyFlags].(int64)
	f1, has1 := m[mc.EntityDataKeyFlagsTwo].(int64)
	f := s.addSpecificMetadataFlags(e, m, actorFlags{low: f0, high: f1}, metaCapsOf(e))
	if f.touchedLow {
		if !has0 {
			_ = m[mc.EntityDataKeyFlags].(int64)
		}
		m[mc.EntityDataKeyFlags] = boxInt64(f.low)
	}
	if f.touchedHigh {
		if !has1 {
			_ = m[mc.EntityDataKeyFlagsTwo].(int64)
		}
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(f.high)
	}
}

// The bits of the horse flags of the actor data of mounts.
const (
	horseFlagEating   = 32
	horseFlagStanding = 128
)

// addMountMetadata writes the flags and data Bedrock clients show of horses,
// llamas and camels, as the dedicated server sends them.
func addMountMetadata(s entity.MountState, m mc.Metadata) {
	f0, has0 := m[mc.EntityDataKeyFlags].(int64)
	f1, has1 := m[mc.EntityDataKeyFlagsTwo].(int64)
	f := addMountMetadataFlags(s, m, actorFlags{low: f0, high: f1})
	if f.touchedLow {
		if !has0 {
			_ = m[mc.EntityDataKeyFlags].(int64)
		}
		m[mc.EntityDataKeyFlags] = boxInt64(f.low)
	}
	if f.touchedHigh {
		if !has1 {
			_ = m[mc.EntityDataKeyFlagsTwo].(int64)
		}
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(f.high)
	}
}

func addMountMetadataFlags(s entity.MountState, m mc.Metadata, f actorFlags) actorFlags {
	if s.Saddled {
		f.set(mc.EntityDataFlagSaddled)
	}
	if s.Tamed {
		f.set(mc.EntityDataFlagTamed)
	}
	if s.Chested {
		f.set(mc.EntityDataFlagChested)
	}
	if s.Sitting {
		f.set(mc.EntityDataFlagSitting)
	}
	if s.Controlled {
		f.set(mc.EntityDataFlagKeyboardControlled)
	}
	if s.PowerJump {
		f.set(mc.EntityDataFlagPowerJump)
	}
	if s.Dash {
		f.set(mc.EntityDataFlagDash)
	}
	if s.Rearing {
		var flags int64
		if s.Eating {
			flags |= horseFlagEating
		}
		if s.Standing {
			flags |= horseFlagStanding
		}
		m[mc.EntityDataKeyHorseFlags] = boxInt64(flags)
	}
	if s.ContainerSize > 0 {
		m[mc.EntityDataKeyContainerType] = boxByte(byte(mc.ContainerTypeHorse))
		m[mc.EntityDataKeyContainerSize] = boxInt32(s.ContainerSize)
		// BDS sends a strength modifier of 3 for tamed llamas, the only
		// mounts with a strength, and 0 for the others.
		var modifier int32
		if s.MaxStrength != 0 {
			modifier = 3
		}
		m[mc.EntityDataKeyContainerStrengthModifier] = boxInt32(modifier)
	}
	if s.MaxStrength > 0 {
		m[mc.EntityDataKeyStrength] = boxInt32(s.Strength)
		m[mc.EntityDataKeyStrengthMax] = boxInt32(s.MaxStrength)
	}
	return f
}

// nameTagState returns the public name tag of an entity, whether that name tag is shown at all distances
// and whether the entity has a name tag at all. Entities that do not report an always show state show
// their name tag at all distances.
func nameTagState(e any) (string, bool, bool) {
	alwaysShow := true
	if a, ok := e.(alwaysShowNameTag); ok {
		alwaysShow = a.AlwaysShowNameTag()
	}
	n, ok := e.(named)
	if !ok {
		return "", alwaysShow, false
	}
	return n.NameTag(), alwaysShow, true
}

// writeNameTagMetadata writes a name tag and its related visibility properties to metadata.
func writeNameTagMetadata(m mc.Metadata, nameTag string, alwaysShow bool) {
	show := nameTag != ""
	always := show && alwaysShow

	m[mc.EntityDataKeyName] = boxString(nameTag)
	m[mc.EntityDataKeyAlwaysShowNameTag] = boxBoolByte(always)
	if show {
		setActorFlag(m, mc.EntityDataFlagShowName)
	} else {
		m.UnsetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagShowName)
	}
	if always {
		setActorFlag(m, mc.EntityDataFlagAlwaysShowName)
	} else {
		m.UnsetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagAlwaysShowName)
	}
}

func writeNameTagMetadataFlags(m mc.Metadata, f actorFlags, nameTag string, alwaysShow bool) actorFlags {
	show := nameTag != ""
	always := show && alwaysShow

	m[mc.EntityDataKeyName] = boxString(nameTag)
	m[mc.EntityDataKeyAlwaysShowNameTag] = boxBoolByte(always)
	if show {
		f.set(mc.EntityDataFlagShowName)
	} else {
		f.unset(mc.EntityDataFlagShowName)
	}
	if always {
		f.set(mc.EntityDataFlagAlwaysShowName)
	} else {
		f.unset(mc.EntityDataFlagAlwaysShowName)
	}
	return f
}

type sneaker interface {
	Sneaking() bool
}

type sprinter interface {
	Sprinting() bool
}

type swimmer interface {
	Swimming() bool
}

type crawler interface {
	Crawling() bool
}

type glider interface {
	Gliding() bool
}

type blocker interface {
	Blocking() (bool, bool)
}

type breather interface {
	Breathing() bool
	AirSupply() time.Duration
	MaxAirSupply() time.Duration
}

type immobile interface {
	Immobile() bool
}

type invisible interface {
	Invisible() bool
}

type scaled interface {
	Scale() float64
}

type owned interface {
	Owner() *world.EntityHandle
}

// leashed is an entity held by a lead; HolderLeashID is the holder's unique ID.
type leashed interface {
	HolderLeashID() int64
}

type named interface {
	NameTag() string
}

type alwaysShowNameTag interface {
	AlwaysShowNameTag() bool
}

type scoreTag interface {
	ScoreTag() string
}

type splash interface {
	Potion() potion.Potion
}

type glint interface {
	Glint() bool
}

type areaEffectCloud interface {
	effectBearer
	Radius() float64
}

type onFire interface {
	OnFireDuration() time.Duration
}

type baby interface {
	Baby() bool
}

type doorBreaker interface {
	CanBreakDoors() bool
}

type breakingObstruction interface {
	BreakingObstruction() bool
}

type armsRaised interface {
	ArmsRaised() bool
}

type effectBearer interface {
	Effects() []effect.Effect
}

type using interface {
	UsingItem() bool
}

type sleeper interface {
	Sleeping() (cube.Pos, bool)
}

// bedSleeper is a mob that lies in a bed, as villagers do. BDS shows such a
// mob with the SLEEPING flag and the small box of the behaviour.sleep
// component, in place of the bed position and player flag of players.
type bedSleeper interface {
	sleeper
	// SleepFacing returns the direction from the foot of the bed to its head.
	SleepFacing() cube.Direction
}

// Width and height of the box BDS gives a villager lying in a bed
// (sleep_collider_width and sleep_collider_height).
const (
	sleepColliderWidth  = 1
	sleepColliderHeight = 0.3
)

// writeBedSleeperMetadataFlags writes the state of a mob in a bed, if it sleeps.
func writeBedSleeperMetadataFlags(bs bedSleeper, m mc.Metadata, f actorFlags) actorFlags {
	if _, ok := bs.Sleeping(); !ok {
		return f
	}
	f.set(mc.EntityDataFlagSleeping)
	m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight] = metaSleepWidth, metaSleepHeight
	return f
}

type arrow interface {
	Critical() bool
}

type orb interface {
	Experience() int
}

type firework interface {
	Firework() item.Firework
	Attached() bool
}

type gameMode interface {
	GameMode() world.GameMode
}

type tnt interface {
	Fuse() time.Duration
}

type igniteable interface {
	Ignited() bool
	FuseTicks() int
}

type living interface {
	UUID() uuid.UUID
	DeathPosition() (mgl64.Vec3, world.Dimension, bool)
}

type variable interface {
	Variant() int32
}

type markVariable interface {
	MarkVariant() int32
}

type armourStand interface {
	PoseIndex() int
}

type hurtDuration interface {
	HurtDuration() time.Duration
}

type endCrystalMeta interface {
	ShowBase() bool
	BeamTarget() (mgl64.Vec3, bool)
}

// witherMeta is implemented by withers, whose heads turn to their targets.
type witherMeta interface {
	HeadTargets() [3]*world.EntityHandle
	InvulnerableTicks() int
}

type displayTile interface {
	DisplayBlock() (world.Block, bool)
	DisplayOffset() int
	CustomDisplay() bool
}

type containerMeta interface {
	ContainerKind() entity.ContainerKind
	ContainerSize() int32
	ContainerStrengthModifier() int32
}

type minecartDamage interface {
	RollingAmplitude() int
	RollingDirection() int
	Damage() float64
}

type rowTime interface {
	RowTimeLeft() float32
	RowTimeRight() float32
}

type buoyantMeta interface {
	Buoyant() bool
}

// buoyancyData is the buoyancy data BDS sends of boats, the buoyant
// vehicles.
const buoyancyData = "{\"apply_gravity\":true,\"base_buoyancy\":1.0,\"big_wave_probability\":0.02999999932944775,\"big_wave_speed\":10.0,\"drag_down_on_buoyancy_removed\":0.0,\"liquid_blocks\":[\"minecraft:water\",\"minecraft:flowing_water\"],\"simulate_waves\":true}"

// interactText returns the prompt clients show when they look at a
// vehicle: to open the container of a chest boat or container minecart, to
// ride a boat with a free seat or an empty minecart that carries
// passengers, and none otherwise. It reports whether e is of a kind that may
// have a prompt, which depends on its dynamic type alone.
func interactText(e any) (string, bool) {
	v, ok := interactTextBoxed(e)
	if v == nil {
		return "", ok
	}
	return v.(string), ok
}

func interactTextBoxed(e any) (any, bool) {
	if _, ok := e.(containerMeta); ok {
		return metaInteractContainer, true
	}
	switch v := e.(type) {
	case interface{ Full() bool }:
		if !v.Full() {
			return metaInteractBoat, true
		}
		return nil, true
	case interface {
		Rideable() bool
		Passenger() *world.EntityHandle
	}:
		if v.Rideable() && v.Passenger() == nil {
			return metaInteractMinecart, true
		}
		return nil, true
	}
	return nil, false
}

type riderSeat interface {
	RiderSeat() world.RiderSeat
}

type rideTarget interface {
	CanRideTarget() bool
}

// writeSeatMetadata writes the seat an entity rides, or last rode, in. BDS
// sends these keys for every entity and sets them on riders, never on
// vehicles, and marks riding only through entity links. It leaves out the
// seat position of an entity that has not sat anywhere yet.
func writeSeatMetadata(m mc.Metadata, seat world.RiderSeat) {
	if seat.Position != (mgl64.Vec3{}) {
		m[mc.EntityDataKeySeatOffset] = vec64To32(seat.Position)
	}
	m[mc.EntityDataKeySeatLockPassengerRotation] = boxBoolByte(seat.LockRotation)
	m[mc.EntityDataKeySeatLockPassengerRotationDegrees] = boxFloat32(float32(seat.LockRotationDegrees))
	m[mc.EntityDataKeySeatRotationOffset] = boxBoolByte(seat.RotateRider)
	m[mc.EntityDataKeySeatRotationOffsetDegrees] = boxFloat32(float32(seat.RotateRiderDegrees))
}

// snifferStateFlags holds the flag Bedrock shows for each state of a sniffer.
var snifferStateFlags = map[string]uint8{
	"feeling_happy": mc.EntityDataFlagFeelingHappy,
	"scenting":      mc.EntityDataFlagScenting,
	"sniffing":      mc.EntityDataFlagSniffing,
	"searching":     mc.EntityDataFlagSearching,
	"digging":       mc.EntityDataFlagDigging,
	"rising":        mc.EntityDataFlagRising,
}

// packVisibleEffects packs the effects with particles shown seven bits each,
// the ID shifted left once and ambience in the lowest bit, as clients show
// them over a mob. BDS packs them in order of their ID, the lowest in the
// highest bits, however they were added.
func packVisibleEffects(effects []effect.Effect) int64 {
	if len(effects) == 0 {
		return 0
	}
	type visible struct {
		id      int
		ambient bool
	}
	var stackBuf [16]visible
	shown := stackBuf[:0]
	for _, ef := range effects {
		if id, found := effect.ID(ef.Type()); found && !ef.ParticlesHidden() {
			shown = append(shown, visible{id: id, ambient: ef.Ambient()})
		}
	}
	if len(shown) == 0 {
		return 0
	}
	if len(shown) > 1 {
		slices.SortFunc(shown, func(a, b visible) int { return cmp.Compare(a.id, b.id) })
	}
	var packed int64
	for _, v := range shown {
		packed = packed<<7 | int64(v.id<<1)
		if v.ambient {
			packed |= 1
		}
	}
	return packed
}
