package cmd

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ResolveTargets resolves a Java selector or player name relative to src, as
// a command argument would. Execute uses it for nested selector arguments.
func ResolveTargets(selector string, src Source, tx *world.Tx) ([]Target, error) {
	line := &Line{args: []string{selector}, src: src}
	return parser{}.parseTargets(line, tx)
}

// executor returns the entity @s refers to: execute's executor when set,
// otherwise the source itself.
func executor(src Source) (Target, bool) {
	if context, ok := ExecutionContextOf(src); ok {
		if context.Executor == nil {
			return nil, false
		}
		return context.Executor, true
	}
	return src, src != nil
}

type selectorRange struct {
	min, max float64
	set      bool
}

func (r selectorRange) contains(v float64) bool {
	return !r.set || v >= r.min && v <= r.max
}

func parseSelectorRange(value string) (selectorRange, error) {
	r := selectorRange{min: math.Inf(-1), max: math.Inf(1), set: true}
	lo, hi, isRange := strings.Cut(value, "..")
	if !isRange {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return r, err
		}
		r.min, r.max = v, v
		return r, nil
	}
	var err error
	if lo != "" {
		if r.min, err = strconv.ParseFloat(lo, 64); err != nil {
			return r, err
		}
	}
	if hi != "" {
		if r.max, err = strconv.ParseFloat(hi, 64); err != nil {
			return r, err
		}
	}
	return r, nil
}

// entitySelector holds the parsed Java EntitySelectorOptions subset that has
// Bedrock-side state: position/volume/distance, type, name, tag, gamemode,
// level, limit and sort.
type entitySelector struct {
	kind     byte
	origin   mgl64.Vec3
	volume   *mgl64.Vec3
	distance selectorRange
	level    selectorRange
	limit    int
	sort     string
	filters  []func(Target) bool
}

func parseSelector(text string, src Source) (*entitySelector, error) {
	s := &entitySelector{kind: text[1], origin: src.Position(), sort: "arbitrary", limit: math.MaxInt}
	switch s.kind {
	case 'p':
		s.sort, s.limit = "nearest", 1
	case 'r':
		s.sort, s.limit = "random", 1
	case 'n':
		s.sort, s.limit = "nearest", 1
	case 'a', 'e', 's':
	default:
		return nil, MessageParameterInvalid.F(text)
	}
	body := text[2:]
	if body == "" {
		return s, nil
	}
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		return nil, MessageParameterInvalid.F(text)
	}
	var dx, dy, dz *float64
	for _, option := range splitSelectorOptions(body[1 : len(body)-1]) {
		key, value, ok := strings.Cut(option, "=")
		if !ok {
			return nil, MessageParameterInvalid.F(option)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		negate := strings.HasPrefix(value, "!")
		value = strings.TrimPrefix(value, "!")
		var err error
		switch key {
		case "x", "y", "z":
			v, perr := strconv.ParseFloat(value, 64)
			if perr != nil {
				return nil, MessageNumberInvalid.F(value)
			}
			s.origin[key[0]-'x'] = v
		case "dx", "dy", "dz":
			v, perr := strconv.ParseFloat(value, 64)
			if perr != nil {
				return nil, MessageNumberInvalid.F(value)
			}
			switch key {
			case "dx":
				dx = &v
			case "dy":
				dy = &v
			default:
				dz = &v
			}
		case "distance":
			s.distance, err = parseSelectorRange(value)
		case "level":
			s.level, err = parseSelectorRange(value)
		case "limit":
			s.limit, err = strconv.Atoi(value)
			if err == nil && s.limit < 1 {
				err = fmt.Errorf("limit must be positive")
			}
		case "sort":
			if !slices.Contains([]string{"nearest", "furthest", "random", "arbitrary"}, value) {
				err = fmt.Errorf("invalid sort")
			}
			s.sort = value
		case "type":
			id := value
			if !strings.Contains(id, ":") {
				id = "minecraft:" + id
			}
			s.filter(negate, func(t Target) bool {
				e, ok := t.(world.Entity)
				return ok && e.H().Type().EncodeEntity() == id
			})
		case "name":
			s.filter(negate, func(t Target) bool { return selectorName(t) == strings.Trim(value, `"`) })
		case "tag":
			s.filter(negate, func(t Target) bool {
				tagged, ok := t.(interface{ CommandTags() []string })
				if !ok {
					return value == ""
				}
				if value == "" {
					return len(tagged.CommandTags()) == 0
				}
				return slices.Contains(tagged.CommandTags(), value)
			})
		case "gamemode":
			s.filter(negate, func(t Target) bool {
				p, ok := t.(interface{ GameMode() world.GameMode })
				if !ok {
					return false
				}
				id, _ := world.GameModeID(p.GameMode())
				names := []string{"survival", "creative", "adventure", "spectator"}
				return id >= 0 && id < len(names) && names[id] == value
			})
		default:
			return nil, MessageParameterInvalid.F(key)
		}
		if err != nil {
			return nil, MessageParameterInvalid.F(option)
		}
	}
	if dx != nil || dy != nil || dz != nil {
		v := mgl64.Vec3{}
		for i, d := range []*float64{dx, dy, dz} {
			if d != nil {
				v[i] = *d
			}
		}
		s.volume = &v
	}
	return s, nil
}

