package block

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
)

var javaActorColourIDs = map[string]uint8{
	"white": 0, "orange": 1, "magenta": 2, "light_blue": 3, "yellow": 4, "lime": 5, "pink": 6, "gray": 7,
	"light_gray": 8, "cyan": 9, "purple": 10, "blue": 11, "brown": 12, "green": 13, "red": 14, "black": 15,
}

// javaColourNames are the Java names of the colours in the order of
// item.Colours.
var javaColourNames = [...]string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

func javaActorColour(name string) (item.Colour, bool) {
	id, ok := javaActorColourIDs[name]
	if !ok {
		return item.Colour{}, false
	}
	return item.Colours()[id], true
}

func javaActorString(properties map[string]any, key, fallback string) (string, error) {
	value, exists := properties[key]
	if !exists {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s %v", key, value)
	}
	return text, nil
}

func javaActorBool(properties map[string]any, key string) (bool, error) {
	value, exists := properties[key]
	if !exists {
		return false, nil
	}
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed, nil
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		if n, ok := javaActorInteger(value); ok && (n == 0 || n == 1) {
			return n == 1, nil
		}
	}
	return false, fmt.Errorf("invalid %s %v", key, value)
}

func javaActorInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v <= uint64(^uint64(0)>>1) {
			return int64(v), true
		}
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	}
	return 0, false
}

func javaActorFacing(value string) (cube.Direction, error) {
	switch strings.ToLower(value) {
	case "north":
		return cube.North, nil
	case "south":
		return cube.South, nil
	case "west":
		return cube.West, nil
	case "east":
		return cube.East, nil
	default:
		return 0, fmt.Errorf("invalid facing %q", value)
	}
}
