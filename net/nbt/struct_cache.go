package nbt

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

type fastKind uint8

const (
	fastNone fastKind = iota
	fastByte
	fastBool
	fastInt16
	fastInt32
	fastInt64
	fastFloat32
	fastFloat64
	fastString
	fastByteArray
	fastInt32Array
	fastInt64Array
	fastSliceByte
	fastSliceBool
	fastSliceInt16
	fastSliceInt32
	fastSliceInt64
	fastSliceFloat32
	fastSliceFloat64
	fastSliceString
	fastSliceAny
	fastSliceMapAny
	fastSliceStruct
	fastMapStringAny
	fastAny
	fastStruct
)

var (
	anyType               = reflect.TypeFor[any]()
	sliceByteType         = reflect.TypeFor[[]byte]()
	sliceBoolType         = reflect.TypeFor[[]bool]()
	sliceInt16Type        = reflect.TypeFor[[]int16]()
	sliceInt32Type        = reflect.TypeFor[[]int32]()
	sliceInt64Type        = reflect.TypeFor[[]int64]()
	sliceFloat32Type      = reflect.TypeFor[[]float32]()
	sliceFloat64Type      = reflect.TypeFor[[]float64]()
	sliceStringType       = reflect.TypeFor[[]string]()
	sliceAnyType          = reflect.TypeFor[[]any]()
	sliceMapStringAnyType = reflect.TypeFor[[]map[string]any]()
)

// structField stores pre-parsed metadata for a single exported struct field.
type structField struct {
	name        string
	index       []int
	singleIndex int
	offset      uintptr
	typ         reflect.Type
	elemType    reflect.Type
	elemSize    uintptr
	arrayLen    int
	fastKind    fastKind
	omitEmpty   bool
	subInfo     atomic.Pointer[structInfo]
}

// loadSubInfo lazily loads and caches struct metadata for nested or slice-element structs,
// avoiding infinite recursion on self-referential struct types.
func (f *structField) loadSubInfo(t reflect.Type) *structInfo {
	if p := f.subInfo.Load(); p != nil {
		return p
	}
	p := getStructInfo(t)
	f.subInfo.Store(p)
	return p
}

// fieldValue returns the reflect.Value for f within struct value val.
func (f *structField) fieldValue(val reflect.Value) reflect.Value {
	if f.singleIndex >= 0 {
		return val.Field(f.singleIndex)
	}
	return val.FieldByIndex(f.index)
}

// structInfo holds cached encode and decode metadata for a struct reflect.Type.
type structInfo struct {
	encodeFields []*structField
	decodeByName map[string]*structField
}

var structCache sync.Map // map[reflect.Type]*structInfo

// getStructInfo returns cached metadata for struct type t.
func getStructInfo(t reflect.Type) *structInfo {
	if v, ok := structCache.Load(t); ok {
		return v.(*structInfo)
	}
	info := buildStructInfo(t)
	actual, _ := structCache.LoadOrStore(t, info)
	return actual.(*structInfo)
}

func buildStructInfo(t reflect.Type) *structInfo {
	info := &structInfo{
		decodeByName: make(map[string]*structField),
	}
	appendEncodeFields(t, nil, 0, &info.encodeFields)
	appendDecodeFields(t, nil, 0, info.decodeByName)
	return info
}

func appendEncodeFields(t reflect.Type, prefix []int, baseOffset uintptr, out *[]*structField) {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("nbt")
		if sf.PkgPath != "" || tag == "-" {
			continue
		}
		idx := appendIndex(prefix, i)
		off := baseOffset + sf.Offset
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			appendEncodeFields(sf.Type, idx, off, out)
			continue
		}
		name := sf.Name
		omitEmpty := false
		if strings.HasSuffix(tag, ",omitempty") {
			omitEmpty = true
			tag = strings.TrimSuffix(tag, ",omitempty")
		}
		if tag != "" {
			name = tag
		}
		*out = append(*out, newStructField(name, idx, off, sf.Type, omitEmpty))
	}
}

func appendDecodeFields(t reflect.Type, prefix []int, baseOffset uintptr, byName map[string]*structField) {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		idx := appendIndex(prefix, i)
		off := baseOffset + sf.Offset
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			appendDecodeFields(sf.Type, idx, off, byName)
			continue
		}
		name := sf.Name
		if tag, ok := sf.Tag.Lookup("nbt"); ok {
			if tag == "-" {
				continue
			}
			tag = strings.TrimSuffix(tag, ",omitempty")
			if tag != "" {
				name = tag
			}
		}
		byName[name] = newStructField(name, idx, off, sf.Type, false)
	}
}

func appendIndex(prefix []int, i int) []int {
	idx := make([]int, len(prefix)+1)
	copy(idx, prefix)
	idx[len(prefix)] = i
	return idx
}

