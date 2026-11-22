package nbt

import (
	"reflect"
	"unsafe"
)

func (d *Decoder) decodeStructPtr(base unsafe.Pointer, info *structInfo, tagName string) error {
	for {
		nestedTagType, nameBytes, err := d.tagBytes()
		if err != nil {
			return err
		}
		if nestedTagType == tagEnd {
			return nil
		}
		if !nestedTagType.IsValid() {
			return UnknownTagError{Off: d.r.offset(), Op: "Struct", TagType: nestedTagType}
		}
		f, ok := info.decodeByName[string(nameBytes)]
		if !ok {
			return UnexpectedNamedTagError{
				Off:     d.r.offset(),
				TagName: tagName + "." + string(nameBytes),
				TagType: nestedTagType,
			}
		}
		if err := d.decodeStructFieldPtr(unsafe.Add(base, f.offset), f, nestedTagType); err != nil {
			return err
		}
	}
}

func (d *Decoder) decodeStructFieldPtr(ptr unsafe.Pointer, f *structField, t tagType) error {
	switch f.fastKind {
	case fastByte:
		if t != tagByte {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.r.readByte()
		if err != nil {
			return BufferOverrunError{Op: "Byte"}
		}
		*(*byte)(ptr) = v
		return nil
	case fastBool:
		if t != tagByte {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.r.readByte()
		if err != nil {
			return BufferOverrunError{Op: "Byte"}
		}
		*(*bool)(ptr) = v == 1
		return nil
	case fastInt16:
		if t != tagInt16 {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readInt16()
		if err != nil {
			return err
		}
		*(*int16)(ptr) = v
		return nil
	case fastInt32:
		if t != tagInt32 {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readInt32()
		if err != nil {
			return err
		}
		*(*int32)(ptr) = v
		return nil
	case fastInt64:
		if t != tagInt64 {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readInt64()
		if err != nil {
			return err
		}
		*(*int64)(ptr) = v
		return nil
	case fastFloat32:
		if t != tagFloat32 {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readFloat32()
		if err != nil {
			return err
		}
		*(*float32)(ptr) = v
		return nil
	case fastFloat64:
		if t != tagFloat64 {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readFloat64()
		if err != nil {
			return err
		}
		*(*float64)(ptr) = v
		return nil
	case fastString:
		if t != tagString {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		v, err := d.readString()
		if err != nil {
			return err
		}
		*(*string)(ptr) = v
		return nil
	default:
		return d.decodeStructFieldPtrComplex(ptr, f, t)
	}
}

func (d *Decoder) decodeStructFieldPtrComplex(ptr unsafe.Pointer, f *structField, t tagType) error {
	switch f.fastKind {
	case fastMapStringAny:
		if t != tagStruct {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		d.depth++
		m, err := d.unmarshalCompoundAny()
		if err != nil {
			return err
		}
		d.depth--
		*(*map[string]any)(ptr) = m
		return nil
	case fastAny:
		v, err := d.unmarshalAny(t)
		if err != nil {
			return err
		}
		*(*any)(ptr) = v
		return nil
	case fastStruct:
		if t != tagStruct {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		d.depth++
		if err := d.decodeStructPtr(ptr, f.loadSubInfo(f.typ), f.name); err != nil {
			return err
		}
		d.depth--
		return nil
	case fastByteArray:
		if t != tagByteArray {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		return d.decodeByteArrayPtr(ptr, f)
	case fastInt32Array:
		if t != tagInt32Array {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		return d.decodeInt32ArrayPtr(ptr, f)
	case fastInt64Array:
		if t != tagInt64Array {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		return d.decodeInt64ArrayPtr(ptr, f)
	case fastSliceByte, fastSliceBool, fastSliceInt16, fastSliceInt32, fastSliceInt64,
		fastSliceFloat32, fastSliceFloat64, fastSliceString, fastSliceAny, fastSliceMapAny, fastSliceStruct:
		if t != tagSlice {
			return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
		}
		return d.decodeFastSlice(ptr, f)
	default:
		return d.unmarshalTag(reflect.NewAt(f.typ, ptr).Elem(), t, f.name)
	}
}

func (d *Decoder) decodeByteArrayPtr(ptr unsafe.Pointer, f *structField) error {
	length, err := d.readInt32()
	if err != nil {
		return err
	}
	if length < 0 {
		return BufferOverrunError{Op: "ByteArray"}
	}
	if int(length) == f.arrayLen {
		if err := d.r.readInto(unsafe.Slice((*byte)(ptr), f.arrayLen)); err != nil {
			return BufferOverrunError{Op: "ByteArray"}
		}
		return nil
	}
	if _, err := d.r.fixed(int64(length)); err != nil {
		return BufferOverrunError{Op: "ByteArray"}
	}
	return InvalidArraySizeError{Off: d.r.offset(), Op: "ByteArray", GoLength: f.arrayLen, NBTLength: int(length)}
}

func (d *Decoder) decodeInt32ArrayPtr(ptr unsafe.Pointer, f *structField) error {
	n, err := d.readInt32()
	if err != nil || n < 0 {
		return BufferOverrunError{Op: "Int32Slice"}
	}
	if int(n) == f.arrayLen {
		dst := unsafe.Slice((*int32)(ptr), f.arrayLen)
		for i := range dst {
			x, err := d.readInt32()
			if err != nil {
				return BufferOverrunError{Op: "Int32Slice"}
			}
			dst[i] = x
		}
		return nil
	}
	if d.encKind == encNetworkLittleEndian {
		if _, err := d.r.capacity(int64(n)); err != nil {
			return BufferOverrunError{Op: "Int32Slice"}
		}
		for range n {
			if _, err := d.readInt32(); err != nil {
				return BufferOverrunError{Op: "Int32Slice"}
			}
		}
	} else {
		if _, err := d.r.fixed(int64(n) * 4); err != nil {
			return BufferOverrunError{Op: "Int32Slice"}
		}
	}
	return InvalidArraySizeError{Off: d.r.offset(), Op: "Int32Array", GoLength: f.arrayLen, NBTLength: int(n)}
}

func (d *Decoder) decodeInt64ArrayPtr(ptr unsafe.Pointer, f *structField) error {
	n, err := d.readInt32()
	if err != nil || n < 0 {
		return BufferOverrunError{Op: "Int64Slice"}
	}
	if int(n) == f.arrayLen {
		dst := unsafe.Slice((*int64)(ptr), f.arrayLen)
		for i := range dst {
			x, err := d.readInt64()
			if err != nil {
				return BufferOverrunError{Op: "Int64Slice"}
			}
			dst[i] = x
		}
		return nil
	}
	if d.encKind == encNetworkLittleEndian {
		if _, err := d.r.capacity(int64(n)); err != nil {
			return BufferOverrunError{Op: "Int64Slice"}
		}
		for range n {
			if _, err := d.readInt64(); err != nil {
				return BufferOverrunError{Op: "Int64Slice"}
			}
		}
	} else {
		if _, err := d.r.fixed(int64(n) * 8); err != nil {
			return BufferOverrunError{Op: "Int64Slice"}
		}
	}
	return InvalidArraySizeError{Off: d.r.offset(), Op: "Int64Array", GoLength: f.arrayLen, NBTLength: int(n)}
}

func (d *Decoder) decodeFastSlice(ptr unsafe.Pointer, f *structField) error {
	if d.depth >= maximumNestingDepth {
		return MaximumDepthReachedError{}
	}
	d.depth++
	listTypeByte, err := d.r.readByte()
	if err != nil {
		return BufferOverrunError{Op: "Slice"}
	}
	listType := tagType(listTypeByte)
	if !listType.IsValid() {
		return UnknownTagError{Off: d.r.offset(), TagType: listType, Op: "Slice"}
	}
	switch listType {
	case tagByte:
		length, err := d.readInt32()
		if err != nil || length < 0 {
			return BufferOverrunError{Op: "ByteSlice"}
		}
		if length == 0 {
			setEmptyFastSlice(ptr, f)
			d.depth--
			return nil
		}
		b, err := d.r.fixed(int64(length))
		if err != nil {
			return BufferOverrunError{Op: "ByteSlice"}
		}
		if f.fastKind == fastSliceByte {
			*(*[]byte)(ptr) = b
			d.depth--
			return nil
		}
		return InvalidTypeError{Off: d.r.offset(), FieldType: f.typ.Elem(), Field: f.name, TagType: listType}
	case tagInt32:
		b, err := d.readInt32Slice()
		if err != nil {
			return BufferOverrunError{Op: "Int32Slice"}
		}
		if f.fastKind == fastSliceInt32 {
			*(*[]int32)(ptr) = b
			d.depth--
			return nil
		}
		return InvalidTypeError{Off: d.r.offset(), FieldType: f.typ.Elem(), Field: f.name, TagType: listType}
	case tagInt64:
		b, err := d.readInt64Slice()
		if err != nil {
			return BufferOverrunError{Op: "Int64Slice"}
		}
		if f.fastKind == fastSliceInt64 {
			*(*[]int64)(ptr) = b
			d.depth--
			return nil
		}
		return InvalidTypeError{Off: d.r.offset(), FieldType: f.typ.Elem(), Field: f.name, TagType: listType}
	default:
		if err := d.decodeFastSlicePayload(ptr, f, listType); err != nil {
			return err
		}
		d.depth--
		return nil
	}
}

func setEmptyFastSlice(ptr unsafe.Pointer, f *structField) {
	switch f.fastKind {
	case fastSliceByte:
		*(*[]byte)(ptr) = []byte{}
	case fastSliceBool:
		*(*[]bool)(ptr) = []bool{}
	case fastSliceInt16:
		*(*[]int16)(ptr) = []int16{}
	case fastSliceInt32:
		*(*[]int32)(ptr) = []int32{}
	case fastSliceInt64:
		*(*[]int64)(ptr) = []int64{}
	case fastSliceFloat32:
		*(*[]float32)(ptr) = []float32{}
	case fastSliceFloat64:
		*(*[]float64)(ptr) = []float64{}
	case fastSliceString:
		*(*[]string)(ptr) = []string{}
	case fastSliceAny:
		*(*[]any)(ptr) = []any{}
	case fastSliceMapAny:
		*(*[]map[string]any)(ptr) = []map[string]any{}
	default:
		reflect.NewAt(f.typ, ptr).Elem().Set(reflect.MakeSlice(f.typ, 0, 0))
	}
}

func (d *Decoder) decodeFastSlicePayload(ptr unsafe.Pointer, f *structField, listType tagType) error {
	length, err := d.readInt32()
	if err != nil {
		return err
	}
	n, err := d.r.capacity(int64(length))
	if err != nil {
		return BufferOverrunError{Op: "Slice"}
	}
	if length == 0 {
		setEmptyFastSlice(ptr, f)
		return nil
	}
	switch f.fastKind {
	case fastSliceAny:
		s := make([]any, 0, n)
		for range length {
			v, err := d.unmarshalAny(listType)
			if err != nil {
				return err
			}
			s = append(s, v)
		}
		*(*[]any)(ptr) = s
		return nil
	case fastSliceStruct:
		if listType == tagStruct {
			return d.decodeFastStructSlice(ptr, f, int(length), n)
		}
	case fastSliceMapAny:
		if listType == tagStruct {
			s := make([]map[string]any, 0, n)
			for i := range int(length) {
				d.depth++
				m, err := d.unmarshalCompoundAny()
				if err != nil {
					return wrapListInvalidType(err, d.r.offset(), f.typ.Elem(), f.name, i, listType)
				}
				d.depth--
				s = append(s, m)
			}
			*(*[]map[string]any)(ptr) = s
			return nil
		}
	default:
		if handled, err := d.decodeFastScalarSlicePayload(ptr, f, listType, length, n); handled {
			return err
		}
	}
	// Mismatched element tag type (or rare []bool with non-byte tag): use reflect loop to produce exact error.
	elem := reflect.New(f.typ.Elem()).Elem()
	err = d.unmarshalTag(elem, listType, "")
	return wrapListInvalidType(err, d.r.offset(), f.typ.Elem(), f.name, 0, listType)
}

func (d *Decoder) decodeFastScalarSlicePayload(ptr unsafe.Pointer, f *structField, listType tagType, length int32, n int) (bool, error) {
	switch f.fastKind {
	case fastSliceFloat32:
		if listType == tagFloat32 {
			s := make([]float32, 0, n)
			for range length {
				v, err := d.readFloat32()
				if err != nil {
					return true, err
				}
				s = append(s, v)
			}
			*(*[]float32)(ptr) = s
			return true, nil
		}
	case fastSliceFloat64:
		if listType == tagFloat64 {
			s := make([]float64, 0, n)
			for range length {
				v, err := d.readFloat64()
				if err != nil {
					return true, err
				}
				s = append(s, v)
			}
			*(*[]float64)(ptr) = s
			return true, nil
		}
	case fastSliceInt16:
		if listType == tagInt16 {
			s := make([]int16, 0, n)
			for range length {
				v, err := d.readInt16()
				if err != nil {
					return true, err
				}
				s = append(s, v)
			}
			*(*[]int16)(ptr) = s
			return true, nil
		}
	case fastSliceString:
		if listType == tagString {
			s := make([]string, 0, n)
			for range length {
				v, err := d.readString()
				if err != nil {
					return true, err
				}
				s = append(s, v)
			}
			*(*[]string)(ptr) = s
			return true, nil
		}
	}
	return false, nil
}

func (d *Decoder) decodeFastStructSlice(ptr unsafe.Pointer, f *structField, length, capN int) error {
	dst := reflect.NewAt(f.typ, ptr).Elem()
	dst.SetZero()
	elemInfo := f.loadSubInfo(f.elemType)
	if capN == length && length > 0 {
		dst.Grow(capN)
		dst.SetLen(capN)
		elemBase := dst.Index(0).Addr().UnsafePointer()
		for i := 0; i < length; i++ {
			d.depth++
			if err := d.decodeStructPtr(unsafe.Add(elemBase, uintptr(i)*f.elemSize), elemInfo, ""); err != nil {
				return wrapListInvalidType(err, d.r.offset(), f.elemType, f.name, i, tagStruct)
			}
			d.depth--
		}
		return nil
	}
	if capN > 0 {
		dst.Grow(capN)
		dst.SetLen(capN)
	}
	for i := 0; i < length; i++ {
		if i == dst.Len() {
			dst.Grow(1)
			dst.SetLen(i + 1)
		}
		d.depth++
		if err := d.decodeStructPtr(dst.Index(i).Addr().UnsafePointer(), elemInfo, ""); err != nil {
			return wrapListInvalidType(err, d.r.offset(), f.elemType, f.name, i, tagStruct)
		}
		d.depth--
	}
	return nil
}

// unmarshalTag decodes a tag from the decoder's input stream into the reflect.Value passed, assuming the tag
// has the type and name passed.
func (d *Decoder) unmarshalTag(val reflect.Value, t tagType, tagName string) error {
	k := val.Kind()
	switch t {
	default:
		return UnknownTagError{Off: d.r.offset(), TagType: t, Op: "Match"}
	case tagEnd:
		return UnexpectedTagError{Off: d.r.offset(), TagType: tagEnd}
	case tagByte:
		value, err := d.r.readByte()
		if err != nil {
			return BufferOverrunError{Op: "Byte"}
		}
		switch {
		case k == reflect.Uint8:
			val.SetUint(uint64(value))
		case k == reflect.Bool:
			val.SetBool(value == 1)
		case isAny(val):
			val.Set(reflect.ValueOf(value))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagInt16:
		value, err := d.readInt16()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Int16:
			val.SetInt(int64(value))
		case isAny(val):
			val.Set(reflect.ValueOf(boxInt16(value)))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagInt32:
		value, err := d.readInt32()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Int32:
			val.SetInt(int64(value))
		case isAny(val):
			val.Set(reflect.ValueOf(boxInt32(value)))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagInt64:
		value, err := d.readInt64()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Int64:
			val.SetInt(value)
		case isAny(val):
			val.Set(reflect.ValueOf(boxInt64(value)))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagFloat32, tagFloat64, tagString:
		return d.unmarshalFloatOrStringTag(val, t, tagName)
	case tagByteArray, tagInt32Array, tagInt64Array:
		return d.unmarshalArrayTag(val, t, tagName)
	case tagSlice:
		return d.unmarshalSliceTag(val, tagName)
	case tagStruct:
		return d.unmarshalCompoundTag(val, tagName)
	}
	return nil
}

func (d *Decoder) unmarshalFloatOrStringTag(val reflect.Value, t tagType, tagName string) error {
	k := val.Kind()
	switch t {
	case tagFloat32:
		value, err := d.readFloat32()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Float32:
			val.SetFloat(float64(value))
		case isAny(val):
			val.Set(reflect.ValueOf(boxFloat32(value)))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagFloat64:
		value, err := d.readFloat64()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Float64:
			val.SetFloat(value)
		case isAny(val):
			val.Set(reflect.ValueOf(boxFloat64(value)))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	case tagString:
		value, err := d.readString()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.String:
			val.SetString(value)
		case isAny(val):
			val.Set(reflect.ValueOf(value))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
	}
	return nil
}

func (d *Decoder) unmarshalArrayTag(val reflect.Value, t tagType, tagName string) error {
	k := val.Kind()
	switch t {
	case tagByteArray:
		length, err := d.readInt32()
		if err != nil {
			return err
		}
		b, err := d.r.fixed(int64(length))
		if err != nil {
			return BufferOverrunError{Op: "ByteArray"}
		}
		switch {
		case k == reflect.Array && val.Type().Elem() == byteType:
			if val.Cap() != int(length) {
				return InvalidArraySizeError{Off: d.r.offset(), Op: "ByteArray", GoLength: val.Cap(), NBTLength: int(length)}
			}
			if val.CanAddr() {
				copy(unsafe.Slice((*byte)(val.Addr().UnsafePointer()), val.Cap()), b)
				return nil
			}
		case isAny(val):
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
		value := reflect.New(reflect.ArrayOf(int(length), byteType)).Elem()
		reflect.Copy(value, reflect.ValueOf(b))
		val.Set(value)
	case tagInt32Array:
		s, err := d.readInt32Slice()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Array && val.Type().Elem() == int32Type:
			if val.Cap() != len(s) {
				return InvalidArraySizeError{Off: d.r.offset(), Op: "Int32Array", GoLength: val.Cap(), NBTLength: len(s)}
			}
			if val.CanAddr() {
				copy(unsafe.Slice((*int32)(val.Addr().UnsafePointer()), val.Cap()), s)
				return nil
			}
		case isAny(val):
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
		value := reflect.New(reflect.ArrayOf(len(s), int32Type)).Elem()
		reflect.Copy(value, reflect.ValueOf(s))
		val.Set(value)
	case tagInt64Array:
		s, err := d.readInt64Slice()
		if err != nil {
			return err
		}
		switch {
		case k == reflect.Array && val.Type().Elem() == int64Type:
			if val.Cap() != len(s) {
				return InvalidArraySizeError{Off: d.r.offset(), Op: "Int64Array", GoLength: val.Cap(), NBTLength: len(s)}
			}
			if val.CanAddr() {
				copy(unsafe.Slice((*int64)(val.Addr().UnsafePointer()), val.Cap()), s)
				return nil
			}
		case isAny(val):
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: t}
		}
		value := reflect.New(reflect.ArrayOf(len(s), int64Type)).Elem()
		reflect.Copy(value, reflect.ValueOf(s))
		val.Set(value)
	}
	return nil
}

func (d *Decoder) unmarshalSliceTag(val reflect.Value, tagName string) error {
	if d.depth >= maximumNestingDepth {
		return MaximumDepthReachedError{}
	}
	d.depth++
	listTypeByte, err := d.r.readByte()
	if err != nil {
		return BufferOverrunError{Op: "Slice"}
	}
	listType := tagType(listTypeByte)
	if !listType.IsValid() {
		return UnknownTagError{Off: d.r.offset(), TagType: listType, Op: "Slice"}
	}
	k := val.Kind()
	sliceType := val.Type()
	if k != reflect.Slice && !isAny(val) {
		return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: tagSlice}
	}
	if k == reflect.Interface {
		sliceType = sliceAnyType
	}
	switch listType {
	case tagByte:
		length, err := d.readInt32()
		if err != nil || length < 0 {
			return BufferOverrunError{Op: "ByteSlice"}
		}
		if length == 0 {
			val.Set(reflect.MakeSlice(sliceType, 0, 0))
			break
		}
		b, err := d.r.fixed(int64(length))
		if err != nil {
			return BufferOverrunError{Op: "ByteSlice"}
		}
		switch {
		case k == reflect.Slice && val.Type().Elem() == byteType, isAny(val):
			val.Set(reflect.ValueOf(b))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type().Elem(), Field: tagName, TagType: listType}
		}
	case tagInt32:
		b, err := d.readInt32Slice()
		if err != nil {
			return BufferOverrunError{Op: "Int32Slice"}
		}
		switch {
		case k == reflect.Slice && val.Type().Elem() == int32Type, isAny(val):
			val.Set(reflect.ValueOf(b))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type().Elem(), Field: tagName, TagType: listType}
		}
	case tagInt64:
		b, err := d.readInt64Slice()
		if err != nil {
			return BufferOverrunError{Op: "Int64Slice"}
		}
		switch {
		case k == reflect.Slice && val.Type().Elem() == int64Type, isAny(val):
			val.Set(reflect.ValueOf(b))
		default:
			return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type().Elem(), Field: tagName, TagType: listType}
		}
	default:
		length, err := d.readInt32()
		if err != nil {
			return err
		}
		n, err := d.r.capacity(int64(length))
		if err != nil {
			return BufferOverrunError{Op: "Slice"}
		}
		v := reflect.MakeSlice(sliceType, n, n)
		for i := 0; i < int(length); i++ {
			if i == v.Len() {
				v = reflect.Append(v, reflect.Zero(sliceType.Elem()))
			}
			if err := d.unmarshalTag(v.Index(i), listType, ""); err != nil {
				return wrapListInvalidType(err, d.r.offset(), sliceType.Elem(), tagName, i, listType)
			}
		}
		val.Set(v)
	}
	d.depth--
	return nil
}

func (d *Decoder) unmarshalCompoundTag(val reflect.Value, tagName string) error {
	d.depth++
	switch val.Kind() {
	default:
		return InvalidTypeError{Off: d.r.offset(), FieldType: val.Type(), Field: tagName, TagType: tagStruct}
	case reflect.Struct:
		info := getStructInfo(val.Type())
		if val.CanAddr() {
			if err := d.decodeStructPtr(val.Addr().UnsafePointer(), info, tagName); err != nil {
				return err
			}
		} else {
			for {
				nestedTagType, nameBytes, err := d.tagBytes()
				if err != nil {
					return err
				}
				if nestedTagType == tagEnd {
					break
				}
				if !nestedTagType.IsValid() {
					return UnknownTagError{Off: d.r.offset(), Op: "Struct", TagType: nestedTagType}
				}
				f, ok := info.decodeByName[string(nameBytes)]
				if !ok {
					return UnexpectedNamedTagError{Off: d.r.offset(), TagName: tagName + "." + string(nameBytes), TagType: nestedTagType}
				}
				if err := d.unmarshalTag(f.fieldValue(val), nestedTagType, f.name); err != nil {
					return err
				}
			}
		}
	case reflect.Interface, reflect.Map:
		valKind := val.Kind()
		valType := val.Type()
		if valKind == reflect.Interface && val.NumMethod() != 0 {
			return InvalidTypeError{Off: d.r.offset(), FieldType: valType, Field: tagName, TagType: tagStruct}
		}
		if (valKind == reflect.Map && valType == mapStringAnyType) || (valKind == reflect.Interface && isAny(val)) {
			m, err := d.unmarshalCompoundAny()
			if err != nil {
				return err
			}
			val.Set(reflect.ValueOf(m))
			break
		}
		if valKind == reflect.Map {
			if valType.Key() != stringType {
				return InvalidTypeError{Off: d.r.offset(), FieldType: valType, Field: tagName, TagType: tagStruct}
			}
			valType = valType.Elem()
		}
		m := reflect.MakeMap(reflect.MapOf(stringType, valType))
		for {
			nestedTagType, nestedTagName, err := d.tag()
			if err != nil {
				return err
			}
			if !nestedTagType.IsValid() {
				return UnknownTagError{Off: d.r.offset(), Op: "Map", TagType: nestedTagType}
			}
			if nestedTagType == tagEnd {
				break
			}
			value := reflect.New(valType).Elem()
			if err := d.unmarshalTag(value, nestedTagType, nestedTagName); err != nil {
				return err
			}
			m.SetMapIndex(reflect.ValueOf(nestedTagName), value)
		}
		val.Set(m)
	}
	d.depth--
	return nil
}
