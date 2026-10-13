package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// executeCommand ports Java ExecuteCommand. Modifiers fork or narrow the
// execution context; `run` executes a nested command with that context.
type executeCommand struct {
	opGate
	Args cmd.Varargs
}

var errExecuteSyntax = errors.New("invalid execute syntax")

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
		if e, ok := src.(world.Entity); ok {
			ctx.Executor, ctx.Rotation = e, e.Rotation()
		}
		if inner, ok := src.(cmd.ContextSource); ok {
			src = inner.Source
		}
	} else if inner, ok := src.(cmd.ContextSource); ok {
		src = inner.Source
	}
	if ctx.World == nil {
		ctx.World = tx.World()
	}
	forks := &executeForks{limit: tx.World().GameRuleInt("max_command_forks"), counts: map[int]int{}}
	executeChain(src, ctx, words, out, tx, forks)
}

// executeForks ports ExecutionContext's fork limit: when the contexts created
// at one stage of an execute chain reach max_command_forks, the command fails
// with "Maximum number of contexts reached" and no further branch runs.
type executeForks struct {
	limit    int
	counts   map[int]int
	exceeded bool
}

// admit records n contexts forked at the stage with remaining words.
func (f *executeForks) admit(out *cmd.Output, remaining, n int) bool {
	if f.exceeded {
		return false
	}
	if f.counts[remaining]+n >= f.limit {
		f.exceeded = true
		out.Errorf("Maximum number of contexts (%d) reached", f.limit)
		return false
	}
	f.counts[remaining] += n
	return true
}

