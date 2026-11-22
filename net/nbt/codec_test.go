package nbt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type plainWriter struct{ w io.Writer }

func (p plainWriter) Write(b []byte) (int, error) { return p.w.Write(b) }

type unexportedEmbedded struct {
	Hidden int32 `nbt:"hidden"`
}

type IgnoredEmbedded struct {
	IgnoredVal int32 `nbt:"ignored_val"`
}

type BaseEmbedded struct {
	BaseID int32 `nbt:"base_id"`
}

type MidEmbedded struct {
	BaseEmbedded
	MidName string `nbt:"mid_name"`
}

type AnonScalar int32

type fullTestStruct struct {
	MidEmbedded
	unexportedEmbedded
	IgnoredEmbedded `nbt:"-"`
	AnonScalar      `nbt:"anon_scalar"`

	ByteVal    byte             `nbt:"byte_val"`
	BoolVal    bool             `nbt:"bool_val"`
	ShortVal   int16            `nbt:"short_val"`
	IntVal     int32            `nbt:"int_val"`
	LongVal    int64            `nbt:"long_val"`
	FloatVal   float32          `nbt:"float_val"`
	DoubleVal  float64          `nbt:"double_val"`
	StringVal  string           `nbt:"string_val"`
	ByteArr    [16]byte         `nbt:"byte_arr"`
	SmallArr   [3]byte          `nbt:"small_arr"`
	IntArr     [4]int32         `nbt:"int_arr"`
	LongArr    [2]int64         `nbt:"long_arr"`
	ByteSlice  []byte           `nbt:"byte_slice"`
	Int16Slice []int16          `nbt:"int16_slice"`
	Int32Slice []int32          `nbt:"int32_slice"`
	Int64Slice []int64          `nbt:"int64_slice"`
	F32Slice   []float32        `nbt:"f32_slice"`
	F64Slice   []float64        `nbt:"f64_slice"`
	StrSlice   []string         `nbt:"str_slice"`
	AnySlice   []any            `nbt:"any_slice"`
	MapSlice   []map[string]any `nbt:"map_slice"`
	MapField   map[string]any   `nbt:"map_field"`
	TypedMap   map[string]int32 `nbt:"typed_map"`
	AnyField   any              `nbt:"any_field"`
	Sub        subTestStruct    `nbt:"sub"`
	Subs       []subTestStruct  `nbt:"subs"`
	Ignored    string           `nbt:"-"`
	unexported string
}

type subTestStruct struct {
	K string `nbt:"k"`
	V int16  `nbt:"v"`
}

type omitTestStruct struct {
	B         byte            `nbt:"b,omitempty"`
	Flag      bool            `nbt:"flag,omitempty"`
	I16       int16           `nbt:"i16,omitempty"`
	I32       int32           `nbt:"i32,omitempty"`
	I64       int64           `nbt:"i64,omitempty"`
	F32       float32         `nbt:"f32,omitempty"`
	F64       float64         `nbt:"f64,omitempty"`
	Str       string          `nbt:"str,omitempty"`
	NilSlice  []int32         `nbt:"nil_slice,omitempty"`
	ZeroSlice []int32         `nbt:"zero_slice,omitempty"`
	NilMap    map[string]any  `nbt:"nil_map,omitempty"`
	ZeroMap   map[string]any  `nbt:"zero_map,omitempty"`
	NilAny    any             `nbt:"nil_any,omitempty"`
	ZeroArr   [4]byte         `nbt:"zero_arr,omitempty"`
	SetArr    [4]byte         `nbt:"set_arr,omitempty"`
	ZeroSub   subTestStruct   `nbt:"zero_sub,omitempty"`
	SetSub    subTestStruct   `nbt:"set_sub,omitempty"`
	NilSubs   []subTestStruct `nbt:"nil_subs,omitempty"`
	ZeroSubs  []subTestStruct `nbt:"zero_subs,omitempty"`
}

type treeNode struct {
	Name     string     `nbt:"name"`
	Children []treeNode `nbt:"children,omitempty"`
}

