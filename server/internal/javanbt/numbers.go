package javanbt

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

type integerLiteral struct {
	digits             string
	radix              int
	kind               byte
	negative, unsigned bool
}

func digit(c byte, radix int) bool {
	if c >= '0' && c <= '9' {
		return int(c-'0') < radix
	}
	return radix == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
}

// NumberRunParseRule allows repeated inner underscores, but neither endpoint.
func (p *parser) digits(radix int) (string, bool) {
	mark := p.i
	p.space()
	start := p.i
	for p.i < len(p.s) && (digit(p.s[p.i], radix) || p.s[p.i] == '_') {
		p.i++
	}
	if p.i == start || p.s[start] == '_' || p.s[p.i-1] == '_' {
		p.i = mark
		return "", false
	}
	return p.s[start:p.i], true
}

func (p *parser) integer() (integerLiteral, error) {
	literal := integerLiteral{radix: 10}
	literal.negative = p.take("+-") == '-'
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
		literal.digits = "0"
		if p.take("xX") != 0 {
			literal.radix = 16
			var ok bool
			literal.digits, ok = p.digits(16)
			if !ok {
				return literal, p.err("expected hexadecimal digits")
			}
		} else {
			mark := p.i
			if p.take("bB") != 0 {
				if digits, ok := p.digits(2); ok {
					literal.radix, literal.digits = 2, digits
				} else {
					p.i = mark
				}
			}
			if literal.radix == 10 {
				if _, ok := p.digits(10); ok {
					return literal, p.err("leading zero is not allowed")
				}
			}
		}
	} else {
		var ok bool
		literal.digits, ok = p.digits(10)
		if !ok {
			return literal, p.err("expected integer digits")
		}
	}
	literal.unsigned = literal.radix != 10
	mark := p.i
	if prefix := p.take("uUsS"); prefix != 0 {
		if kind := p.take("bBsSiIlL"); kind != 0 {
			literal.kind = kind | 0x20
			literal.unsigned = prefix == 'u' || prefix == 'U'
			return literal, nil
		}
		p.i = mark
	}
	if kind := p.take("bBsSiIlL"); kind != 0 {
		literal.kind = kind | 0x20
	}
	return literal, nil
}

func (n integerLiteral) value(defaultKind byte) (any, error) {
	kind := n.kind
	if kind == 0 {
		kind = defaultKind
	}
	bits := 32
	switch kind {
	case 'b':
		bits = 8
	case 's':
		bits = 16
	case 'l':
		bits = 64
	}
	if n.unsigned && n.negative {
		return nil, fmt.Errorf("unsigned SNBT integer cannot be negative")
	}
	digits := n.digits
	if strings.IndexByte(digits, '_') >= 0 {
		digits = strings.ReplaceAll(digits, "_", "")
	}
	magnitude, err := strconv.ParseUint(digits, n.radix, bits)
	if err != nil {
		return nil, fmt.Errorf("SNBT integer: %w", err)
	}
	if !n.unsigned {
		limit := uint64(1) << (bits - 1)
		if !n.negative {
			limit--
		}
		if magnitude > limit {
			return nil, fmt.Errorf("SNBT signed integer out of range")
		}
	}
	value := int64(magnitude)
	if n.negative {
		value = -value
	}
	switch kind {
	case 'b':
		return uint8(value), nil
	case 's':
		return int16(value), nil
	case 'i':
		return int32(value), nil
	case 'l':
		return value, nil
	default:
		return nil, fmt.Errorf("invalid SNBT integer type %q", kind)
	}
}

func (p *parser) floating() (any, bool, error) {
	start := p.i
	p.take("+-")
	_, whole := p.digits(10)
	dot := p.take(".") != 0
	fraction := false
	if dot {
		_, fraction = p.digits(10)
	}
	if !whole && (!dot || !fraction) {
		p.i = start
		return nil, false, nil
	}
	exponent := false
	mark := p.i
	if p.take("eE") != 0 {
		p.take("+-")
		if _, ok := p.digits(10); ok {
			exponent = true
		} else {
			p.i = mark
		}
	}
	end := p.i
	suffix := p.take("fFdD")
	if !dot && !exponent && suffix == 0 {
		p.i = start
		return nil, false, nil
	}
	text := p.s[start:end]
	for _, c := range text {
		if c == '_' || java26.IsWhitespace(c) {
			text = strings.Map(func(r rune) rune {
				if r == '_' || java26.IsWhitespace(r) {
					return -1
				}
				return r
			}, text)
			break
		}
	}
	bits := 64
	if suffix == 'f' || suffix == 'F' {
		bits = 32
	}
	value, err := strconv.ParseFloat(text, bits)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return nil, true, p.err("invalid or non-finite floating-point value")
	}
	// FloatTag/DoubleTag.valueOf intern either sign of zero as their ZERO tag.
	if value == 0 {
		value = 0
	}
	if bits == 32 {
		return float32(value), true, nil
	}
	return value, true, nil
}
