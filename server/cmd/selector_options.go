package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

// This file holds what EntitySelectorOptions decides about which options a
// selector may carry, and the filters of the options that need more than a
// line.

// selectorSet is Java's InvertableSetOptionState: what name, gamemode, team
// and type have parsed so far. A positive element settles the option for good,
// negated ones and tags only exclude more.
type selectorSet struct {
	state int // 0 none, 1 a positive element, 2 negated elements or tags.
	tags  map[string]bool
}

// canParseAny reports if the option may be given at all.
func (s *selectorSet) canParseAny() bool { return s.state != 1 }

// canParseElement reports if an element, negated or not, may follow.
func (s *selectorSet) canParseElement(inverted bool) bool {
	if inverted {
		return s.state != 1
	}
	return s.state == 0
}

func (s *selectorSet) markElement(inverted bool) {
	s.state = 1
	if inverted {
		s.state = 2
	}
}

// markTag notes a type tag, which only excludes, and reports if it was new.
func (s *selectorSet) markTag(tag string) bool {
	if s.state == 1 || s.tags[tag] {
		return false
	}
	if s.tags == nil {
		s.tags = make(map[string]bool)
	}
	s.state, s.tags[tag] = 2, true
	return true
}

// Java's once-only options: EntitySelectorOptions registers each with a
// predicate that holds while its value is unset.
var selectorOnceOptions = []string{"x", "y", "z", "dx", "dy", "dz", "x_rotation", "y_rotation", "distance", "level", "limit", "sort", "scores"}

var selectorSetOptions = []string{"name", "gamemode", "team", "type"}

// selectorError is a refusal of the selector parser, worded as Java's
// argument.entity.options texts.
type selectorError string

func (e selectorError) Error() string { return string(e) }

func selectorInapplicable(key string) error {
	return selectorError(fmt.Sprintf("Option '%s' isn't applicable here", key))
}

// selectorKnownOptions are the options Java registers.
var selectorKnownOptions = append(slices.Concat(selectorOnceOptions, selectorSetOptions), "tag", "nbt", "predicate", "advancements")

// selectorOptions is the state of the options parsed so far: which once-only
// options were given and what the set options hold.
type selectorOptions struct {
	given map[string]bool
	sets  map[string]*selectorSet
}

func newSelectorOptions() *selectorOptions {
	o := &selectorOptions{given: make(map[string]bool), sets: make(map[string]*selectorSet, len(selectorSetOptions))}
	for _, key := range selectorSetOptions {
		o.sets[key] = &selectorSet{}
	}
	return o
}

// check is EntitySelectorOptions.get: it refuses an unknown option and one
// that cannot apply before it reads the value. limit and sort are refused
// for @s, which selects its executor alone.
func (o *selectorOptions) check(kind byte, key string) error {
	if !slices.Contains(selectorKnownOptions, key) {
		return selectorError(fmt.Sprintf("Unknown option '%s'", key))
	}
	if slices.Contains(selectorOnceOptions, key) {
		if o.given[key] || kind == 's' && (key == "limit" || key == "sort") {
			return selectorInapplicable(key)
		}
		o.given[key] = true
	}
	if set, ok := o.sets[key]; ok && !set.canParseAny() {
		return selectorInapplicable(key)
	}
	return nil
}

// element marks an element of a set option parsed, or refuses it.
func (o *selectorOptions) element(key string, negate bool) error {
	if !o.sets[key].canParseElement(negate) {
		return selectorInapplicable(key)
	}
	o.sets[key].markElement(negate)
	return nil
}

func (s *entitySelector) scoresFilter(expected map[string]selectorRange, tx *world.Tx) {
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

func (s *entitySelector) teamFilter(value string, negate bool, tx *world.Tx) {
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
}

func (s *entitySelector) tagFilter(value string, negate bool) {
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
}

var selectorGameModes = []string{"survival", "creative", "adventure", "spectator"}

// gameModeFilter adds the gamemode option, which refuses a name that is no
// game mode.
func (s *entitySelector) gameModeFilter(opts *selectorOptions, value string, negate bool) error {
	if !opts.sets["gamemode"].canParseElement(negate) {
		return selectorInapplicable("gamemode")
	}
	if !slices.Contains(selectorGameModes, value) {
		return selectorError(fmt.Sprintf("Unknown or illegal game mode '%s'", value))
	}
	opts.sets["gamemode"].markElement(negate)
	s.filter(negate, func(t Target) bool {
		p, ok := t.(interface{ GameMode() world.GameMode })
		if !ok {
			return false
		}
		id, _ := world.GameModeID(p.GameMode())
		return id >= 0 && id < len(selectorGameModes) && selectorGameModes[id] == value
	})
	return nil
}

// typeFilter adds the type option: an entity type, or a tag of them, which
// only excludes and may not repeat. Java refuses an entity type it does not
// register; a tag it does not register is accepted and holds no entity, as
// TagKey.create does not look it up.
func (s *entitySelector) typeFilter(opts *selectorOptions, value string, negate bool) error {
	isTag := strings.HasPrefix(value, "#")
	id := strings.TrimPrefix(value, "#")
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	set := opts.sets["type"]
	if isTag && !set.markTag(id) || !isTag && !set.canParseElement(negate) {
		return selectorInapplicable("type")
	}
	if !isTag {
		if _, ok := javaEntityTypes[id]; !ok {
			return selectorError(fmt.Sprintf("Invalid or unknown entity type '%s'", id))
		}
		set.markElement(negate)
	}
	s.filter(negate, func(t Target) bool {
		e, ok := t.(world.Entity)
		if !ok {
			return false
		}
		if isTag {
			in, err := gamedata.EntityTypeTagContains(id, world.JavaEntityID(e))
			return err == nil && in
		}
		return e.H().Type().EncodeEntity() == id || world.JavaEntityID(e) == id
	})
	return nil
}
