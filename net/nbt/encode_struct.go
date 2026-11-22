package nbt

import (
	"math"
	"reflect"
	"slices"
	"unsafe"
)

// marshalReflect encodes a reflect.Value with a tag name passed to its NBT representation.
func (e *Encoder) marshalReflect(val reflect.Value, tagName string) error {
	if val.Kind() == reflect.Interface {
		val = val.Elem()
	}
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	t := tagFromType(val.Type())
	if t == math.MaxUint8 {
		return IncompatibleTypeError{Type: val.Type(), ValueName: tagName}
	}
	if err := e.writeTag(t, tagName); err != nil {
		return err
	}
	return e.encodeReflect(val, tagName)
}

// encodeReflect encodes the payload of a reflect.Value without writing the tag header.
func (e *Encoder) encodeReflect(val reflect.Value, tagName string) error {
	kind := val.Kind()
	if kind == reflect.Interface {
		val = val.Elem()
		kind = val.Kind()
	}
	switch kind {
	case reflect.Uint8:
		return e.w.writeByte(byte(val.Uint()))
	case reflect.Bool:
		return e.writeBool(val.Bool())
	case reflect.Int16:
		return e.writeInt16(int16(val.Int()))
	case reflect.Int32:
		return e.writeInt32(int32(val.Int()))
	case reflect.Int64:
		return e.writeInt64(val.Int())
	case reflect.Float32:
		return e.writeFloat32(float32(val.Float()))
	case reflect.Float64:
		return e.writeFloat64(val.Float())
	case reflect.String:
		return e.writeString(val.String())
	case reflect.Array:
		return e.encodeArrayReflect(val)
	case reflect.Slice:
		return e.encodeSliceReflect(val, tagName)
	case reflect.Struct:
		return e.encodeStructReflect(val)
	case reflect.Map:
		return e.encodeMapReflect(val, tagName)
	}
	return nil
}