// executeChain evaluates the remaining modifiers for one context branch.
func executeChain(src cmd.Source, ctx cmd.ExecutionContext, words []string, out *cmd.Output, tx *world.Tx, forks *executeForks) {
	at := func() cmd.Source { return cmd.WithContext(src, ctx) }
	for len(words) > 0 {
		switch words[0] {
		case "as", "at":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			targets, err := cmd.ResolveTargets(words[1], at(), tx)
			if err != nil {
				out.Error(err)
				return
			}
			if !forks.admit(out, len(words), len(targets)) {
				return
			}
			for _, t := range targets {
				e, ok := t.(world.Entity)
				if !ok {
					continue
				}
				next := ctx
				if words[0] == "as" {
					next.Executor = e
				} else {
					next.Position, next.Rotation = e.Position(), e.Rotation()
				}
				executeChain(src, next, words[2:], out, tx, forks)
			}
			return
		case "positioned":
			if len(words) >= 3 && words[1] == "as" {
				targets, err := cmd.ResolveTargets(words[2], at(), tx)
				if err != nil || len(targets) == 0 {
					return
				}
				if !forks.admit(out, len(words), len(targets)) {
					return
				}
				for _, t := range targets {
					next := ctx
					next.Position = t.Position()
					executeChain(src, next, words[3:], out, tx, forks)
				}
				return
			}
			if len(words) < 4 {
				out.Error(errExecuteSyntax)
				return
			}
			pos, err := cmd.ResolveCoordinates(words[1:4], at())
			if err != nil {
				out.Error(err)
				return
			}
			ctx.Position, ctx.Anchor = pos, cmd.Feet
			words = words[4:]
		case "rotated":
			if len(words) >= 3 && words[1] == "as" {
				targets, err := cmd.ResolveTargets(words[2], at(), tx)
				if err != nil || len(targets) == 0 {
					return
				}
				if !forks.admit(out, len(words), len(targets)) {
					return
				}
				for _, t := range targets {
					if e, ok := t.(world.Entity); ok {
						next := ctx
						next.Rotation = e.Rotation()
						executeChain(src, next, words[3:], out, tx, forks)
					}
				}
				return
			}
			if len(words) < 3 {
				out.Error(errExecuteSyntax)
				return
			}
			yaw, pitch, err := parseRotation(words[1:3], ctx.Rotation)
			if err != nil {
				out.Error(err)
				return
			}
			ctx.Rotation = cube.Rotation{yaw, pitch}
			words = words[3:]
		case "facing":
			if len(words) >= 4 && words[1] == "entity" {
				targets, err := cmd.ResolveTargets(words[2], at(), tx)
				if err != nil || len(targets) == 0 {
					return
				}
				if !forks.admit(out, len(words), len(targets)) {
					return
				}
				for _, t := range targets {
					look := t.Position()
					if words[3] == "eyes" {
						if eyed, ok := t.(interface{ EyeHeight() float64 }); ok {
							look[1] += eyed.EyeHeight()
						}
					}
					next := ctx
					next.Rotation = rotationTowards(anchorPosition(ctx), look)
					executeChain(src, next, words[4:], out, tx, forks)
				}
				return
			}
			if len(words) < 4 {
				out.Error(errExecuteSyntax)
				return
			}
			pos, err := cmd.ResolveCoordinates(words[1:4], at())
			if err != nil {
				out.Error(err)
				return
			}
			ctx.Rotation = rotationTowards(anchorPosition(ctx), pos)
			words = words[4:]
		case "align":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			for _, axis := range words[1] {
				if axis < 'x' || axis > 'z' {
					out.Error("invalid align axis")
					return
				}
				ctx.Position[axis-'x'] = math.Floor(ctx.Position[axis-'x'])
			}
			words = words[2:]
		case "anchored":
			if len(words) < 2 || words[1] != "eyes" && words[1] != "feet" {
				out.Error("execute anchored requires feet or eyes")
				return
			}
			ctx.Anchor = cmd.Feet
			if words[1] == "eyes" {
				ctx.Anchor = cmd.Eyes
			}
			words = words[2:]
		case "in":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			target, ok := dimensionWorld(words[1])
			if !ok {
				out.Errorf("Unknown dimension '%s'", words[1])
				return
			}
			if target == ctx.World {
				words = words[2:]
				continue
			}
			// CommandSourceStack.withLevel scales X/Z by the coordinate scales.
			scale := dimensionScale(ctx.World.Dimension()) / dimensionScale(target.Dimension())
			ctx.Position[0], ctx.Position[2] = ctx.Position[0]*scale, ctx.Position[2]*scale
			ctx.World = target
			rest := words[2:]
			next := ctx
			// The remaining chain needs the target world's transaction.
			target.Exec(func(other *world.Tx) { executeChain(src, next, rest, out, other, forks) })
			return
		case "on":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			related := relatedEntities(ctx.Executor, words[1], tx)
			if !forks.admit(out, len(words), len(related)) {
				return
			}
			for _, e := range related {
				next := ctx
				next.Executor = e
				executeChain(src, next, words[2:], out, tx, forks)
			}
			return
		case "summon":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			spawn := tx.World().EntityRegistry().Config().SpawnMob
			id := words[1]
			if !strings.Contains(id, ":") {
				id = "minecraft:" + id
			}
			if spawn == nil {
				out.Errorf("Unable to summon entity")
				return
			}
			h := spawn(id, world.EntitySpawnOpts{Position: ctx.Position, SpawnReason: "command"})
			if h == nil {
				out.Errorf("Unable to summon entity")
				return
			}
			ctx.Executor = tx.AddEntity(h)
			words = words[2:]
		case "store":
			sink, used, err := executeStoreSink(words[1:], src, ctx, tx)
			if err != nil {
				out.Error(err)
				return
			}
			src = &storeSource{Source: src, store: sink}
			words = words[1+used:]
		case "if", "unless":
			if len(words) < 2 {
				out.Error(errExecuteSyntax)
				return
			}
			count, used, err := executeCondition(words[1:], src, ctx, tx)
			if err != nil {
				out.Error(err)
				return
			}
			pass := count > 0
			if words[0] == "unless" {
				pass, count = !pass, 1
			}
			words = words[1+used:]
			if len(words) == 0 {
				// A trailing condition reports its result, as Java does.
				if !pass {
					out.Error("Test failed")
					return
				}
				out.SetResult(count)
				out.Printf("Test passed")
				if store, ok := src.(*storeSource); ok {
					store.store(true, count)
				}
				return
			}
			if !pass {
				return
			}
		case "run":
			if len(words) < 2 {
				out.Error("execute run requires a command")
				return
			}
			command, ok := cmd.ByAlias(strings.TrimPrefix(words[1], "/"))
			if !ok {
				out.Errort(cmd.MessageUnknown, words[1])
				return
			}
			command.Execute(strings.Join(words[2:], " "), cmd.WithContext(src, ctx), tx)
			return
		default:
			out.Errorf("Unknown execute subcommand '%s'", words[0])
			return
		}
	}
	out.Error("execute chain must end in run or a condition")
}

