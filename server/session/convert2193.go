package session

import (
	"fmt"
	"image/color"
	"reflect"
	"unsafe"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// to converts a value of gophertunnel's protocol types into the type T of
// the generated codecs of protocol 2193, field by field by name: the
// schema of the codecs was imported from gophertunnel, so the two share
// the names of their fields. A field of T that the value lacks is a bug of
// the conversion, and panics. It is for values of no weight on the CPU;
// those sent all the time convert by hand.
func to[T any](src any) T {
	var dst T
	if src == nil {
		return dst
	}
	// A copy is addressable, so that its unexported fields can be read.
	v := reflect.New(reflect.TypeOf(src)).Elem()
	v.Set(reflect.ValueOf(src))
	convertValue(reflect.ValueOf(&dst).Elem(), v)
	return dst
}

var (
	colourType    = reflect.TypeFor[color.RGBA]()
	protoColour   = reflect.TypeFor[proto.Color]()
	rgbType       = reflect.TypeFor[[3]float32]()
	gtInputFlags  = reflect.TypeFor[protocol.InputFlags]()
	bitsetType    = reflect.TypeFor[protocol.Bitset]()
	protoFlags    = reflect.TypeFor[v2193.InputFlags]()
	protoBitset   = reflect.TypeFor[v2193.Bitset]()
	optionalValue = "Value"
)

// unionTypes maps the concrete types gophertunnel's interfaces hold to
// those of the generated codecs, by name.
var unionTypes = func() map[string]reflect.Type {
	m := map[string]reflect.Type{}
	for _, v := range []any{
		// Item descriptors.
		v2193.InvalidItemDescriptor{}, v2193.DefaultItemDescriptor{}, v2193.MoLangItemDescriptor{},
		v2193.ItemTagItemDescriptor{},
		// Stack request actions.
		v2193.TakeStackRequestAction{}, v2193.PlaceStackRequestAction{}, v2193.SwapStackRequestAction{},
		v2193.DropStackRequestAction{}, v2193.DestroyStackRequestAction{}, v2193.ConsumeStackRequestAction{},
		v2193.CreateStackRequestAction{}, v2193.LabTableCombineStackRequestAction{}, v2193.BeaconPaymentStackRequestAction{},
		v2193.MineBlockStackRequestAction{}, v2193.CraftRecipeStackRequestAction{}, v2193.AutoCraftRecipeStackRequestAction{},
		v2193.CraftCreativeStackRequestAction{}, v2193.CraftRecipeOptionalStackRequestAction{},
		v2193.CraftGrindstoneRecipeStackRequestAction{}, v2193.CraftLoomRecipeStackRequestAction{},
		v2193.CraftNonImplementedStackRequestAction{}, v2193.CraftResultsDeprecatedStackRequestAction{},
		// Transaction data.
		v2193.NormalTransactionData{}, v2193.MismatchTransactionData{}, v2193.UseItemTransactionData{},
		v2193.UseItemOnEntityTransactionData{}, v2193.ReleaseItemTransactionData{},
		// Events.
		v2193.AchievementAwardedEvent{}, v2193.EntityInteractEvent{}, v2193.PortalBuiltEvent{}, v2193.PortalUsedEvent{},
		v2193.MobKilledEvent{}, v2193.CauldronUsedEvent{}, v2193.PlayerDiedEvent{}, v2193.BossKilledEvent{},
		v2193.AgentCommandEvent{}, v2193.AgentCreatedEvent{}, v2193.PatternRemovedEvent{}, v2193.SlashCommandExecutedEvent{},
		v2193.FishBucketedEvent{}, v2193.MobBornEvent{}, v2193.PetDiedEvent{}, v2193.CauldronInteractEvent{},
		v2193.ComposterInteractEvent{}, v2193.BellUsedEvent{}, v2193.EntityDefinitionTriggerEvent{}, v2193.RaidUpdateEvent{},
		v2193.MovementAnomalyEvent{}, v2193.MovementCorrectedEvent{}, v2193.ExtractHoneyEvent{}, v2193.TargetBlockHitEvent{},
		v2193.PiglinBarterEvent{}, v2193.WaxedOrUnwaxedCopperEvent{}, v2193.CodeBuilderRuntimeActionEvent{},
		v2193.CodeBuilderScoreboardEvent{}, v2193.StriderRiddenInLavaInOverworldEvent{}, v2193.SneakCloseToSculkSensorEvent{},
		v2193.CarefulRestorationEvent{}, v2193.ItemUsedEvent{},
		// Shapes.
		v2193.LastShape{}, v2193.ArrowShape{}, v2193.TextShape{}, v2193.BoxShape{}, v2193.LineShape{},
		v2193.SphereShape{}, v2193.CylinderShape{}, v2193.PyramidShape{}, v2193.EllipsoidShape{}, v2193.ConeShape{},
	} {
		t := reflect.TypeOf(v)
		m[t.Name()] = t
	}
	return m
}()

func convertValue(dst, src reflect.Value) {
	if !src.IsValid() {
		return
	}
	src = open(src)
	st, dt := src.Type(), dst.Type()
	switch {
	case st == dt:
		dst.Set(src)
		return
	case st == colourType && dt == protoColour:
		c := src.Interface().(color.RGBA)
		dst.Set(reflect.ValueOf(proto.Color{c.R, c.G, c.B, c.A}))
		return
	case st == colourType && dt == rgbType:
		// gophertunnel writes the channels of an rgb colour as fractions.
		c := src.Interface().(color.RGBA)
		dst.Set(reflect.ValueOf([3]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}))
		return
	case st == gtInputFlags && dt == protoFlags:
		ids := open(src.FieldByName("ids"))
		dst.Set(reflect.ValueOf(v2193.InputFlags(append([]int32(nil), ids.Interface().([]int32)...))))
		return
	case st == bitsetType && dt == protoBitset:
		b := src.Interface().(protocol.Bitset)
		var out v2193.Bitset
		for i := range b.Len() {
			if b.Load(i) {
				out.Set(i)
			}
		}
		dst.Set(reflect.ValueOf(out))
		return
	case dt.Kind() == reflect.Interface:
		if dt.NumMethod() == 0 {
			// Values of no type of their own, as those of NBT and game
			// rules, are the same on both sides.
			if st.Kind() == reflect.Interface {
				if !src.IsNil() {
					dst.Set(src.Elem())
				}
				return
			}
			dst.Set(src)
			return
		}
		if src.IsNil() {
			return
		}
		elem := src.Elem()
		for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Interface {
			elem = elem.Elem()
		}
		t, ok := unionTypes[elem.Type().Name()]
		if !ok {
			panic(fmt.Sprintf("session: no type of the codecs for %s", elem.Type()))
		}
		v := reflect.New(t)
		convertValue(v.Elem(), elem)
		dst.Set(v)
		return
	case src.Kind() == reflect.Pointer:
		if !src.IsNil() {
			convertValue(dst, src.Elem())
		}
		return
	case isOptional(dt):
		// gophertunnel's Optional keeps its value behind Value.
		out := src.MethodByName(optionalValue).Call(nil)
		if out[1].Bool() {
			dst.Field(0).SetBool(true)
			convertValue(dst.Field(1), out[0])
		}
		return
	case st.Kind() == dt.Kind() && st.ConvertibleTo(dt) && dt.Kind() != reflect.Struct,
		numeric(st) && numeric(dt):
		dst.Set(src.Convert(dt))
		return
	}
	switch dt.Kind() {
	case reflect.Struct:
		for i := range dt.NumField() {
			name := dt.Field(i).Name
			f := src.FieldByName(name)
			if !f.IsValid() {
				panic(fmt.Sprintf("session: %s has no field %s of %s", st, name, dt))
			}
			convertValue(dst.Field(i), f)
		}
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		out := reflect.MakeSlice(dt, src.Len(), src.Len())
		for i := range src.Len() {
			convertValue(out.Index(i), src.Index(i))
		}
		dst.Set(out)
	case reflect.Array:
		for i := range dt.Len() {
			convertValue(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		out := reflect.MakeMapWithSize(dt, src.Len())
		iter := src.MapRange()
		for iter.Next() {
			k := reflect.New(dt.Key()).Elem()
			convertValue(k, iter.Key())
			v := reflect.New(dt.Elem()).Elem()
			convertValue(v, iter.Value())
			out.SetMapIndex(k, v)
		}
		dst.Set(out)
	default:
		panic(fmt.Sprintf("session: cannot convert %s to %s", st, dt))
	}
}

// numeric reports an integer or floating point type.
func numeric(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// isOptional reports a proto.Optional.
func isOptional(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t.NumField() == 2 && t.Field(0).Name == "Set" && t.Field(1).Name == "Val" &&
		t.PkgPath() == reflect.TypeFor[proto.Optional[int]]().PkgPath()
}

// open makes the value of an unexported field usable.
func open(v reflect.Value) reflect.Value {
	if v.CanInterface() || !v.CanAddr() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}