func TestWireFormatGolden(t *testing.T) {
	type sample struct {
		A int32  `nbt:"a"`
		S string `nbt:"s"`
	}
	v := sample{A: -5, S: "hi"}

	// LittleEndian:
	// 0x0a 0x00 0x00 (root compound "")
	// 0x03 0x01 0x00 'a' 0xfb 0xff 0xff 0xff
	// 0x08 0x01 0x00 's' 0x02 0x00 'h' 'i'
	// 0x00
	wantLE := []byte{
		0x0a, 0x00, 0x00,
		0x03, 0x01, 0x00, 'a', 0xfb, 0xff, 0xff, 0xff,
		0x08, 0x01, 0x00, 's', 0x02, 0x00, 'h', 'i',
		0x00,
	}
	gotLE, err := MarshalEncoding(v, LittleEndian)
	if err != nil {
		t.Fatalf("LittleEndian marshal: %v", err)
	}
	if !bytes.Equal(gotLE, wantLE) {
		t.Fatalf("LittleEndian bytes mismatch:\ngot:  %x\nwant: %x", gotLE, wantLE)
	}

	// NetworkLittleEndian: zigzag(-5) = 9
	wantNLE := []byte{
		0x0a, 0x00,
		0x03, 0x01, 'a', 0x09,
		0x08, 0x01, 's', 0x02, 'h', 'i',
		0x00,
	}
	gotNLE, err := MarshalEncoding(v, NetworkLittleEndian)
	if err != nil {
		t.Fatalf("NetworkLittleEndian marshal: %v", err)
	}
	if !bytes.Equal(gotNLE, wantNLE) {
		t.Fatalf("NetworkLittleEndian bytes mismatch:\ngot:  %x\nwant: %x", gotNLE, wantNLE)
	}

	// BigEndian:
	wantBE := []byte{
		0x0a, 0x00, 0x00,
		0x03, 0x00, 0x01, 'a', 0xff, 0xff, 0xff, 0xfb,
		0x08, 0x00, 0x01, 's', 0x00, 0x02, 'h', 'i',
		0x00,
	}
	gotBE, err := MarshalEncoding(v, BigEndian)
	if err != nil {
		t.Fatalf("BigEndian marshal: %v", err)
	}
	if !bytes.Equal(gotBE, wantBE) {
		t.Fatalf("BigEndian bytes mismatch:\ngot:  %x\nwant: %x", gotBE, wantBE)
	}

	// NetworkBigEndian: root compound omits 2-byte empty name
	wantNBE := append([]byte{0x0a}, wantBE[3:]...)
	gotNBE, err := MarshalEncoding(v, NetworkBigEndian)
	if err != nil {
		t.Fatalf("NetworkBigEndian marshal: %v", err)
	}
	if !bytes.Equal(gotNBE, wantNBE) {
		t.Fatalf("NetworkBigEndian bytes mismatch:\ngot:  %x\nwant: %x", gotNBE, wantNBE)
	}

	// Verify pointer marshal produces identical wire bytes as value marshal for all encodings.
	for _, enc := range encodings {
		byVal, err := MarshalEncoding(v, enc)
		if err != nil {
			t.Fatalf("%T byVal: %v", enc, err)
		}
		byPtr, err := MarshalEncoding(&v, enc)
		if err != nil {
			t.Fatalf("%T byPtr: %v", enc, err)
		}
		if !bytes.Equal(byVal, byPtr) {
			t.Fatalf("%T byVal != byPtr: %x vs %x", enc, byVal, byPtr)
		}
	}
}

