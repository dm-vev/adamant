package world

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/javanbt"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// ParseBlockState parses the block-state prefix accepted by Java's
// BlockStateParser.parseForBlock, completing omitted properties from Java defaults.
func ParseBlockState(input string) (BlockState, error) {
	p := javaStateReader{s: input}
	name, err := p.identifier()
	if err != nil {
		return BlockState{}, err
	}
	if strings.HasPrefix(name, ":") {
		name = "minecraft" + name
	} else if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	if !gamedata.HasBlock(name) {
		return BlockState{}, fmt.Errorf("unknown Java block %q", name)
	}
	props := gamedata.BlockPropertiesWithDefaults(name, nil)
	if p.peek('[') {
		p.i++
		afterComma := false
		var keys [16]string
		seen := keys[:0]
		for {
			if afterComma && p.peek(']') {
				p.i++
				break
			}
			p.space()
			if p.peek(']') {
				if afterComma {
					return BlockState{}, fmt.Errorf("expected property after comma")
				}
				p.i++
				break
			}
			if p.i >= len(p.s) {
				return BlockState{}, fmt.Errorf("expected end of properties")
			}
			key, e := p.string()
			if e != nil {
				return BlockState{}, e
			}
			if !gamedata.BlockHasProperty(name, key) {
				return BlockState{}, fmt.Errorf("unknown property %q for %s", key, name)
			}
			for _, previous := range seen {
				if previous == key {
					return BlockState{}, fmt.Errorf("duplicate property %q", key)
				}
			}
			seen = append(seen, key)
			p.space()
			if !p.peek('=') {
				return BlockState{}, fmt.Errorf("expected '=' after property %q", key)
			}
			p.i++
			p.space()
			value, e := p.string()
			if e != nil {
				return BlockState{}, e
			}
			v, e := gamedata.ParseBlockPropertyValue(name, key, value)
			if e != nil {
				return BlockState{}, e
			}
			props[key] = v
			p.space()
			if p.peek(']') {
				p.i++
				break
			}
			if !p.peek(',') {
				return BlockState{}, fmt.Errorf("expected ',' or ']'")
			}
			p.i++
			afterComma = true
		}
	}
	if p.peek('{') {
		if _, _, err := javanbt.ParseCompoundArgument(input, p.i); err != nil {
			return BlockState{}, err
		}
	}
	return BlockState{Name: name, Properties: props}, nil
}

type javaStateReader struct {
	s string
	i int
}

func (p *javaStateReader) peek(b byte) bool { return p.i < len(p.s) && p.s[p.i] == b }
func (p *javaStateReader) space() {
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if !vanilla.IsWhitespace(r) {
			break
		}
		p.i += n
	}
}
func (p *javaStateReader) identifier() (string, error) {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == '/' || c == ':' {
			p.i++
		} else {
			break
		}
	}
	if start == p.i {
		return "", fmt.Errorf("expected block identifier")
	}
	return p.s[start:p.i], nil
}
func (p *javaStateReader) string() (string, error) {
	if p.i >= len(p.s) {
		return "", fmt.Errorf("expected string")
	}
	quote := p.s[p.i]
	if quote != '\'' && quote != '"' {
		start := p.i
		for p.i < len(p.s) {
			c := p.s[p.i]
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || c == '-' || c == '.' || c == '+') {
				break
			}
			p.i++
		}
		if start == p.i {
			return "", nil
		}
		return p.s[start:p.i], nil
	}
	p.i++
	start, segment := p.i, p.i
	var b strings.Builder
	escaped := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == quote {
			end := p.i
			p.i++
			if !escaped {
				return p.s[start:end], nil
			}
			b.WriteString(p.s[segment:end])
			return b.String(), nil
		}
		if c != '\\' {
			p.i++
			continue
		}
		b.WriteString(p.s[segment:p.i])
		escaped = true
		p.i++
		if p.i == len(p.s) {
			break
		}
		c = p.s[p.i]
		p.i++
		if c != quote && c != '\\' {
			return "", fmt.Errorf("invalid escape %q", c)
		}
		b.WriteByte(c)
		segment = p.i
	}
	return "", fmt.Errorf("unterminated quoted string")
}
