package cmd

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/javanbt"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// selectorParse is the state of parsing the option list of one selector: the
// selector so far, which options were given, the extent of the volume and the
// transaction the filters read the scoreboard and entities of.
type selectorParse struct {
	s      *entitySelector
	opts   *selectorOptions
	tx     *world.Tx
	extent [3]*float64
}

// selectorOption is one option of the list as its parser gets it: the whole
// option, its key, and its value with the negation taken off.
type selectorOption struct {
	*selectorParse
	option, key, value string
	negate             bool
}

// selectorOptionParser parses the value of an option into the selector. An
// error that is a selectorError is the refusal of the parser, worded as Java
// words it, and a selectorFinal is the refusal that the parser words itself;
// any other error means the parameter is not valid.
type selectorOptionParser func(o selectorOption) error

// selectorFinal is an error of an option that is returned as it is.
type selectorFinal struct{ error }

// selectorNegatable are the options that take a negated value.
var selectorNegatable = []string{"type", "name", "tag", "team", "gamemode", "nbt"}

// selectorOptionParsers are the parsers of the options by key. The options
// that Java registers and this server has no data for, predicate and
// advancements, have none.
var selectorOptionParsers = map[string]selectorOptionParser{
	"x":          parseSelectorOrigin,
	"y":          parseSelectorOrigin,
	"z":          parseSelectorOrigin,
	"dx":         parseSelectorExtent,
	"dy":         parseSelectorExtent,
	"dz":         parseSelectorExtent,
	"distance":   parseSelectorDistance,
	"level":      parseSelectorLevel,
	"x_rotation": func(o selectorOption) error { return parseSelectorRotation(o, true) },
	"y_rotation": func(o selectorOption) error { return parseSelectorRotation(o, false) },
	"scores":     parseSelectorScoresOption,
	"team":       parseSelectorTeam,
	"nbt":        parseSelectorNBT,
	"limit":      parseSelectorLimit,
	"sort":       parseSelectorSort,
	"type":       func(o selectorOption) error { return o.s.typeFilter(o.opts, o.value, o.negate) },
	"name":       parseSelectorName,
	"tag":        func(o selectorOption) error { o.s.tagFilter(o.value, o.negate); return nil },
	"gamemode":   func(o selectorOption) error { return o.s.gameModeFilter(o.opts, o.value, o.negate) },
}

// newEntitySelector returns the selector of the kind that text starts with,
// with the defaults of that kind.
func newEntitySelector(text string, src Source) (*entitySelector, error) {
	s := &entitySelector{kind: text[1], origin: src.Position(), sort: "arbitrary", limit: math.MaxInt}
	switch s.kind {
	case 'p', 'n':
		s.sort, s.limit = "nearest", 1
	case 'r':
		s.sort, s.limit = "random", 1
	case 'a', 'e', 's':
	default:
		return nil, MessageParameterInvalid.F(text)
	}
	return s, nil
}

// parseSelectorTable parses a selector such as @e[type=cow,limit=1] by the
// table of the parsers of its options.
func parseSelectorTable(text string, src Source, tx *world.Tx) (*entitySelector, error) {
	s, err := newEntitySelector(text, src)
	if err != nil {
		return nil, err
	}
	body := text[2:]
	if body == "" {
		return s, nil
	}
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		return nil, MessageParameterInvalid.F(text)
	}
	p := &selectorParse{s: s, opts: newSelectorOptions(), tx: tx}
	for _, option := range splitSelectorOptions(body[1 : len(body)-1]) {
		if err := p.parseOption(option); err != nil {
			return nil, err
		}
	}
	if slices.ContainsFunc(p.extent[:], func(d *float64) bool { return d != nil }) {
		v := mgl64.Vec3{}
		for i, d := range p.extent {
			if d != nil {
				v[i] = *d
			}
		}
		s.volume = &v
	}
	return s, nil
}

// parseOption parses one option of the list into the selector.
func (p *selectorParse) parseOption(option string) error {
	key, value, ok := strings.Cut(option, "=")
	if !ok {
		return MessageParameterInvalid.F(option)
	}
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	negate := strings.HasPrefix(value, "!")
	value = strings.TrimPrefix(value, "!")
	if negate && !slices.Contains(selectorNegatable, key) {
		return MessageParameterInvalid.F(option)
	}
	if err := p.opts.check(p.s.kind, key); err != nil {
		return err
	}
	parse, ok := selectorOptionParsers[key]
	if !ok {
		// A Java option this server has no data for: predicate, advancements.
		return MessageParameterInvalid.F(key)
	}
	err := parse(selectorOption{selectorParse: p, option: option, key: key, value: value, negate: negate})
	if final := (selectorFinal{}); errors.As(err, &final) {
		return final.error
	}
	if selectorErr := (selectorError("")); errors.As(err, &selectorErr) {
		return err
	}
	if err != nil {
		return MessageParameterInvalid.F(option)
	}
	return nil
}