func sampleFullStruct() fullTestStruct {
	longStr := strings.Repeat("minecraft:villager_", 12) // > 128 bytes (multi-byte varint length)
	return fullTestStruct{
		MidEmbedded:        MidEmbedded{BaseEmbedded: BaseEmbedded{BaseID: 77}, MidName: "mid"},
		unexportedEmbedded: unexportedEmbedded{Hidden: 99},
		IgnoredEmbedded:    IgnoredEmbedded{IgnoredVal: 88},
		AnonScalar:         123,
		ByteVal:            0x7f,
		BoolVal:            true,
		ShortVal:           -12345,
		IntVal:             -987654321,
		LongVal:            math.MinInt64 / 3,
		FloatVal:           -123.5,
		DoubleVal:          3.141592653589793,
		StringVal:          longStr,
		ByteArr:            [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SmallArr:           [3]byte{9, 8, 7},
		IntArr:             [4]int32{-1, 0, 1, math.MaxInt32},
		LongArr:            [2]int64{math.MinInt64, math.MaxInt64},
		ByteSlice:          []byte{0, 1, 2, 255},
		Int16Slice:         []int16{-3, 0, 7, 300},
		Int32Slice:         []int32{-100, 0, 42, 100000},
		Int64Slice:         []int64{-500, 0, 9999999999},
		F32Slice:           []float32{0, 1, -2.5},
		F64Slice:           []float64{0, 1, -9.25},
		StrSlice:           []string{"", "a", "minecraft:stone", longStr},
		AnySlice:           []any{map[string]any{"x": int32(1)}, map[string]any{"x": int32(2)}},
		MapSlice:           []map[string]any{{"k": "v1"}, {"k": "v2"}},
		MapField:           map[string]any{"nested": int16(5), "empty": []any{}, "negZero": float32(math.Copysign(0, -1))},
		TypedMap:           map[string]int32{"one": 1, "two": -2},
		AnyField:           [5]int32{10, 20, 30, 40, 50},
		Sub:                subTestStruct{K: "subkey", V: 42},
		Subs:               []subTestStruct{{K: "s0", V: 1}, {K: "s1", V: -2}},
		Ignored:            "should-not-appear",
		unexported:         "private",
	}
}

func TestRoundTripStructAndStreams(t *testing.T) {
	orig := sampleFullStruct()
	expected := orig
	expected.unexportedEmbedded = unexportedEmbedded{}
	expected.IgnoredEmbedded = IgnoredEmbedded{}
	expected.Ignored = ""
	expected.unexported = ""

	for _, enc := range encodings {
		// 1. Marshal by pointer and by value; verify identical bytes.
		bPtr, err := MarshalEncoding(&orig, enc)
		if err != nil {
			t.Fatalf("%T marshal ptr: %v", enc, err)
		}
		bVal, err := MarshalEncoding(orig, enc)
		if err != nil {
			t.Fatalf("%T marshal val: %v", enc, err)
		}
		var fromPtr, fromVal fullTestStruct
		if err := UnmarshalEncoding(bPtr, &fromPtr, enc); err != nil {
			t.Fatalf("%T unmarshal ptr: %v", enc, err)
		}
		if err := UnmarshalEncoding(bVal, &fromVal, enc); err != nil {
			t.Fatalf("%T unmarshal val: %v", enc, err)
		}
		if !reflect.DeepEqual(fromPtr, expected) {
			t.Fatalf("%T ptr roundtrip mismatch:\ngot:  %+v\nwant: %+v", enc, fromPtr, expected)
		}
		if !reflect.DeepEqual(fromVal, expected) {
			t.Fatalf("%T val roundtrip mismatch", enc)
		}
		// Verify negative zero float32 bit pattern preserved inside MapField.
		negZero := fromPtr.MapField["negZero"].(float32)
		if math.Float32bits(negZero) != math.Float32bits(float32(math.Copysign(0, -1))) {
			t.Fatalf("%T negative zero bits lost: %x", enc, math.Float32bits(negZero))
		}

		// 2. Encode & decode via *bytes.Buffer (sequential multi-object).
		var buf bytes.Buffer
		w := NewEncoderWithEncoding(&buf, enc)
		if err := w.Encode(&orig); err != nil {
			t.Fatalf("%T buf encode 1: %v", enc, err)
		}
		if err := w.Encode(orig.MapField); err != nil {
			t.Fatalf("%T buf encode 2: %v", enc, err)
		}
		r := NewDecoderWithEncoding(&buf, enc)
		var dec1 fullTestStruct
		var dec2 map[string]any
		if err := r.Decode(&dec1); err != nil {
			t.Fatalf("%T buf decode 1: %v", enc, err)
		}
		if err := r.Decode(&dec2); err != nil {
			t.Fatalf("%T buf decode 2: %v", enc, err)
		}
		if !reflect.DeepEqual(dec1, expected) || !reflect.DeepEqual(dec2, orig.MapField) {
			t.Fatalf("%T sequential buffer roundtrip mismatch", enc)
		}

		// 3. Encode & decode via plain io.Writer / io.Reader (no ByteWriter/ByteReader/Len).
		var streamBuf bytes.Buffer
		sw := NewEncoderWithEncoding(plainWriter{&streamBuf}, enc)
		if err := sw.Encode(&orig); err != nil {
			t.Fatalf("%T stream encode: %v", enc, err)
		}
		var decStream fullTestStruct
		sr := NewDecoderWithEncoding(stream{bytes.NewReader(streamBuf.Bytes())}, enc)
		if err := sr.Decode(&decStream); err != nil {
			t.Fatalf("%T stream decode: %v", enc, err)
		}
		if !reflect.DeepEqual(decStream, expected) {
			t.Fatalf("%T stream roundtrip mismatch", enc)
		}
	}
}

func TestOmitEmptyAndRecursiveStructs(t *testing.T) {
	v := omitTestStruct{
		ZeroSlice: []int32{},
		ZeroMap:   map[string]any{},
		SetArr:    [4]byte{0, 0, 1, 0},
		SetSub:    subTestStruct{K: "ok"},
		ZeroSubs:  []subTestStruct{},
	}
	for _, enc := range encodings {
		b, err := MarshalEncoding(&v, enc)
		if err != nil {
			t.Fatalf("%T marshal: %v", enc, err)
		}
		var m map[string]any
		if err := UnmarshalEncoding(b, &m, enc); err != nil {
			t.Fatalf("%T unmarshal map: %v", enc, err)
		}
		wantKeys := []string{"zero_slice", "zero_map", "set_arr", "set_sub", "zero_subs"}
		if len(m) != len(wantKeys) {
			t.Fatalf("%T got keys %v, want %v", enc, m, wantKeys)
		}
		for _, k := range wantKeys {
			if _, ok := m[k]; !ok {
				t.Fatalf("%T missing key %q in %v", enc, k, m)
			}
		}

		// Recursive struct roundtrip.
		root := treeNode{
			Name: "root",
			Children: []treeNode{
				{Name: "a", Children: []treeNode{{Name: "a1"}}},
				{Name: "b"},
			},
		}
		tb, err := MarshalEncoding(&root, enc)
		if err != nil {
			t.Fatalf("%T tree marshal: %v", enc, err)
		}
		var gotTree treeNode
		if err := UnmarshalEncoding(tb, &gotTree, enc); err != nil {
			t.Fatalf("%T tree unmarshal: %v", enc, err)
		}
		if !reflect.DeepEqual(gotTree, root) {
			t.Fatalf("%T tree mismatch:\ngot:  %+v\nwant: %+v", enc, gotTree, root)
		}
	}
}

func TestUnmarshalBufferOwnership(t *testing.T) {
	orig := benchEntityMap()
	for _, enc := range encodings {
		raw, err := MarshalEncoding(orig, enc)
		if err != nil {
			t.Fatalf("%T marshal: %v", enc, err)
		}
		var expected map[string]any
		if err := UnmarshalEncoding(raw, &expected, enc); err != nil {
			t.Fatalf("%T unmarshal baseline: %v", enc, err)
		}
		input := append([]byte(nil), raw...)
		var decoded map[string]any
		if err := UnmarshalEncoding(input, &decoded, enc); err != nil {
			t.Fatalf("%T unmarshal: %v", enc, err)
		}
		for i := range input {
			input[i] = 0xff
		}
		if !reflect.DeepEqual(decoded, expected) {
			t.Fatalf("%T decoded map aliased caller input buffer", enc)
		}
	}
}

func TestAllowZero(t *testing.T) {
	for _, enc := range encodings {
		// Disallowed by default.
		var m map[string]any
		err := UnmarshalEncoding([]byte{byte(tagEnd)}, &m, enc)
		var uErr UnexpectedTagError
		if !errors.As(err, &uErr) || uErr.TagType != tagEnd {
			t.Fatalf("%T want UnexpectedTagError(tagEnd), got %v", enc, err)
		}

		// Allowed with AllowZero: nil map becomes non-nil empty map.
		dec := NewDecoderWithEncoding(bytes.NewReader([]byte{byte(tagEnd)}), enc)
		dec.AllowZero = true
		var nilMap map[string]any
		if err := dec.Decode(&nilMap); err != nil {
			t.Fatalf("%T AllowZero nilMap: %v", enc, err)
		}
		if nilMap == nil || len(nilMap) != 0 {
			t.Fatalf("%T AllowZero nilMap = %#v, want empty non-nil map", enc, nilMap)
		}

		// Allowed with AllowZero: populated map is cleared.
		dec2 := NewDecoderWithEncoding(bytes.NewReader([]byte{byte(tagEnd)}), enc)
		dec2.AllowZero = true
		popMap := map[string]any{"k": int32(1)}
		if err := dec2.Decode(&popMap); err != nil {
			t.Fatalf("%T AllowZero popMap: %v", enc, err)
		}
		if len(popMap) != 0 {
			t.Fatalf("%T AllowZero popMap not cleared: %#v", enc, popMap)
		}

		// Allowed with AllowZero: *any and *struct.
		dec3 := NewDecoderWithEncoding(bytes.NewReader([]byte{byte(tagEnd)}), enc)
		dec3.AllowZero = true
		var anyVal any
		if err := dec3.Decode(&anyVal); err != nil || anyVal != nil {
			t.Fatalf("%T AllowZero any: val=%v err=%v", enc, anyVal, err)
		}
		dec4 := NewDecoderWithEncoding(bytes.NewReader([]byte{byte(tagEnd)}), enc)
		dec4.AllowZero = true
		var s subTestStruct
		if err := dec4.Decode(&s); err != nil {
			t.Fatalf("%T AllowZero struct: %v", enc, err)
		}
	}
}

func TestEmptyListsAndByteListSemantics(t *testing.T) {
	for _, enc := range encodings {
		// Empty TAG_List<TAG_Byte> -> any produces []any{}, into []string produces non-nil []string{}.
		b, err := MarshalEncoding(map[string]any{"x": []byte{}}, enc)
		if err != nil {
			t.Fatalf("%T marshal empty byte list: %v", enc, err)
		}
		var m map[string]any
		if err := UnmarshalEncoding(b, &m, enc); err != nil {
			t.Fatalf("%T unmarshal empty byte list into map: %v", enc, err)
		}
		if sl, ok := m["x"].([]any); !ok || len(sl) != 0 {
			t.Fatalf("%T empty byte list into any = %#v (%T), want []any{}", enc, m["x"], m["x"])
		}
		var strHolder struct {
			X []string `nbt:"x"`
		}
		if err := UnmarshalEncoding(b, &strHolder, enc); err != nil {
			t.Fatalf("%T empty byte list into []string: %v", enc, err)
		}
		if strHolder.X == nil || len(strHolder.X) != 0 {
			t.Fatalf("%T empty byte list into []string = %#v, want non-nil empty", enc, strHolder.X)
		}

		// Non-empty TAG_List<TAG_Byte> fails into []any and []bool, succeeds into []byte.
		bNonEmpty, err := MarshalEncoding(map[string]any{"x": []byte{1, 2}}, enc)
		if err != nil {
			t.Fatalf("%T marshal byte list: %v", enc, err)
		}
		var anyHolder struct {
			X []any `nbt:"x"`
		}
		if err := UnmarshalEncoding(bNonEmpty, &anyHolder, enc); !errors.As(err, new(InvalidTypeError)) {
			t.Fatalf("%T non-empty byte list into []any: got %v, want InvalidTypeError", enc, err)
		}
		var boolHolder struct {
			X []bool `nbt:"x"`
		}
		if err := UnmarshalEncoding(bNonEmpty, &boolHolder, enc); !errors.As(err, new(InvalidTypeError)) {
			t.Fatalf("%T non-empty byte list into []bool: got %v, want InvalidTypeError", enc, err)
		}
		var byteHolder struct {
			X []byte `nbt:"x"`
		}
		if err := UnmarshalEncoding(bNonEmpty, &byteHolder, enc); err != nil || !bytes.Equal(byteHolder.X, []byte{1, 2}) {
			t.Fatalf("%T non-empty byte list into []byte: got %v, err=%v", enc, byteHolder.X, err)
		}

		// Empty TAG_List<TAG_Int> still rejects mismatched slice type.
		bEmptyInt, err := MarshalEncoding(map[string]any{"x": []int32{}}, enc)
		if err != nil {
			t.Fatalf("%T marshal empty int32 list: %v", enc, err)
		}
		var strHolder2 struct {
			X []string `nbt:"x"`
		}
		if err := UnmarshalEncoding(bEmptyInt, &strHolder2, enc); !errors.As(err, new(InvalidTypeError)) {
			t.Fatalf("%T empty int32 list into []string: got %v, want InvalidTypeError", enc, err)
		}
	}
}

func TestErrorTypesAndOffsets(t *testing.T) {
	// NonPointerTypeError
	var npErr NonPointerTypeError
	if err := UnmarshalEncoding([]byte{0x0a, 0x00, 0x00, 0x00}, nil, LittleEndian); !errors.As(err, &npErr) {
		t.Fatalf("nil dst: got %v, want NonPointerTypeError", err)
	}
	if err := UnmarshalEncoding([]byte{0x0a, 0x00, 0x00, 0x00}, subTestStruct{}, LittleEndian); !errors.As(err, &npErr) {
		t.Fatalf("non-ptr dst: got %v, want NonPointerTypeError", err)
	}

	// IncompatibleTypeError
	for _, bad := range []any{int(5), map[int]string{1: "a"}, []any{complex64(1)}} {
		if _, err := MarshalEncoding(bad, LittleEndian); !errors.As(err, new(IncompatibleTypeError)) {
			t.Fatalf("marshal %T: got %v, want IncompatibleTypeError", bad, err)
		}
	}

	// UnexpectedNamedTagError with non-zero offset
	raw, _ := MarshalEncoding(map[string]any{"k": "v", "extra": int32(1)}, LittleEndian)
	var sub subTestStruct
	var unErr UnexpectedNamedTagError
	if err := UnmarshalEncoding(raw, &sub, LittleEndian); !errors.As(err, &unErr) || unErr.Off <= 0 {
		t.Fatalf("unexpected field: got %v (off=%d), want UnexpectedNamedTagError with Off>0", err, unErr.Off)
	}

	// InvalidTypeError inside struct list formats field[i].sub
	listRaw, _ := MarshalEncoding(map[string]any{
		"subs": []map[string]any{{"k": "ok", "v": int16(1)}, {"k": "bad", "v": "wrong"}},
	}, LittleEndian)
	var listDst struct {
		Subs []subTestStruct `nbt:"subs"`
	}
	var itErr InvalidTypeError
	if err := UnmarshalEncoding(listRaw, &listDst, LittleEndian); !errors.As(err, &itErr) || itErr.Field != "subs[1].v" || itErr.Off <= 0 {
		t.Fatalf("list element type mismatch: got %v (field=%q off=%d)", err, itErr.Field, itErr.Off)
	}

	// InvalidArraySizeError for byte/int32/int64 arrays
	for _, enc := range encodings {
		bArr, _ := MarshalEncoding(map[string]any{"b": [5]byte{1, 2, 3, 4, 5}}, enc)
		var bDst struct {
			B [4]byte `nbt:"b"`
		}
		var arrErr InvalidArraySizeError
		if err := UnmarshalEncoding(bArr, &bDst, enc); !errors.As(err, &arrErr) || arrErr.GoLength != 4 || arrErr.NBTLength != 5 || arrErr.Off <= 0 {
			t.Fatalf("%T byte array size: got %v", enc, err)
		}

		iArr, _ := MarshalEncoding(map[string]any{"i": [3]int32{1, 2, 3}}, enc)
		var iDst struct {
			I [2]int32 `nbt:"i"`
		}
		if err := UnmarshalEncoding(iArr, &iDst, enc); !errors.As(err, &arrErr) || arrErr.GoLength != 2 || arrErr.NBTLength != 3 || arrErr.Off <= 0 {
			t.Fatalf("%T int32 array size: got %v", enc, err)
		}

		lArr, _ := MarshalEncoding(map[string]any{"l": [3]int64{1, 2, 3}}, enc)
		var lDst struct {
			L [1]int64 `nbt:"l"`
		}
		if err := UnmarshalEncoding(lArr, &lDst, enc); !errors.As(err, &arrErr) || arrErr.GoLength != 1 || arrErr.NBTLength != 3 || arrErr.Off <= 0 {
			t.Fatalf("%T int64 array size: got %v", enc, err)
		}
	}

	// UnknownTagError
	var unkErr UnknownTagError
	if err := UnmarshalEncoding([]byte{0x0a, 0x00, 0x00, 0x7f, 0x00, 0x00}, new(map[string]any), LittleEndian); !errors.As(err, &unkErr) || unkErr.Off <= 0 {
		t.Fatalf("unknown tag: got %v, want UnknownTagError with Off>0", err)
	}

	// InvalidStringError (> maxStringSize)
	tooLong := strings.Repeat("a", maxStringSize+1)
	for _, enc := range encodings {
		var strErr InvalidStringError
		if _, err := MarshalEncoding(map[string]any{"s": tooLong}, enc); !errors.As(err, &strErr) || strErr.Off <= 0 {
			t.Fatalf("%T too long string: got %v (off=%d)", enc, err, strErr.Off)
		}
	}

	// InvalidVarintError on NetworkLittleEndian
	badVarint := []byte{0x0a, 0x00, 0x03, 0x01, 'a', 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	var varErr InvalidVarintError
	if err := UnmarshalEncoding(badVarint, new(map[string]any), NetworkLittleEndian); !errors.As(err, &varErr) || varErr.Off <= 0 {
		t.Fatalf("bad varint: got %v (off=%d), want InvalidVarintError", err, varErr.Off)
	}

	// MaximumBytesReadError on NetworkLittleEndian (> 4 MiB)
	var huge bytes.Buffer
	huge.Write([]byte{0x0a, 0x00})
	// Write a 4 MiB + 16 byte array tag, followed by another tag so d.tag() checks offset >= maximumNetworkOffset.
	huge.Write([]byte{byte(tagByteArray), 0x01, 'b'})
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], uint64((maximumNetworkOffset+16)<<1))
	huge.Write(tmp[:n])
	huge.Write(make([]byte, maximumNetworkOffset+16))
	huge.Write([]byte{byte(tagByte), 0x01, 'c', 0x01, 0x00})
	if err := UnmarshalEncoding(huge.Bytes(), new(map[string]any), NetworkLittleEndian); !errors.As(err, new(MaximumBytesReadError)) {
		t.Fatalf("NetworkLittleEndian >4MiB: got %v, want MaximumBytesReadError", err)
	}
}

