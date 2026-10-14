package cmd

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/internal/javanbt"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ResolveTargets resolves a Java selector or player name relative to src, as
// a command argument would. Execute uses it for nested selector arguments.
func ResolveTargets(selector string, src Source, tx *world.Tx) ([]Target, error) {
	line := &Line{args: []string{selector}, src: src}
	return parser{}.parseTargets(line, tx)
}

// ScoreHolderName is Entity.getScoreboardName: players hold scores and join
// teams by their name, other entities by their UUID.
func ScoreHolderName(t Target) string {
	e, ok := t.(world.Entity)
	if !ok {
		return ""
	}
	if named, ok := t.(NamedTarget); ok && e.H().Type().EncodeEntity() == "minecraft:player" {
		return named.Name()
	}
	return e.H().UUID().String()
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

// negativeMin reports a range with a set, negative minimum, which distance
// and level reject.
func (r selectorRange) negativeMin() bool {
	return !math.IsInf(r.min, -1) && r.min < 0
}

// swapped reports a range whose minimum exceeds its maximum, which Java's
// integer and decimal bounds reject.
func (r selectorRange) swapped() bool {
	return r.min > r.max
}

// parseSelectorRange ports MinMaxBounds.Bounds.fromReader: one value or an
// inclusive range with at most one open end.
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
	if lo == "" && hi == "" {
		return r, fmt.Errorf("empty range")
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

// parseIntRange ports MinMaxBounds.Ints.fromReader: integer ends that are not
// swapped.
func parseIntRange(value string) (selectorRange, error) {
	r, err := parseSelectorRange(value)
	if err != nil {
		return r, err
	}
	for _, end := range []float64{r.min, r.max} {
		if !math.IsInf(end, 0) && end != math.Trunc(end) {
			return r, fmt.Errorf("range ends must be integers")
		}
	}
	if r.swapped() {
		return r, fmt.Errorf("swapped range")
	}
	return r, nil
}

// wrapDegrees ports Mth.wrapDegrees to [-180, 180).
func wrapDegrees(angle float64) float64 {
	angle = math.Mod(angle, 360)
	if angle >= 180 {
		angle -= 360
	}
	if angle < -180 {
		angle += 360
	}
	return angle
}

// rotationFilter ports EntitySelectorParser.createRotationPredicate: open
// ends default to 0 and 359 degrees, and a range whose wrapped minimum
// exceeds its maximum matches across the wrap.
func rotationFilter(r selectorRange, pitch bool) func(Target) bool {
	lo, hi := 0.0, 359.0
	if !math.IsInf(r.min, -1) {
		lo = r.min
	}
	if !math.IsInf(r.max, 1) {
		hi = r.max
	}
	lo, hi = wrapDegrees(lo), wrapDegrees(hi)
	return func(t Target) bool {
		rotated, ok := t.(interface{ Rotation() cube.Rotation })
		if !ok {
			return false
		}
		v := rotated.Rotation().Yaw()
		if pitch {
			v = rotated.Rotation().Pitch()
		}
		v = wrapDegrees(v)
		if lo > hi {
			return v >= lo || v <= hi
		}
		return v >= lo && v <= hi
	}
}

// parseSelectorScores parses the scores option: {objective=range,...}.
func parseSelectorScores(value string) (map[string]selectorRange, error) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, fmt.Errorf("scores must be enclosed in braces")
	}
	expected := make(map[string]selectorRange)
	for _, entry := range splitSelectorOptions(value[1 : len(value)-1]) {
		name, bounds, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("score %q has no range", entry)
		}
		r, err := parseIntRange(strings.TrimSpace(bounds))
		if err != nil {
			return nil, err
		}
		expected[strings.TrimSpace(name)] = r
	}
	return expected, nil
}

// nbtContains ports NbtUtils.compareNbt with partial list matching: every
// tag of expected must be present in actual with the same type and value,
// and every element of an expected list must match some actual element.
func nbtContains(expected, actual any) bool {
	if expected == nil {
		return true
	}
	if actual == nil {
		return false
	}
	expected, actual = nbtTag(expected), nbtTag(actual)
	if reflect.TypeOf(expected) != reflect.TypeOf(actual) && !(nbtIsList(expected) && nbtIsList(actual)) {
		return false
	}
	switch want := expected.(type) {
	case map[string]any:
		got := actual.(map[string]any)
		if len(got) < len(want) {
			return false
		}
		for key, value := range want {
			if !nbtContains(value, got[key]) {
				return false
			}
		}
		return true
	case []uint8, []int32, []int64:
		return reflect.DeepEqual(expected, actual)
	}
	if nbtIsList(expected) {
		want, got := reflect.ValueOf(expected), reflect.ValueOf(actual)
		if want.Len() == 0 {
			return got.Len() == 0
		}
		if got.Len() < want.Len() {
			return false
		}
		for i := range want.Len() {
			if !slices.ContainsFunc(nbtElements(got), func(v any) bool { return nbtContains(want.Index(i).Interface(), v) }) {
				return false
			}
		}
		return true
	}
	return expected == actual
}