func newStructField(name string, index []int, offset uintptr, typ reflect.Type, omitEmpty bool) *structField {
	f := &structField{
		name:        name,
		index:       index,
		singleIndex: -1,
		offset:      offset,
		typ:         typ,
		omitEmpty:   omitEmpty,
	}
	if len(index) == 1 {
		f.singleIndex = index[0]
	}
	f.fastKind, f.arrayLen, f.elemType, f.elemSize = classifyFieldType(typ)
	return f
}

func classifyFieldType(t reflect.Type) (fastKind, int, reflect.Type, uintptr) {
	switch t.Kind() {
	case reflect.Uint8:
		return fastByte, 0, nil, 0
	case reflect.Bool:
		return fastBool, 0, nil, 0
	case reflect.Int16:
		return fastInt16, 0, nil, 0
	case reflect.Int32:
		return fastInt32, 0, nil, 0
	case reflect.Int64:
		return fastInt64, 0, nil, 0
	case reflect.Float32:
		return fastFloat32, 0, nil, 0
	case reflect.Float64:
		return fastFloat64, 0, nil, 0
	case reflect.String:
		return fastString, 0, nil, 0
	case reflect.Struct:
		return fastStruct, 0, nil, 0
	case reflect.Interface:
		if t == anyType {
			return fastAny, 0, nil, 0
		}
	case reflect.Map:
		if t == mapStringAnyType {
			return fastMapStringAny, 0, nil, 0
		}
	case reflect.Array:
		switch t.Elem() {
		case byteType:
			return fastByteArray, t.Len(), byteType, 1
		case int32Type:
			return fastInt32Array, t.Len(), int32Type, 4
		case int64Type:
			return fastInt64Array, t.Len(), int64Type, 8
		}
	case reflect.Slice:
		return classifySliceType(t)
	}
	return fastNone, 0, nil, 0
}

func classifySliceType(t reflect.Type) (fastKind, int, reflect.Type, uintptr) {
	switch t {
	case sliceByteType:
		return fastSliceByte, 0, byteType, 1
	case sliceBoolType:
		return fastSliceBool, 0, nil, 1
	case sliceInt16Type:
		return fastSliceInt16, 0, nil, 2
	case sliceInt32Type:
		return fastSliceInt32, 0, int32Type, 4
	case sliceInt64Type:
		return fastSliceInt64, 0, int64Type, 8
	case sliceFloat32Type:
		return fastSliceFloat32, 0, nil, 4
	case sliceFloat64Type:
		return fastSliceFloat64, 0, nil, 8
	case sliceStringType:
		return fastSliceString, 0, stringType, 0
	case sliceAnyType:
		return fastSliceAny, 0, anyType, 0
	case sliceMapStringAnyType:
		return fastSliceMapAny, 0, mapStringAnyType, 0
	}
	if t.Elem().Kind() == reflect.Struct {
		return fastSliceStruct, 0, t.Elem(), t.Elem().Size()
	}
	return fastNone, 0, nil, 0
}

// Pre-boxed immutable scalar values avoid heap allocations when decoding common numbers into any.
var (
	boxedInt16     [256]any
	boxedInt16Neg1 any = int16(-1)
	boxedInt32     [256]any
	boxedInt32Neg1 any = int32(-1)
	boxedInt64     [64]any
	boxedInt64Neg1 any = int64(-1)

	boxedFloat32Zero any = float32(0)
	boxedFloat32One  any = float32(1)
	boxedFloat64Zero any = float64(0)
	boxedFloat64One  any = float64(1)
)

func init() {
	for i := range boxedInt16 {
		boxedInt16[i] = int16(i)
	}
	for i := range boxedInt32 {
		boxedInt32[i] = int32(i)
	}
	for i := range boxedInt64 {
		boxedInt64[i] = int64(i)
	}
}

func boxInt16(v int16) any {
	if uint16(v) < uint16(len(boxedInt16)) {
		return boxedInt16[v]
	}
	if v == -1 {
		return boxedInt16Neg1
	}
	return v
}

func boxInt32(v int32) any {
	if uint32(v) < uint32(len(boxedInt32)) {
		return boxedInt32[v]
	}
	if v == -1 {
		return boxedInt32Neg1
	}
	return v
}

func boxInt64(v int64) any {
	if uint64(v) < uint64(len(boxedInt64)) {
		return boxedInt64[v]
	}
	if v == -1 {
		return boxedInt64Neg1
	}
	return v
}

func boxFloat32(v float32) any {
	switch math.Float32bits(v) {
	case 0:
		return boxedFloat32Zero
	case 0x3f800000:
		return boxedFloat32One
	default:
		return v
	}
}

func boxFloat64(v float64) any {
	switch math.Float64bits(v) {
	case 0:
		return boxedFloat64Zero
	case 0x3ff0000000000000:
		return boxedFloat64One
	default:
		return v
	}
}
