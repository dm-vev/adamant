// Package javanbt parses Java 26.3 SNBT into native Go NBT values.
package javanbt

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

// ParseCompoundArgument parses one compound at a UTF-8 byte offset. Like Java's
// TagParser.parseCompoundAsArgument, it does not require or consume trailing input.
// Bytes use uint8, matching the native NBT codec; their bits retain Java's signed value.
func ParseCompoundArgument(input string, offset int) (map[string]any, int, error) {
	if offset < 0 || offset > len(input) || offset < len(input) && !utf8.RuneStart(input[offset]) {
		return nil, offset, fmt.Errorf("invalid SNBT byte offset %d", offset)
	}
	p := parser{s: input, i: offset}
	value, err := p.compound()
	return value, p.i, err
}

type parser struct {
	s string
	i int
}

func (p *parser) err(message string) error { return fmt.Errorf("SNBT at byte %d: %s", p.i, message) }

func (p *parser) space() {
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if !java26.IsWhitespace(r) {
			return
		}
		p.i += n
	}
}

// Grammar terminals skip Java whitespace, even inside a numeric literal.
// A failed optional terminal restores its cursor, including skipped whitespace.
func (p *parser) take(chars string) byte {
	start := p.i
	p.space()
	if p.i < len(p.s) && strings.IndexByte(chars, p.s[p.i]) >= 0 {
		c := p.s[p.i]
		p.i++
		return c
	}
	p.i = start
	return 0
}

func unquoted(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '-' || c == '.' || c == '+'
}

func numberStart(c byte) bool { return c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.' }

func (p *parser) word() (string, error) {
	p.space()
	start := p.i
	for p.i < len(p.s) && unquoted(p.s[p.i]) {
		p.i++
	}
	if p.i == start {
		return "", p.err("expected unquoted string")
	}
	return p.s[start:p.i], nil
}

func (p *parser) compound() (map[string]any, error) {
	if p.take("{") == 0 {
		return nil, p.err("expected compound")
	}
	result := make(map[string]any)
	if p.take("}") != 0 {
		return result, nil
	}
	for {
		p.space()
		var key string
		var err error
		if p.i < len(p.s) && (p.s[p.i] == '\'' || p.s[p.i] == '"') {
			key, err = p.quoted()
		} else {
			key, err = p.word()
		}
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, p.err("empty compound key")
		}
		if p.take(":") == 0 {
			return nil, p.err("expected colon")
		}
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		result[key] = value
		if p.take("}") != 0 {
			return result, nil
		}
		if p.take(",") == 0 {
			return nil, p.err("expected comma or closing brace")
		}
		if p.take("}") != 0 {
			return result, nil
		}
	}
}

func (p *parser) value() (any, error) {
	p.space()
	if p.i == len(p.s) {
		return nil, p.err("expected value")
	}
	switch p.s[p.i] {
	case '{':
		return p.compound()
	case '[':
		return p.list()
	case '\'', '"':
		return p.quoted()
	}
	if numberStart(p.s[p.i]) {
		if value, ok, err := p.floating(); ok {
			return value, err
		}
		literal, err := p.integer()
		if err != nil {
			return nil, err
		}
		return literal.value('i')
	}
	word, err := p.word()
	if err != nil {
		return nil, err
	}
	if p.take("(") != 0 {
		return p.operation(word)
	}
	if strings.EqualFold(word, "true") {
		return uint8(1), nil
	}
	if strings.EqualFold(word, "false") {
		return uint8(0), nil
	}
	return word, nil
}

func (p *parser) list() (any, error) {
	p.i++ // Opening bracket, already identified by value.
	start := p.i
	if kind := p.take("BIL"); kind != 0 && p.take(";") != 0 {
		return p.array(kind)
	}
	p.i = start
	result := []any{}
	if p.take("]") != 0 {
		return result, nil
	}
	for {
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		if p.take("]") != 0 {
			return result, nil
		}
		if p.take(",") == 0 {
			return nil, p.err("expected comma or closing bracket")
		}
		if p.take("]") != 0 {
			return result, nil
		}
	}
}