// parseSelectorOrigin is x, y and z: the origin of the selector.
func parseSelectorOrigin(o selectorOption) error {
	v, err := ParseNumber(o.value, 64)
	if err != nil {
		return selectorFinal{MessageNumberInvalid.F(o.value)}
	}
	o.s.origin[o.key[0]-'x'] = v
	return nil
}

// parseSelectorExtent is dx, dy and dz: the extent of the volume.
func parseSelectorExtent(o selectorOption) error {
	v, err := ParseNumber(o.value, 64)
	if err != nil {
		return selectorFinal{MessageNumberInvalid.F(o.value)}
	}
	o.extent[o.key[1]-'x'] = &v
	return nil
}

// parseSelectorDistance is distance, a range that is not negative.
func parseSelectorDistance(o selectorOption) error {
	var err error
	o.s.distance, err = parseSelectorRange(o.value)
	if err == nil && o.s.distance.swapped() {
		err = fmt.Errorf("invalid distance")
	}
	if err == nil && o.s.distance.negative() {
		return selectorError("Distance cannot be negative")
	}
	return err
}

// parseSelectorLevel is level, a range of integers that is not negative.
func parseSelectorLevel(o selectorOption) error {
	var err error
	o.s.level, err = parseIntRange(o.value)
	if err == nil && o.s.level.negative() {
		return selectorError("Level shouldn't be negative")
	}
	return err
}

// parseSelectorRotation is x_rotation, for the pitch, and y_rotation, for the
// yaw.
func parseSelectorRotation(o selectorOption, pitch bool) error {
	r, err := parseSelectorRange(o.value)
	if err == nil {
		o.s.filters = append(o.s.filters, rotationFilter(r, pitch))
	}
	return err
}

// parseSelectorScoresOption is scores, the ranges of the scores of the
// objectives.
func parseSelectorScoresOption(o selectorOption) error {
	expected, err := parseSelectorScores(o.value)
	if err == nil && len(expected) > 0 {
		o.s.scoresFilter(expected, o.tx)
	}
	return err
}

// parseSelectorTeam is team, the name of a team or none.
func parseSelectorTeam(o selectorOption) error {
	err := o.opts.element(o.key, o.negate)
	o.s.teamFilter(o.value, o.negate, o.tx)
	return err
}

// parseSelectorNBT is nbt, a compound that the NBT of the entity contains.
func parseSelectorNBT(o selectorOption) error {
	expected, end, err := javanbt.ParseCompoundArgument(o.value, 0)
	if err != nil || strings.TrimSpace(o.value[end:]) != "" {
		return selectorFinal{MessageParameterInvalid.F(o.option)}
	}
	tx := o.tx
	o.s.filter(o.negate, func(t Target) bool {
		e, ok := t.(world.Entity)
		return ok && nbtContains(expected, e.H().NBT(tx))
	})
	return nil
}

// parseSelectorLimit is limit, the number of targets, at least one.
func parseSelectorLimit(o selectorOption) error {
	var err error
	o.s.limit, err = strconv.Atoi(o.value)
	if err == nil && o.s.limit < 1 {
		return selectorError("The minimum number allowed is 1")
	}
	return err
}

// selectorSorts are the orders of the sort option.
var selectorSorts = []string{"nearest", "furthest", "random", "arbitrary"}

// parseSelectorSort is sort, the order of the targets.
func parseSelectorSort(o selectorOption) error {
	if !slices.Contains(selectorSorts, o.value) {
		return selectorError(fmt.Sprintf("Invalid or unknown sort type '%s'", o.value))
	}
	o.s.sort = o.value
	return nil
}

// parseSelectorName is name, the name of the target.
func parseSelectorName(o selectorOption) error {
	err := o.opts.element(o.key, o.negate)
	value := o.value
	o.s.filter(o.negate, func(t Target) bool { return selectorName(t) == strings.Trim(value, `"`) })
	return err
}