func (s *entitySelector) filter(negate bool, f func(Target) bool) {
	if negate {
		s.filters = append(s.filters, func(t Target) bool { return !f(t) })
		return
	}
	s.filters = append(s.filters, f)
}

// splitSelectorOptions splits on top-level commas, keeping nested braces and
// quoted names intact.
func splitSelectorOptions(body string) []string {
	var options []string
	depth, quoted, start := 0, false, 0
	for i, r := range body {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '{' || r == '[':
			depth++
		case r == '}' || r == ']':
			depth--
		case r == ',' && depth == 0:
			options = append(options, body[start:i])
			start = i + 1
		}
	}
	if strings.TrimSpace(body[start:]) != "" {
		options = append(options, body[start:])
	}
	return options
}

func selectorName(t Target) string {
	if named, ok := t.(NamedTarget); ok {
		return named.Name()
	}
	if tagged, ok := t.(interface{ NameTag() string }); ok {
		return tagged.NameTag()
	}
	return ""
}

func (s *entitySelector) matches(t Target) bool {
	pos := t.Position()
	if !s.distance.contains(pos.Sub(s.origin).Len()) {
		return false
	}
	if s.volume != nil {
		lo := s.origin
		hi := s.origin.Add(*s.volume)
		for i := range 3 {
			l, h := min(lo[i], hi[i]), max(lo[i], hi[i])+1
			if pos[i] < l || pos[i] > h {
				return false
			}
		}
	}
	if s.level.set {
		levelled, ok := t.(interface{ ExperienceLevel() int })
		if !ok || !s.level.contains(float64(levelled.ExperienceLevel())) {
			return false
		}
	}
	for _, f := range s.filters {
		if !f(t) {
			return false
		}
	}
	return true
}

// selectTargets applies EntitySelector.findEntities: filter, sort, then limit.
func (s *entitySelector) selectTargets(src Source, entities []Target, players []NamedTarget) []Target {
	var candidates []Target
	switch s.kind {
	case 's':
		self, ok := executor(src)
		if !ok {
			return nil
		}
		candidates = []Target{self}
	case 'e', 'n':
		candidates = entities
	default:
		for _, p := range players {
			candidates = append(candidates, p)
		}
	}
	result := make([]Target, 0, len(candidates))
	for _, t := range candidates {
		if s.matches(t) {
			result = append(result, t)
		}
	}
	switch s.sort {
	case "nearest":
		slices.SortStableFunc(result, func(a, b Target) int {
			return cmp.Compare(a.Position().Sub(s.origin).Len(), b.Position().Sub(s.origin).Len())
		})
	case "furthest":
		slices.SortStableFunc(result, func(a, b Target) int {
			return cmp.Compare(b.Position().Sub(s.origin).Len(), a.Position().Sub(s.origin).Len())
		})
	case "random":
		rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	}
	if len(result) > s.limit {
		result = result[:s.limit]
	}
	return result
}