func TestConcurrentMarshalUnmarshalRace(t *testing.T) {
	entity := benchEntityMap()
	chest := benchBlockEntityMap()
	st := sampleFullStruct()
	largePayload := map[string]any{
		"blob": strings.Repeat("x", 30000),
		"b2":   strings.Repeat("y", 30000),
		"b3":   strings.Repeat("z", 10000), // exceeds 64 KiB pool cap
	}

	const workers = 12
	var wg sync.WaitGroup
	errCh := make(chan error, workers*4)

	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			enc := encodings[id%len(encodings)]
			for range 25 {
				b1, err := MarshalEncoding(entity, enc)
				if err != nil {
					errCh <- err
					return
				}
				var m1 map[string]any
				if err := UnmarshalEncoding(b1, &m1, enc); err != nil {
					errCh <- err
					return
				}

				b2, err := MarshalEncoding(chest, enc)
				if err != nil {
					errCh <- err
					return
				}
				var m2 map[string]any
				if err := UnmarshalEncoding(b2, &m2, enc); err != nil {
					errCh <- err
					return
				}

				b3, err := MarshalEncoding(&st, enc)
				if err != nil {
					errCh <- err
					return
				}
				var stOut fullTestStruct
				if err := UnmarshalEncoding(b3, &stOut, enc); err != nil {
					errCh <- err
					return
				}

				b4, err := MarshalEncoding(largePayload, enc)
				if err != nil {
					errCh <- err
					return
				}
				var m4 map[string]any
				if err := UnmarshalEncoding(b4, &m4, enc); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
