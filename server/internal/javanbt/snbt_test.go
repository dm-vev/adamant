package javanbt

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestParseCompoundArgumentMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_snbt.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input    string
		Offset, Cursor int
		OK             bool
		Value          json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, cursor, err := ParseCompoundArgument(tc.Input, tc.Offset)
			if !tc.OK {
				if err == nil {
					t.Fatalf("source rejects %q; got %#v", tc.Input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("source accepts %q: %v", tc.Input, err)
			}
			if cursor != tc.Cursor {
				t.Fatalf("cursor=%d, source=%d for %q", cursor, tc.Cursor, tc.Input)
			}
			encoded, err := json.Marshal(oracleTag(t, got))
			if err != nil {
				t.Fatal(err)
			}
			var expected any
			if err := json.Unmarshal(tc.Value, &expected); err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, want) {
				t.Fatalf("input=%q\ngot %s\nsource %s", tc.Input, encoded, want)
			}
		})
	}
}

// The source fixture records tag IDs, exact numeric bits and UTF-16 string units;
// ordinary JSON numbers/strings would erase long precision and lone surrogates.
func oracleTag(t *testing.T, value any) any {
	t.Helper()
	switch v := value.(type) {
	case uint8:
		return []any{1, strconv.FormatInt(int64(int8(v)), 10)}
	case int16:
		return []any{2, strconv.FormatInt(int64(v), 10)}
	case int32:
		return []any{3, strconv.FormatInt(int64(v), 10)}
	case int64:
		return []any{4, strconv.FormatInt(v, 10)}
	case float32:
		return []any{5, strconv.FormatUint(uint64(math.Float32bits(v)), 16)}
	case float64:
		return []any{6, strconv.FormatUint(math.Float64bits(v), 16)}
	case []byte:
		values := make([]string, len(v))
		for i, n := range v {
			values[i] = strconv.FormatInt(int64(int8(n)), 10)
		}
		return []any{7, values}
	case string:
		return []any{8, oracleUTF16(v)}
	case []any:
		values := make([]any, len(v))
		for i, n := range v {
			values[i] = oracleTag(t, n)
		}
		return []any{9, values}
	case map[string]any:
		values := make(map[string]any, len(v))
		for key, n := range v {
			values[key] = oracleTag(t, n)
		}
		return []any{10, values}
	case []int32:
		values := make([]string, len(v))
		for i, n := range v {
			values[i] = strconv.FormatInt(int64(n), 10)
		}
		return []any{11, values}
	case []int64:
		values := make([]string, len(v))
		for i, n := range v {
			values[i] = strconv.FormatInt(n, 10)
		}
		return []any{12, values}
	default:
		t.Fatalf("unexpected native NBT value %T", value)
		return nil
	}
}

func oracleUTF16(text string) []uint16 {
	units := []uint16{}
	for len(text) > 0 {
		if len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2] >= 0x80 && text[2] <= 0xbf {
			units = append(units, uint16(text[0]&0xf)<<12|uint16(text[1]&0x3f)<<6|uint16(text[2]&0x3f))
			text = text[3:]
			continue
		}
		r, n := utf8.DecodeRuneInString(text)
		text = text[n:]
		if r > 0xffff {
			high, low := utf16.EncodeRune(r)
			units = append(units, uint16(high), uint16(low))
		} else {
			units = append(units, uint16(r))
		}
	}
	return units
}