func (e *Encoder) encodeArrayReflect(val reflect.Value) error {
	n := val.Cap()
	switch val.Type().Elem().Kind() {
	case reflect.Uint8:
		if err := e.writeInt32(int32(n)); err != nil {
			return err
		}
		if val.CanAddr() {
			data := unsafe.Slice((*byte)(val.Addr().UnsafePointer()), n)
			if err := e.w.writeBytes(data); err != nil {
				return FailedWriteError{Op: "WriteByteArray", Off: e.w.offset()}
			}
			return nil
		}
		if e.w.useRaw {
			e.w.raw = slices.Grow(e.w.raw, n)
			for i := 0; i < n; i++ {
				e.w.raw = append(e.w.raw, byte(val.Index(i).Uint()))
			}
			return nil
		}
		for off := 0; off < n; off += len(e.w.buf) {
			chunk := min(len(e.w.buf), n-off)
			for i := 0; i < chunk; i++ {
				e.w.buf[i] = byte(val.Index(off + i).Uint())
			}
			if err := e.w.writeBytes(e.w.buf[:chunk]); err != nil {
				return FailedWriteError{Op: "WriteByteArray", Off: e.w.offset()}
			}
		}
		return nil
	case reflect.Int32:
		if err := e.writeInt32(int32(n)); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := e.writeInt32(int32(val.Index(i).Int())); err != nil {
				return err
			}
		}
	case reflect.Int64:
		if err := e.writeInt32(int32(n)); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			if err := e.writeInt64(val.Index(i).Int()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Encoder) encodeSliceReflect(val reflect.Value, tagName string) error {
	e.depth++
	elemType := val.Type().Elem()
	if elemType.Kind() == reflect.Interface {
		if val.Len() == 0 {
			elemType = nil
		} else {
			elemType = val.Index(0).Elem().Type()
		}
	}
	listType := tagFromType(elemType)
	if listType == math.MaxUint8 {
		return IncompatibleTypeError{Type: val.Type(), ValueName: tagName}
	}
	if err := e.writeSliceHeader(listType, val.Len()); err != nil {
		return err
	}
	n := val.Len()
	if elemType != nil && elemType.Kind() == reflect.Struct && n > 0 {
		elemInfo := getStructInfo(elemType)
		elemSize := elemType.Size()
		elemBase := val.Index(0).Addr().UnsafePointer()
		for i := 0; i < n; i++ {
			e.depth++
			if err := e.encodeStructPtr(unsafe.Add(elemBase, uintptr(i)*elemSize), elemInfo); err != nil {
				return err
			}
			e.depth--
			if err := e.w.writeByte(byte(tagEnd)); err != nil {
				return err
			}
		}
		e.depth--
		return nil
	}
	for i := 0; i < n; i++ {
		if err := e.encodeReflect(val.Index(i), ""); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeStructReflect(val reflect.Value) error {
	e.depth++
	info := getStructInfo(val.Type())
	if val.CanAddr() {
		if err := e.encodeStructPtr(val.Addr().UnsafePointer(), info); err != nil {
			return err
		}
	} else {
		if err := e.encodeStructNonAddr(val, info); err != nil {
			return err
		}
	}
	e.depth--
	return e.w.writeByte(byte(tagEnd))
}

func (e *Encoder) encodeMapReflect(val reflect.Value, tagName string) error {
	e.depth++
	if val.Type().Key().Kind() != reflect.String {
		return IncompatibleTypeError{Type: val.Type(), ValueName: tagName}
	}
	iter := val.MapRange()
	for iter.Next() {
		if err := e.marshalReflect(iter.Value(), iter.Key().String()); err != nil {
			return err
		}
	}
	e.depth--
	return e.w.writeByte(byte(tagEnd))
}

func (e *Encoder) encodeStructPtr(base unsafe.Pointer, info *structInfo) error {
	for _, f := range info.encodeFields {
		if err := e.encodeStructFieldPtr(unsafe.Add(base, f.offset), f); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeStructFieldPtr(ptr unsafe.Pointer, f *structField) error {
	switch f.fastKind {
	case fastByte:
		v := *(*byte)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagByte, f.name); err != nil {
			return err
		}
		return e.w.writeByte(v)
	case fastBool:
		v := *(*bool)(ptr)
		if f.omitEmpty && !v {
			return nil
		}
		if err := e.writeTag(tagByte, f.name); err != nil {
			return err
		}
		return e.writeBool(v)
	case fastInt16:
		v := *(*int16)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagInt16, f.name); err != nil {
			return err
		}
		return e.writeInt16(v)
	case fastInt32:
		v := *(*int32)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagInt32, f.name); err != nil {
			return err
		}
		return e.writeInt32(v)
	case fastInt64:
		v := *(*int64)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagInt64, f.name); err != nil {
			return err
		}
		return e.writeInt64(v)
	case fastFloat32:
		v := *(*float32)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagFloat32, f.name); err != nil {
			return err
		}
		return e.writeFloat32(v)
	case fastFloat64:
		v := *(*float64)(ptr)
		if f.omitEmpty && v == 0 {
			return nil
		}
		if err := e.writeTag(tagFloat64, f.name); err != nil {
			return err
		}
		return e.writeFloat64(v)
	case fastString:
		v := *(*string)(ptr)
		if f.omitEmpty && v == "" {
			return nil
		}
		if err := e.writeTag(tagString, f.name); err != nil {
			return err
		}
		return e.writeString(v)
	default:
		return e.encodeStructFieldPtrComplex(ptr, f)
	}
}

func (e *Encoder) encodeStructFieldPtrComplex(ptr unsafe.Pointer, f *structField) error {
	switch f.fastKind {
	case fastMapStringAny:
		v := *(*map[string]any)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagStruct, f.name); err != nil {
			return err
		}
		return e.encodeMapAny(v)
	case fastAny:
		v := *(*any)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		return e.marshalAny(v, f.name)
	case fastStruct:
		if f.omitEmpty && reflect.NewAt(f.typ, ptr).Elem().IsZero() {
			return nil
		}
		if err := e.writeTag(tagStruct, f.name); err != nil {
			return err
		}
		e.depth++
		if err := e.encodeStructPtr(ptr, f.loadSubInfo(f.typ)); err != nil {
			return err
		}
		e.depth--
		return e.w.writeByte(byte(tagEnd))
	case fastByteArray:
		s := unsafe.Slice((*byte)(ptr), f.arrayLen)
		if f.omitEmpty && isZeroBytes(s) {
			return nil
		}
		if err := e.writeTag(tagByteArray, f.name); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(s))); err != nil {
			return err
		}
		if err := e.w.writeBytes(s); err != nil {
			return FailedWriteError{Op: "WriteByteArray", Off: e.w.offset()}
		}
		return nil
	case fastInt32Array:
		s := unsafe.Slice((*int32)(ptr), f.arrayLen)
		if f.omitEmpty && isZeroInt32s(s) {
			return nil
		}
		if err := e.writeTag(tagInt32Array, f.name); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(s))); err != nil {
			return err
		}
		for _, x := range s {
			if err := e.writeInt32(x); err != nil {
				return err
			}
		}
		return nil
	case fastInt64Array:
		s := unsafe.Slice((*int64)(ptr), f.arrayLen)
		if f.omitEmpty && isZeroInt64s(s) {
			return nil
		}
		if err := e.writeTag(tagInt64Array, f.name); err != nil {
			return err
		}
		if err := e.writeInt32(int32(len(s))); err != nil {
			return err
		}
		for _, x := range s {
			if err := e.writeInt64(x); err != nil {
				return err
			}
		}
		return nil
	default:
		return e.encodeStructFieldPtrSlice(ptr, f)
	}
}