// nbtTag folds the Go types of one NBT tag: bytes are uint8 whether signed
// or a bool.
func nbtTag(v any) any {
	switch v := v.(type) {
	case bool:
		if v {
			return uint8(1)
		}
		return uint8(0)
	case int8:
		return uint8(v)
	case []int8:
		b := make([]uint8, len(v))
		for i, x := range v {
			b[i] = uint8(x)
		}
		return b
	}
	return v
}

// nbtIsList reports a list tag: any slice other than the byte, int and long
// array tags.
func nbtIsList(v any) bool {
	switch v.(type) {
	case []uint8, []int32, []int64:
		return false
	}
	kind := reflect.TypeOf(v).Kind()
	return kind == reflect.Slice || kind == reflect.Array
}

func nbtElements(list reflect.Value) []any {
	elements := make([]any, list.Len())
	for i := range elements {
		elements[i] = list.Index(i).Interface()
	}
	return elements
}

// scoreboardOf returns the scoreboard selectors query, if any.
func scoreboardOf(tx *world.Tx) *scoreboard.Model {
	if tx == nil {
		return nil
	}
	return tx.World().Scoreboard()
}

// entitySelector holds the parsed Java EntitySelectorOptions that have
// Bedrock-side state: position/volume/distance, rotation, type and type tags,
// name, tag, team, scores, nbt, gamemode, level, limit and sort. Predicates
// and advancements belong to datapacks, which are not supported.
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

func parseSelector(text string, src Source, tx *world.Tx) (*entitySelector, error) {
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
		if negate && !slices.Contains([]string{"type", "name", "tag", "team", "gamemode", "nbt"}, key) {
			return nil, MessageParameterInvalid.F(option)
		}
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
			if err == nil && (s.distance.swapped() || s.distance.negativeMin()) {
				err = fmt.Errorf("invalid distance")
			}
		case "level":
			s.level, err = parseIntRange(value)
			if err == nil && s.level.negativeMin() {
				err = fmt.Errorf("negative level")
			}
		case "x_rotation", "y_rotation":
			var r selectorRange
			if r, err = parseSelectorRange(value); err == nil {
				s.filters = append(s.filters, rotationFilter(r, key == "x_rotation"))
			}
		case "scores":
			var expected map[string]selectorRange
			if expected, err = parseSelectorScores(value); err == nil && len(expected) > 0 {
				board := scoreboardOf(tx)
				s.filters = append(s.filters, func(t Target) bool {
					holder := ScoreHolderName(t)
					for objective, r := range expected {
						if board == nil {
							return false
						}
						v, ok := board.Score(objective, holder)
						if !ok || !r.contains(float64(v)) {
							return false
						}
					}
					return true
				})
			}
		case "team":
			board := scoreboardOf(tx)
			s.filter(negate, func(t Target) bool {
				current := ""
				if board != nil {
					if team, ok := board.TeamFor(ScoreHolderName(t)); ok {
						current = team.Name
					}
				}
				return current == value
			})
		case "nbt":
			expected, end, perr := javanbt.ParseCompoundArgument(value, 0)
			if perr != nil || strings.TrimSpace(value[end:]) != "" {
				return nil, MessageParameterInvalid.F(option)
			}
			s.filter(negate, func(t Target) bool {
				e, ok := t.(world.Entity)
				return ok && nbtContains(expected, e.H().NBT(tx))
			})
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
			isTag := strings.HasPrefix(value, "#")
			id := strings.TrimPrefix(value, "#")
			if !strings.Contains(id, ":") {
				id = "minecraft:" + id
			}
			s.filter(negate, func(t Target) bool {
				e, ok := t.(world.Entity)
				if !ok {
					return false
				}
				if isTag {
					in, err := javadata.EntityTypeTagContains(id, loot.JavaEntityID(e))
					return err == nil && in
				}
				return e.H().Type().EncodeEntity() == id || loot.JavaEntityID(e) == id
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