func anchorPosition(ctx cmd.ExecutionContext) mgl64.Vec3 {
	pos := ctx.Position
	if ctx.Anchor == cmd.Eyes {
		if eyed, ok := ctx.Executor.(interface{ EyeHeight() float64 }); ok {
			pos[1] += eyed.EyeHeight()
		}
	}
	return pos
}

func dimensionWorld(name string) (*world.World, bool) {
	if commandServer == nil {
		return nil, false
	}
	var w *world.World
	switch strings.TrimPrefix(name, "minecraft:") {
	case "overworld":
		w = commandServer.World()
	case "the_nether":
		w = commandServer.Nether()
	case "the_end":
		w = commandServer.End()
	}
	return w, w != nil
}

// dimensionScale is DimensionType.coordinateScale: 8 for the Nether.
func dimensionScale(d world.Dimension) float64 {
	if d == world.Nether {
		return 8
	}
	return 1
}

// relatedEntities ports EntityRelation for the relations Bedrock entities
// track: vehicle, passengers, controller and owner/origin.
func relatedEntities(e world.Entity, relation string, tx *world.Tx) []world.Entity {
	if e == nil {
		return nil
	}
	resolve := func(h *world.EntityHandle) []world.Entity {
		if h == nil {
			return nil
		}
		if other, ok := h.Entity(tx); ok {
			return []world.Entity{other}
		}
		return nil
	}
	switch relation {
	case "vehicle":
		if rider, ok := e.(entity.Rider); ok {
			return resolve(rider.Riding())
		}
	case "passengers", "controller":
		linker, ok := e.(entity.Linker)
		if !ok {
			return nil
		}
		var result []world.Entity
		for _, link := range linker.Links() {
			if link.Ridden == e.H() {
				result = append(result, resolve(link.Rider)...)
			}
		}
		if relation == "controller" && len(result) > 1 {
			result = result[:1]
		}
		return result
	case "owner", "origin":
		if owned, ok := e.(interface{ Owner() *world.EntityHandle }); ok {
			return resolve(owned.Owner())
		}
		if ent, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
			if owned, ok := ent.Behaviour().(interface{ Owner() *world.EntityHandle }); ok {
				return resolve(owned.Owner())
			}
		}
	}
	return nil
}

// storeSource intercepts the nested command's output to store its result or
// success, as ExecuteCommand's result consumers do.
type storeSource struct {
	cmd.Source
	store func(success bool, result int)
}

func (s *storeSource) SendCommandOutput(o *cmd.Output) {
	s.store(o.Success(), o.Result())
	s.Source.SendCommandOutput(o)
}

// executeStoreSink parses `store (result|success) <destination>` for score,
// bossbar and storage destinations.
func executeStoreSink(words []string, src cmd.Source, ctx cmd.ExecutionContext, tx *world.Tx) (func(bool, int), int, error) {
	if len(words) < 2 || words[0] != "result" && words[0] != "success" {
		return nil, 0, errExecuteSyntax
	}
	storeSuccess := words[0] == "success"
	value := func(success bool, result int) int {
		if storeSuccess {
			if success {
				return 1
			}
			return 0
		}
		return result
	}
	switch words[1] {
	case "score":
		if len(words) < 4 {
			return nil, 0, errExecuteSyntax
		}
		m := tx.World().Scoreboard()
		holders, err := scoreHolders(words[2], cmd.WithContext(src, ctx), tx, m)
		if err != nil {
			return nil, 0, err
		}
		objective := words[3]
		return func(success bool, result int) {
			for _, holder := range holders {
				_ = m.SetScore(objective, holder, value(success, result))
			}
			refreshScoreboard(tx, m)
		}, 4, nil
	case "bossbar":
		if len(words) < 4 || words[3] != "value" && words[3] != "max" {
			return nil, 0, errExecuteSyntax
		}
		m := tx.World().Scoreboard()
		id, property := words[2], words[3]
		return func(success bool, result int) {
			if m.ModifyBossBar(id, property, strconv.Itoa(value(success, result))) == nil {
				refreshBossBar(tx, m, id)
			}
		}, 4, nil
	case "storage":
		if len(words) < 6 {
			return nil, 0, errExecuteSyntax
		}
		key, path, kind := words[2], words[3], words[4]
		scale, err := strconv.ParseFloat(words[5], 64)
		if err != nil {
			return nil, 0, err
		}
		return func(success bool, result int) {
			storeDataValue(tx, key, path, kind, float64(value(success, result))*scale)
		}, 6, nil
	}
	return nil, 0, fmt.Errorf("unsupported store destination '%s'", words[1])
}

