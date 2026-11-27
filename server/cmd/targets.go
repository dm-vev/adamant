package cmd

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/dm-vev/adamant/server/world"
)

var (
	// ErrNoPlayerFound is Java's commands.generic.player.notFound error text.
	ErrNoPlayerFound = errors.New("No player was found") //nolint:staticcheck // Java Minecraft command error text.
	// ErrNoEntityFound is Java's commands.generic.entity.notFound error text.
	ErrNoEntityFound = errors.New("No entity was found") //nolint:staticcheck // Java Minecraft command error text.
)

// Selector is a parsed target selector or player name passed to ForEach. It is
// implemented by Entities, OneEntity, Players and OnePlayer.
type Selector interface {
	selector() *entitySelector
}

// Entities is a parameter: a selector or a player name, parsed and not yet
// resolved. It may select any number of entities of any world, as Java's
// EntityArgument.entities() does.
type Entities struct{ sel *entitySelector }

// OneEntity refuses, when parsed, a selector that may select more than one.
type OneEntity struct{ sel *entitySelector }

// Players refuses, when parsed, a selector that includes entities that are not
// players.
type Players struct{ sel *entitySelector }

// OnePlayer refuses, when parsed, a selector that includes non-players or may
// select more than one.
type OnePlayer struct{ sel *entitySelector }

func (e Entities) selector() *entitySelector  { return e.sel }
func (e OneEntity) selector() *entitySelector { return e.sel }
func (p Players) selector() *entitySelector   { return p.sel }
func (p OnePlayer) selector() *entitySelector { return p.sel }

// ParseEntities parses a target selector, entity UUID, or player name as an
// Entities selector relative to src without needing a world transaction.
func ParseEntities(text string, src Source) (Entities, error) {
	sel, err := parseSelectorInput(text, src, false, false)
	if err != nil {
		return Entities{}, err
	}
	return Entities{sel: sel}, nil
}

// ParseOneEntity parses a single-entity selector, entity UUID, or player name
// relative to src without needing a world transaction.
func ParseOneEntity(text string, src Source) (OneEntity, error) {
	sel, err := parseSelectorInput(text, src, false, true)
	if err != nil {
		return OneEntity{}, err
	}
	return OneEntity{sel: sel}, nil
}

// ParsePlayers parses a player-only selector or player name relative to src
// without needing a world transaction.
func ParsePlayers(text string, src Source) (Players, error) {
	sel, err := parseSelectorInput(text, src, true, false)
	if err != nil {
		return Players{}, err
	}
	return Players{sel: sel}, nil
}

// ParseOnePlayer parses a single-player selector or player name relative to
// src without needing a world transaction.
func ParseOnePlayer(text string, src Source) (OnePlayer, error) {
	sel, err := parseSelectorInput(text, src, true, true)
	if err != nil {
		return OnePlayer{}, err
	}
	return OnePlayer{sel: sel}, nil
}

// Result is what a command did to one target.
type Result[R any] struct {
	// Name is the name of the target as output shows it, read in its world.
	Name   string
	Handle *world.EntityHandle
	Value  R
}

// Tally sums results as Java's CommandResponseTracker does.
type Tally struct {
	// Total is the sum of the values: the result of the command.
	Total int
	// Count and NonZero are the total targets and targets with a non-zero value.
	Count, NonZero int
	// Only and OnlyNonZero hold the target's display name when the respective
	// count is exactly one.
	Only, OnlyNonZero string
}

// TallyOf sums the results of a command that returns an integer per target.
func TallyOf(results []Result[int]) Tally {
	var t Tally
	for _, r := range results {
		t.Count++
		if t.Count == 1 {
			t.Only = r.Name
		} else {
			t.Only = ""
		}
		if r.Value != 0 {
			t.Total += r.Value
			t.NonZero++
			if t.NonZero == 1 {
				t.OnlyNonZero = r.Name
			} else {
				t.OnlyNonZero = ""
			}
		}
	}
	return t
}

// Feedback prints single with the one name or multiple with the count, sets
// the result to Total, or adds failed if no target counted.
func (t Tally) Feedback(o *Output, failed error, single, multiple func(who string) string) {
	if t.NonZero == 0 {
		if failed != nil {
			o.Error(failed)
		}
		return
	}
	if t.NonZero == 1 {
		o.Print(single(t.OnlyNonZero))
	} else {
		o.Print(multiple(strconv.Itoa(t.NonZero)))
	}
	o.SetResult(t.Total)
}

// targetHandle returns the EntityHandle of t if t is a world.Entity.
func targetHandle(t Target) *world.EntityHandle {
	if e, ok := t.(world.Entity); ok {
		return e.H()
	}
	return nil
}

// targetDisplayName returns the display name of t as command output shows it:
// a player's name, a mob's custom name tag, or the title-cased Java entity
// type name.
func targetDisplayName(t Target) string {
	if named, ok := t.(NamedTarget); ok && named.Name() != "" {
		return named.Name()
	}
	if tagged, ok := t.(interface{ NameTag() string }); ok && tagged.NameTag() != "" {
		return tagged.NameTag()
	}
	if e, ok := t.(world.Entity); ok {
		return displayTypeName(world.CanonicalEntityTypeID(e.H().Type().EncodeEntity()))
	}
	return ""
}

// displayTypeName formats a namespaced identifier such as minecraft:iron_golem
// as "Iron Golem".
func displayTypeName(id string) string {
	words := strings.Split(strings.TrimPrefix(id, "minecraft:"), "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
