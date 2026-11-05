package builtin

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
)

type dataCommand struct {
	gameMaster
	Args cmd.Varargs
}

func formatData(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(b)
}
func nbtPath(tag map[string]any, path string) (any, bool) {
	var value any = tag
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// dataResult is DataCommands.getSingleTag's result: numbers truncate to an
// int, strings and collections return their length.
func dataResult(v any) int {
	switch value := v.(type) {
	case bool:
		if value {
			return 1
		}
		return 0
	case uint8:
		return int(value)
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case int:
		return value
	case float32:
		return int(math.Floor(float64(value)))
	case float64:
		return int(math.Floor(value))
	case string:
		return len(value)
	case map[string]any:
		return len(value)
	case []any:
		return len(value)
	}
	return 0
}