// executeCondition evaluates an if/unless predicate and returns Java's match
// count (entities matched, blocks matched or 1/0) and the words consumed.
func executeCondition(words []string, src cmd.Source, ctx cmd.ExecutionContext, tx *world.Tx) (int, int, error) {
	if len(words) == 0 {
		return 0, 0, errExecuteSyntax
	}
	at := cmd.WithContext(src, ctx)
	boolCount := func(ok bool) int {
		if ok {
			return 1
		}
		return 0
	}
	blockPos := func(args []string) (cube.Pos, error) {
		pos, err := cmd.ResolveCoordinates(args, at)
		return cube.PosFromVec3(pos), err
	}
	switch words[0] {
	case "entity":
		if len(words) < 2 {
			return 0, 0, errExecuteSyntax
		}
		targets, err := cmd.ResolveTargets(words[1], at, tx)
		if err != nil {
			return 0, 2, nil
		}
		return len(targets), 2, nil
	case "loaded":
		if len(words) < 4 {
			return 0, 0, errExecuteSyntax
		}
		pos, err := blockPos(words[1:4])
		if err != nil {
			return 0, 0, err
		}
		_, loaded := tx.BlockLoaded(pos)
		return boolCount(loaded), 4, nil
	case "block":
		if len(words) < 5 {
			return 0, 0, errExecuteSyntax
		}
		pos, err := blockPos(words[1:4])
		if err != nil {
			return 0, 0, err
		}
		match, err := blockMatches(tx, pos, words[4])
		return boolCount(match), 5, err
	case "dimension":
		if len(words) < 2 {
			return 0, 0, errExecuteSyntax
		}
		w, ok := dimensionWorld(words[1])
		return boolCount(ok && w == ctx.World), 2, nil
	case "biome":
		if len(words) < 5 {
			return 0, 0, errExecuteSyntax
		}
		pos, err := blockPos(words[1:4])
		if err != nil {
			return 0, 0, err
		}
		want := strings.TrimPrefix(words[4], "minecraft:")
		return boolCount(strings.TrimPrefix(tx.Biome(pos).String(), "minecraft:") == want), 5, nil
	case "blocks":
		if len(words) < 11 {
			return 0, 0, errExecuteSyntax
		}
		a, err := blockPos(words[1:4])
		if err != nil {
			return 0, 0, err
		}
		b, err := blockPos(words[4:7])
		if err != nil {
			return 0, 0, err
		}
		dest, err := blockPos(words[7:10])
		if err != nil {
			return 0, 0, err
		}
		masked := words[10] == "masked"
		lo, hi := cube.Min(a, b), cube.Max(a, b)
		if (hi[0]-lo[0]+1)*(hi[1]-lo[1]+1)*(hi[2]-lo[2]+1) > tx.World().GameRuleInt("max_block_modifications") {
			return 0, 0, fmt.Errorf("too many blocks in the specified area")
		}
		count := 0
		for x := lo[0]; x <= hi[0]; x++ {
			for y := lo[1]; y <= hi[1]; y++ {
				for z := lo[2]; z <= hi[2]; z++ {
					p := cube.Pos{x, y, z}
					source := tx.Block(p)
					if masked && isAir(source) {
						continue
					}
					if tx.Block(dest.Add(p.Sub(lo))) != source {
						return 0, 11, nil
					}
					count++
				}
			}
		}
		return count, 11, nil
	case "score":
		if len(words) < 5 {
			return 0, 0, errExecuteSyntax
		}
		m := tx.World().Scoreboard()
		holders, err := scoreHolders(words[1], at, tx, m)
		if err != nil || len(holders) != 1 {
			return 0, 0, errExecuteSyntax
		}
		value, exists := m.Score(words[2], holders[0])
		if words[3] == "matches" {
			return boolCount(exists && scoreRangeMatches(value, words[4])), 5, nil
		}
		if len(words) < 6 {
			return 0, 0, errExecuteSyntax
		}
		others, err := scoreHolders(words[4], at, tx, m)
		if err != nil || len(others) != 1 {
			return 0, 0, errExecuteSyntax
		}
		other, ok := m.Score(words[5], others[0])
		if !exists || !ok {
			return 0, 6, nil
		}
		var pass bool
		switch words[3] {
		case "=":
			pass = value == other
		case ">":
			pass = value > other
		case "<":
			pass = value < other
		case ">=":
			pass = value >= other
		case "<=":
			pass = value <= other
		default:
			return 0, 0, errExecuteSyntax
		}
		return boolCount(pass), 6, nil
	}
	return 0, 0, fmt.Errorf("unknown execute condition '%s'", words[0])
}

