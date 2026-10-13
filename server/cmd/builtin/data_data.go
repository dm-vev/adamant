package builtin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
		ctx, hasContext := cmd.ExecutionContextOf(src)
		if !hasContext {
			ctx.Executor, _ = src.(world.Entity)
		}
		entities := selectEntities(words[2], tx, ctx.Executor)
		if len(entities) == 0 {
			out.Error(cmd.MessageNoTargets)
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
					out.Printf("%s", formatData(v))
				} else {
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
	ctx, hasContext := cmd.ExecutionContextOf(src)
	base := src.Position()
	if hasContext {
		base = ctx.Position
	}
	pos, used, err := parseDataPos(words[2:], base)
	if err != nil {
		out.Error(err)
		return
	}
	b := tx.Block(pos)
	actor, ok := b.(world.NBTer)
	if action == "get" {
		if !ok {
			out.Error("block has no data")
			return
		}
		tag := actor.EncodeNBT()
		if used < len(words[2:]) {
			key := words[2+used]
			v, exists := tag[key]
			if !exists {
				out.Error("missing data key " + key)
				return
			}
			out.Printf("%s", formatData(v))
			return
		}
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

func parseDataPos(words []string, base mgl64.Vec3) (cube.Pos, int, error) {
	if len(words) < 3 {
		return cube.Pos{}, 0, fmt.Errorf("expected block coordinates")
	}
	x, y, z, err := parseCoords(words[:3], base)
	if err != nil {
		return cube.Pos{}, 0, err
	}
	return cube.Pos{int(x), int(y), int(z)}, 3, nil
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