func (p *parser) array(kind byte) (any, error) {
	bytes, ints, longs := []byte{}, []int32{}, []int64{}
	if p.take("]") == 0 {
		for {
			literal, err := p.integer()
			if err != nil {
				return nil, err
			}
			if literal.kind != 0 && (kind == 'B' && literal.kind != 'b' || kind == 'I' && literal.kind == 'l') {
				return nil, p.err("invalid typed-array element type")
			}
			value, err := literal.value(kind + ('a' - 'A'))
			if err != nil {
				return nil, err
			}
			var n int64
			switch value := value.(type) {
			case uint8:
				n = int64(int8(value))
			case int16:
				n = int64(value)
			case int32:
				n = int64(value)
			case int64:
				n = value
			}
			switch kind {
			case 'B':
				bytes = append(bytes, byte(n))
			case 'I':
				ints = append(ints, int32(n))
			case 'L':
				longs = append(longs, n)
			}
			if p.take("]") != 0 {
				break
			}
			if p.take(",") == 0 {
				return nil, p.err("expected comma or closing bracket")
			}
			if p.take("]") != 0 {
				break
			}
		}
	}
	switch kind {
	case 'B':
		return bytes, nil
	case 'I':
		return ints, nil
	default:
		return longs, nil
	}
}

func (p *parser) operation(name string) (any, error) {
	var argument any
	count := 0
	if p.take(")") == 0 {
		for {
			value, err := p.value()
			if err != nil {
				return nil, err
			}
			if count == 0 {
				argument = value
			}
			count++
			if p.take(")") != 0 {
				break
			}
			if p.take(",") == 0 {
				return nil, p.err("expected comma or closing parenthesis")
			}
			if p.take(")") != 0 {
				break
			}
		}
	}
	if count != 1 {
		return nil, p.err("unknown operation arity")
	}
	switch name {
	case "bool":
		var nonzero bool
		switch n := argument.(type) {
		case uint8:
			nonzero = n != 0
		case int16:
			nonzero = n != 0
		case int32:
			nonzero = n != 0
		case int64:
			nonzero = n != 0
		case float32:
			nonzero = n != 0
		case float64:
			nonzero = n != 0
		default:
			return nil, p.err("bool requires a number or boolean")
		}
		if nonzero {
			return uint8(1), nil
		}
		return uint8(0), nil
	case "uuid":
		text, ok := argument.(string)
		if !ok {
			return nil, p.err("uuid requires a string")
		}
		return parseUUID(text)
	default:
		return nil, p.err("unknown operation " + name)
	}
}

// UUID.fromString also accepts short/oversized groups, leading plus signs, and
// BMP hexadecimal digits. Its slow path masks the five parsed signed longs.
func parseUUID(text string) ([]int32, error) {
	if utf8.RuneCountInString(text) > 36 {
		return nil, fmt.Errorf("UUID string too large")
	}
	var values [5]uint64
	for i := range values {
		part := text
		if i == 4 && strings.IndexByte(part, '-') >= 0 {
			return nil, fmt.Errorf("invalid UUID groups")
		}
		if i < 4 {
			end := strings.IndexByte(text, '-')
			if end < 0 {
				return nil, fmt.Errorf("invalid UUID groups")
			}
			part, text = text[:end], text[end+1:]
		}
		n, err := java26.ParseSignedInteger(part, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID group: %w", err)
		}
		values[i] = uint64(n)
	}
	most := (values[0]&0xffffffff)<<32 | (values[1]&0xffff)<<16 | values[2]&0xffff
	least := (values[3]&0xffff)<<48 | values[4]&0xffffffffffff
	return []int32{int32(most >> 32), int32(most), int32(least >> 32), int32(least)}, nil
}