func (e *Encoder) encodeStructFieldPtrSlice(ptr unsafe.Pointer, f *structField) error {
	switch f.fastKind {
	case fastSliceByte:
		v := *(*[]byte)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeByteSlice(v)
	case fastSliceBool:
		v := *(*[]bool)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeBoolSlice(v)
	case fastSliceInt16:
		v := *(*[]int16)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeInt16Slice(v)
	case fastSliceInt32:
		v := *(*[]int32)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeInt32Slice(v)
	case fastSliceInt64:
		v := *(*[]int64)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeInt64Slice(v)
	case fastSliceFloat32:
		v := *(*[]float32)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeFloat32Slice(v)
	case fastSliceFloat64:
		v := *(*[]float64)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeFloat64Slice(v)
	case fastSliceString:
		v := *(*[]string)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeStringSlice(v)
	default:
		return e.encodeStructFieldPtrAnySlice(ptr, f)
	}
}

func (e *Encoder) encodeStructFieldPtrAnySlice(ptr unsafe.Pointer, f *structField) error {
	switch f.fastKind {
	case fastSliceAny:
		v := *(*[]any)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeSliceAny(v, f.name)
	case fastSliceMapAny:
		v := *(*[]map[string]any)(ptr)
		if f.omitEmpty && v == nil {
			return nil
		}
		if err := e.writeTag(tagSlice, f.name); err != nil {
			return err
		}
		return e.encodeSliceMapAny(v)
	case fastSliceStruct:
		return e.encodeStructSliceField(ptr, f)
	default:
		fv := reflect.NewAt(f.typ, ptr).Elem()
		if f.omitEmpty && fv.IsZero() {
			return nil
		}
		return e.marshalReflect(fv, f.name)
	}
}

func (e *Encoder) encodeStructSliceField(ptr unsafe.Pointer, f *structField) error {
	fv := reflect.NewAt(f.typ, ptr).Elem()
	if f.omitEmpty && fv.IsNil() {
		return nil
	}
	if err := e.writeTag(tagSlice, f.name); err != nil {
		return err
	}
	e.depth++
	n := fv.Len()
	if err := e.writeSliceHeader(tagStruct, n); err != nil {
		return err
	}
	if n > 0 {
		elemInfo := f.loadSubInfo(f.elemType)
		elemBase := fv.Index(0).Addr().UnsafePointer()
		for i := 0; i < n; i++ {
			e.depth++
			if err := e.encodeStructPtr(unsafe.Add(elemBase, uintptr(i)*f.elemSize), elemInfo); err != nil {
				return err
			}
			e.depth--
			if err := e.w.writeByte(byte(tagEnd)); err != nil {
				return err
			}
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeStructNonAddr(val reflect.Value, info *structInfo) error {
	for _, f := range info.encodeFields {
		fv := f.fieldValue(val)
		if f.omitEmpty && fv.IsZero() {
			continue
		}
		if err := e.marshalReflect(fv, f.name); err != nil {
			return err
		}
	}
	return nil
}

func isZeroBytes(s []byte) bool {
	for _, b := range s {
		if b != 0 {
			return false
		}
	}
	return true
}

func isZeroInt32s(s []int32) bool {
	for _, x := range s {
		if x != 0 {
			return false
		}
	}
	return true
}

func isZeroInt64s(s []int64) bool {
	for _, x := range s {
		if x != 0 {
			return false
		}
	}
	return true
}
