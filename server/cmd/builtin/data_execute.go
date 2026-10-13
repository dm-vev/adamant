package builtin

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type executeCommand struct {
	opGate
	Args cmd.Varargs
}

func (c executeCommand) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	if tx == nil {
		out.Error("execute requires a world")
		return
	}
	words := strings.Fields(string(c.Args))
	if len(words) == 0 {
		out.Error("execute requires a subcommand")
		return
	}
	ctx, ok := cmd.ExecutionContextOf(src)
	if !ok {
		ctx = cmd.ExecutionContext{Position: src.Position(), World: tx.World(), Anchor: cmd.Feet}
		if executor, ok := src.(world.Entity); ok {
			ctx.Executor, ctx.Rotation = executor, executor.Rotation()
		}
	}
	for len(words) > 0 {
		switch words[0] {
		case "as":
			if len(words) < 2 {
				out.Error("execute as requires a target")
				return
			}
			entities := selectEntities(words[1], tx, ctx.Executor)
			if len(entities) == 0 {
				return
			}
			words = words[2:]
			for _, entity := range entities {
				c.executeWords(cmd.WithContext(src, executeContextAt(ctx, entity)), words, out, tx)
			}
			return
		case "at":
			if len(words) < 2 {
				out.Error("execute at requires a target")
				return
			}
			entities := selectEntities(words[1], tx, ctx.Executor)
			if len(entities) == 0 {
				return
			}
			words = words[2:]
			for _, entity := range entities {
				nested := cmd.WithContext(src, executeContextAt(ctx, entity))
				c.executeWords(nested, words, out, tx)
			}
			return
		case "positioned":
			if len(words) >= 3 && words[1] == "as" {
				targets := selectEntities(words[2], tx, ctx.Executor)
				if len(targets) == 0 {
					return
				}
				ctx.Position = targets[0].Position()
				words = words[3:]
				continue
			}
			if len(words) < 4 {
				out.Error("execute positioned requires coordinates or a target")
				return
			}
			x, y, z, err := parseCoords(words[1:4], ctx.Position)
			if err != nil {
				out.Error(err)
				return
			}
			ctx.Position = mgl64.Vec3{x, y, z}
			words = words[4:]
		case "rotated":
			if len(words) >= 3 && words[1] == "as" {
				targets := selectEntities(words[2], tx, ctx.Executor)
				if len(targets) == 0 {
					return
				}
				ctx.Rotation = targets[0].Rotation()
				words = words[3:]
				continue
			}
			if len(words) < 3 {
				out.Error("execute rotated requires yaw and pitch or a target")
				return
			}
			yaw, pitch, err := parseRotation(words[1:3], ctx.Rotation)
			if err != nil {
				out.Error(err)
				return
			}
			ctx.Rotation = cube.Rotation{yaw, pitch}
			words = words[3:]
		case "align":
			if len(words) < 2 {
				out.Error("execute align requires axes")
				return
			}
			for _, axis := range words[1] {
				switch axis {
				case 'x':
					ctx.Position[0] = math.Floor(ctx.Position[0])
				case 'y':
					ctx.Position[1] = math.Floor(ctx.Position[1])
				case 'z':
					ctx.Position[2] = math.Floor(ctx.Position[2])
				default:
					out.Error("invalid align axis")
					return
				}
			}
			words = words[2:]
		case "facing":
			if len(words) < 4 {
				out.Error("execute facing requires a target or coordinates")
				return
			}
			if words[1] == "entity" {
				if len(words) < 4 {
					out.Error("execute facing entity requires a target and anchor")
					return
				}
				targets := selectEntities(words[2], tx, ctx.Executor)
				if len(targets) == 0 {
					return
				}
				words = words[4:]
				ctx.Rotation = rotationTowards(ctx.Position, targets[0].Position())
			} else {
				x, y, z, err := parseCoords(words[1:4], ctx.Position)
				if err != nil {
					out.Error(err)
					return
				}
				ctx.Rotation = rotationTowards(ctx.Position, mgl64.Vec3{x, y, z})
				words = words[4:]
			}
		case "anchored":
			if len(words) < 2 {
				out.Error("execute anchored requires feet or eyes")
				return
			}
			if words[1] == "eyes" {
				ctx.Anchor = cmd.Eyes
			} else if words[1] == "feet" {
				ctx.Anchor = cmd.Feet
			} else {
				out.Error("invalid anchor")
				return
			}
			words = words[2:]
		case "if", "unless":
			if len(words) < 2 {
				out.Error("execute condition requires a predicate")
				return
			}
			negate := words[0] == "unless"
			pass, used, err := executeCondition(words[1:], ctx, tx)
			if err != nil {
				out.Error(err)
				return
			}
			if negate {
				pass = !pass
			}
			if !pass {
				return
			}
			words = words[1+used:]
		case "run":
			if len(words) < 2 {
				out.Error("execute run requires a command")
				return
			}
			command, ok := cmd.ByAlias(words[1])
			if !ok {
				out.Error("unknown command: " + words[1])
				return
			}
			command.Execute(strings.Join(words[2:], " "), cmd.WithContext(src, ctx), tx)
			return
		default:
			out.Error("unsupported execute subcommand: " + words[0])
			return
		}
	}
	out.Error("execute chain must end in run")
}

