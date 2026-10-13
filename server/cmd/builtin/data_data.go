package builtin

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type dataCommand struct {
	opGate
	Args cmd.Varargs
}

func (c dataCommand) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	if tx == nil {
		out.Error("data requires a world")
		return
	}
	words := strings.Fields(string(c.Args))
	if len(words) < 2 {
		out.Error("data requires get, merge, or remove and a target")
		return
	}
	action, target := words[0], words[1]
	if target == "storage" {
		if len(words) < 3 {
			out.Error("data storage requires a key")
			return
		}
		key := strings.Join(words[2:], " ")
		data, err := tx.World().LoadWorldData("java:storage:" + words[2])
		if action == "get" {
			if err != nil {
				out.Error(err)
				return
			}
			out.Printf("%s", string(data))
			return
		}
		if action == "remove" {
			if err := tx.World().SaveWorldData("java:storage:"+words[2], nil); err != nil {
				out.Error(err)
				return
			}
			out.Printf("Removed storage %s", key)
			return
		}
		if action == "merge" && len(words) >= 4 {
			var current, patch map[string]any
			if err == nil {
				if err := json.Unmarshal(data, &current); err != nil {
					out.Error(err)
					return
				}
			}
			if current == nil {
				current = make(map[string]any)
			}
			if err := json.Unmarshal([]byte(strings.Join(words[3:], " ")), &patch); err != nil {
				out.Error(err)
				return
			}
			for name, value := range patch {
				current[name] = value
			}
			encoded, err := json.Marshal(current)
			if err != nil {
				out.Error(err)
				return
			}
			if err := tx.World().SaveWorldData("java:storage:"+words[2], encoded); err != nil {
				out.Error(err)
				return
			}
			out.Printf("Merged storage %s", key)
			return
		}
		out.Error("unsupported storage operation")
		return
	}
	if target == "entity" {
		if len(words) < 3 {
			out.Error("data entity requires a target")
			return
		}
		targets, err := cmd.ResolveTargets(words[2], src, tx)
		if err != nil {
			out.Error(err)
			return
		}
		var entities []world.Entity
		for _, t := range targets {
			if e, ok := t.(world.Entity); ok {
				entities = append(entities, e)
			}
		}
		if len(entities) == 0 {
			out.Errort(cmd.MessageNoTargets)
			return
		}
		for _, entity := range entities {
			tag := entity.H().NBT(tx)
			if action == "get" {
				if len(words) > 3 {
					v, exists := nbtPath(tag, words[3])
					if !exists {
						out.Error("missing data path " + words[3])
						return
					}
					out.SetResult(dataResult(v))
					out.Printf("%s", formatData(v))
				} else {
					out.SetResult(dataResult(tag))
					out.Printf("%s", formatData(tag))
				}
				continue
			}
			if action == "merge" && len(words) > 3 {
				var patch map[string]any
				if err := json.Unmarshal([]byte(strings.Join(words[3:], " ")), &patch); err != nil {
					out.Error(err)
					return
				}
				if err := entity.H().MergeNBT(tx, patch); err != nil {
					out.Error(err)
					return
				}
				continue
			}
			if action == "remove" && len(words) > 3 {
				if err := entity.H().MergeNBT(tx, map[string]any{words[3]: nil}); err != nil {
					out.Error(err)
					return
				}
				continue
			}
			out.Error("unsupported entity data action")
			return
		}
		out.Printf("Updated data for %d entity(s)", len(entities))
		return
	}
	if target != "block" || len(words) < 5 {
		out.Error("unsupported data target")
		return
	}
	vec, err := cmd.ResolveCoordinates(words[2:5], src)
	if err != nil {
		out.Error(err)
		return
	}
	pos, used := cube.PosFromVec3(vec), 3
	b := tx.Block(pos)
	actor, ok := b.(world.NBTer)
	if action == "get" {
		if !ok {
			out.Error("block has no data")
			return
		}
		tag := actor.EncodeNBT()
		if used < len(words[2:]) {
			v, exists := nbtPath(tag, words[2+used])
			if !exists {
				out.Error("missing data path " + words[2+used])
				return
			}
			out.SetResult(dataResult(v))
			out.Printf("%s", formatData(v))
			return
		}
		out.SetResult(dataResult(tag))
		out.Printf("%s", formatData(tag))
		return
	}
	if !ok {
		out.Error("block has no data")
		return
	}
	if action == "remove" {
		if used >= len(words[2:]) {
			out.Error("data remove requires a path")
			return
		}
		tag := actor.EncodeNBT()
		delete(tag, words[2+used])
		tx.SetBlockEntity(pos, actor.DecodeNBT(tag).(world.Block))
		out.Printf("Removed data from block at %s", pos)
		return
	}
	if action == "merge" {
		if used >= len(words[2:]) {
			out.Error("data merge requires a JSON object")
			return
		}
		var merge map[string]any
		if err := json.Unmarshal([]byte(strings.Join(words[2+used:], " ")), &merge); err != nil {
			out.Error(err)
			return
		}
		tag := actor.EncodeNBT()
		for k, v := range merge {
			tag[k] = v
		}
		decoded, ok := actor.DecodeNBT(tag).(world.Block)
		if !ok {
			out.Error("block data cannot be merged")
			return
		}
		tx.SetBlockEntity(pos, decoded)
		out.Printf("Merged data into block at %s", pos)
		return
	}
	out.Error("unsupported data action")
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