// blockMatches compares the full state when properties are given and only the
// block identifier otherwise, as BlockPredicateArgument does.
func blockMatches(tx *world.Tx, pos cube.Pos, spec string) (bool, error) {
	name, props, err := parseCommandBlockSpec(spec)
	if err != nil {
		return false, err
	}
	current, _ := tx.Block(pos).EncodeBlock()
	if len(props) == 0 {
		return current == name, nil
	}
	want, err := commandBlock(tx.World(), spec)
	if err != nil {
		return false, err
	}
	return tx.Block(pos) == want, nil
}

func parseRotation(words []string, base cube.Rotation) (float64, float64, error) {
	values := [2]float64{}
	for i, word := range words[:2] {
		text := strings.TrimPrefix(word, "~")
		value := 0.0
		if text != "" {
			var err error
			if value, err = strconv.ParseFloat(text, 64); err != nil {
				return 0, 0, err
			}
		}
		if strings.HasPrefix(word, "~") {
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

func scoreRangeMatches(value int, text string) bool {
	lo, hi, isRange := strings.Cut(text, "..")
	if !isRange {
		n, err := strconv.Atoi(text)
		return err == nil && value == n
	}
	minValue, maxValue := math.MinInt, math.MaxInt
	if lo != "" {
		n, err := strconv.Atoi(lo)
		if err != nil {
			return false
		}
		minValue = n
	}
	if hi != "" {
		n, err := strconv.Atoi(hi)
		if err != nil {
			return false
		}
		maxValue = n
	}
	return value >= minValue && value <= maxValue
}

func refreshBossBar(tx *world.Tx, m *scoreboard.Model, id string) {
	if bar, ok := m.BossBar(id); ok {
		sendBossBar(tx, bar)
	}
}

// storeDataValue writes a stored number into command storage at a dotted
// path, converting it to the requested NBT numeric type like Java's
// StoreResult: integral types truncate, float and double keep fractions.
func storeDataValue(tx *world.Tx, key, path, kind string, v float64) {
	namespace := "java:storage:" + key
	current := map[string]any{}
	if data, err := tx.World().LoadWorldData(namespace); err == nil {
		_ = json.Unmarshal(data, &current)
	}
	var stored any = v
	switch kind {
	case "byte", "short", "int", "long":
		stored = int64(v)
	}
	parts := strings.Split(path, ".")
	node := current
	for _, part := range parts[:len(parts)-1] {
		child, ok := node[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[part] = child
		}
		node = child
	}
	node[parts[len(parts)-1]] = stored
	if encoded, err := json.Marshal(current); err == nil {
		_ = tx.World().SaveWorldData(namespace, encoded)
	}
}