func executeContextAt(ctx cmd.ExecutionContext, entity world.Entity) cmd.ExecutionContext {
	ctx.Executor, ctx.Position, ctx.Rotation = entity, entity.Position(), entity.Rotation()
	return ctx
}

func selectEntities(selector string, tx *world.Tx, self world.Entity) []world.Entity {
	if selector == "@s" {
		if self != nil {
			return []world.Entity{self}
		}
		return nil
	}
	var result []world.Entity
	if selector == "@e" || selector == "@a" || selector == "@p" {
		for entity := range tx.Entities() {
			if selector != "@a" || entity.H().Type().EncodeEntity() == "minecraft:player" {
				result = append(result, entity)
			}
		}
		if selector == "@p" && len(result) > 1 {
			return result[:1]
		}
	}
	return result
}

func executeCondition(words []string, ctx cmd.ExecutionContext, tx *world.Tx) (bool, int, error) {
	invalid := errors.New("invalid execute condition")
	if len(words) == 0 {
		return false, 0, invalid
	}
	switch words[0] {
	case "entity":
		if len(words) < 2 {
			return false, 0, invalid
		}
		return len(selectEntities(words[1], tx, ctx.Executor)) > 0, 2, nil
	case "loaded":
		if len(words) < 4 {
			return false, 0, invalid
		}
		x, y, z, err := parseCoords(words[1:4], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		_, loaded := tx.BlockLoaded(cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))})
		return loaded, 4, nil
	case "block":
		if len(words) < 5 {
			return false, 0, invalid
		}
		x, y, z, err := parseCoords(words[1:4], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		name, _ := tx.Block(cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}).EncodeBlock()
		return name == words[4], 5, nil
	case "dimension":
		if len(words) < 2 {
			return false, 0, invalid
		}
		id, ok := world.DimensionID(tx.World().Dimension())
		names := []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"}
		if ok && id >= 0 && id < len(names) {
			return names[id] == words[1] || strconv.Itoa(id) == words[1], 2, nil
		}
		return false, 2, nil
	case "biome":
		if len(words) < 5 {
			return false, 0, invalid
		}
		x, y, z, err := parseCoords(words[1:4], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		return tx.Biome(cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}).String() == words[4], 5, nil
	case "blocks":
		if len(words) < 10 {
			return false, 0, invalid
		}
		x1, y1, z1, err := parseCoords(words[1:4], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		x2, y2, z2, err := parseCoords(words[4:7], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		dx, dy, dz, err := parseCoords(words[7:10], ctx.Position)
		if err != nil {
			return false, 0, err
		}
		a, b, d := cube.Pos{int(x1), int(y1), int(z1)}, cube.Pos{int(x2), int(y2), int(z2)}, cube.Pos{int(dx), int(dy), int(dz)}
		minPos, maxPos := cube.Pos{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2])}, cube.Pos{max(a[0], b[0]), max(a[1], b[1]), max(a[2], b[2])}
		matches := 0
		total := (maxPos[0] - minPos[0] + 1) * (maxPos[1] - minPos[1] + 1) * (maxPos[2] - minPos[2] + 1)
		for bx := minPos[0]; bx <= maxPos[0]; bx++ {
			for by := minPos[1]; by <= maxPos[1]; by++ {
				for bz := minPos[2]; bz <= maxPos[2]; bz++ {
					source := tx.Block(cube.Pos{bx, by, bz})
					target := tx.Block(cube.Pos{d[0] + bx - minPos[0], d[1] + by - minPos[1], d[2] + bz - minPos[2]})
					sn, _ := source.EncodeBlock()
					tn, _ := target.EncodeBlock()
					if sn == tn {
						matches++
					}
				}
			}
		}
		return matches == total, 10, nil
	case "score":
		if len(words) < 5 {
			return false, 0, invalid
		}
		holder := scoreHolder(words[1], tx, ctx.Executor)
		if holder == "" {
			return false, 0, nil
		}
		model := tx.World().Scoreboard()
		value, exists := model.Score(words[2], holder)
		if !exists {
			return false, 5, nil
		}
		if words[3] == "matches" {
			return scoreRangeMatches(value, words[4]), 5, nil
		}
		if len(words) < 7 {
			return false, 0, invalid
		}
		otherHolder := scoreHolder(words[5], tx, ctx.Executor)
		other, ok := model.Score(words[6], otherHolder)
		if !ok {
			return false, 7, nil
		}
		switch words[3] {
		case "=":
			return value == other, 7, nil
		case ">":
			return value > other, 7, nil
		case "<":
			return value < other, 7, nil
		case ">=":
			return value >= other, 7, nil
		case "<=":
			return value <= other, 7, nil
		default:
			return false, 0, invalid
		}
	default:
		return false, 0, invalid
	}
}

func (c executeCommand) executeWords(src cmd.Source, words []string, out *cmd.Output, tx *world.Tx) {
	executeCommand{Args: cmd.Varargs(strings.Join(words, " "))}.Run(src, out, tx)
}

func parseCoords(words []string, base mgl64.Vec3) (float64, float64, float64, error) {
	if len(words) < 3 {
		return 0, 0, 0, errors.New("expected three coordinates")
	}
	values := [3]float64{}
	for i, word := range words[:3] {
		rel := strings.HasPrefix(word, "~")
		text := strings.TrimPrefix(word, "~")
		value := 0.0
		if text != "" {
			var err error
			value, err = strconv.ParseFloat(text, 64)
			if err != nil {
				return 0, 0, 0, err
			}
		}
		if rel {
			value += base[i]
		}
		values[i] = value
	}
	return values[0], values[1], values[2], nil
}

func parseRotation(words []string, base cube.Rotation) (float64, float64, error) {
	values := [2]float64{}
	for i, word := range words[:2] {
		rel := strings.HasPrefix(word, "~")
		text := strings.TrimPrefix(word, "~")
		value := 0.0
		if text != "" {
			var err error
			value, err = strconv.ParseFloat(text, 64)
			if err != nil {
				return 0, 0, err
			}
		}
		if rel {
			value += base[i]
		}
		values[i] = value
	}
	return values[0], values[1], nil
}
func rotationTowards(from, to mgl64.Vec3) cube.Rotation {
	delta := to.Sub(from)
	horizontal := math.Hypot(delta[0], delta[2])
	return cube.Rotation{
		math.Atan2(-delta[0], delta[2]) * 180 / math.Pi,
		-math.Atan2(delta[1], horizontal) * 180 / math.Pi,
	}
}
func scoreHolder(name string, tx *world.Tx, self world.Entity) string {
	if !strings.HasPrefix(name, "@") {
		return name
	}
	targets := selectEntities(name, tx, self)
	if len(targets) == 0 {
		return ""
	}
	if named, ok := targets[0].(interface{ Name() string }); ok {
		return named.Name()
	}
	if named, ok := targets[0].(interface{ NameTag() string }); ok {
		return named.NameTag()
	}
	return ""
}

func scoreRangeMatches(value int, text string) bool {
	parts := strings.SplitN(text, "..", 2)
	if len(parts) == 1 {
		n, err := strconv.Atoi(text)
		return err == nil && value == n
	}
	minValue, maxValue := -int(^uint(0)>>1)-1, int(^uint(0)>>1)
	if parts[0] != "" {
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			return false
		}
		minValue = n
	}
	if parts[1] != "" {
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return false
		}
		maxValue = n
	}
	return value >= minValue && value <= maxValue
}
