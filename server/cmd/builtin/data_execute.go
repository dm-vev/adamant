package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/scoreboard"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// executeCommand ports Java ExecuteCommand. Modifiers fork or narrow the
// execution context; `run` executes a nested command with that context.
type executeCommand struct {
	gameMaster
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
			ctx.Executor, ctx.ExecutorHandle, ctx.Rotation = e, e.H(), e.Rotation()
		}
	} else {
		ctx.ExecutorHandle = ctx.EntityHandle()
	}
	src = unwrapContextSource(src)
	if ctx.World == nil {
		ctx.World = tx.World()
	}
	eyeHeight := executorEyeHeight(ctx.Executor)
	forks := &executeForks{limit: tx.World().GameRuleInt("max_command_forks"), counts: map[int]int{}}
	if ctx.World != tx.World() {
		next := stripExecuteEntity(ctx)
		cmd.InWorld(src, out, tx, ctx.World, func(wSrc cmd.Source, wOut *cmd.Output, wTx *world.Tx) {
			executeChain(unwrapContextSource(wSrc), resolveExecuteCtx(next, wTx), eyeHeight, nil, words, wOut, wTx, forks)
		})
		return
	}
	executeChain(src, ctx, eyeHeight, nil, words, out, tx, forks)
}

func unwrapContextSource(src cmd.Source) cmd.Source {
	for {
		inner, ok := src.(cmd.ContextSource)
		if !ok {
			return src
		}
		src = inner.Source
	}
}

func executorEyeHeight(e world.Entity) float64 {
	if eyed, ok := e.(interface{ EyeHeight() float64 }); ok {
		return eyed.EyeHeight()
	}
	return 0
}

func stripExecuteEntity(ctx cmd.ExecutionContext) cmd.ExecutionContext {
	ctx.ExecutorHandle = ctx.EntityHandle()
	ctx.Executor = nil
	return ctx
}

func resolveExecuteCtx(ctx cmd.ExecutionContext, tx *world.Tx) cmd.ExecutionContext {
	h := ctx.EntityHandle()
	ctx.ExecutorHandle = h
	ctx.Executor = nil
	if h != nil && tx != nil && h.World() == tx.World() {
		if e, ok := h.Entity(tx); ok {
			ctx.Executor = e
		}
	}
	return ctx
}

// executeForks ports ExecutionContext's fork limit: when the contexts created
// at one stage of an execute chain reach max_command_forks, the command fails
// with "Maximum number of contexts reached" and no further branch runs.
type executeForks struct {
	mu       sync.Mutex
	limit    int
	counts   map[int]int
	exceeded bool
}

// admit records n contexts forked at the stage with remaining words.
func (f *executeForks) admit(out *cmd.Output, remaining, n int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func anchorPosition(ctx cmd.ExecutionContext, eyeHeight float64) mgl64.Vec3 {
	pos := ctx.Position
	if ctx.Anchor == cmd.Eyes {
		if h := executorEyeHeight(ctx.Executor); h != 0 {
			pos[1] += h
		} else {
			pos[1] += eyeHeight
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

// storeSource intercepts the nested command's output (both synchronous and
// deferred via cmd.Later) to store its result or success, as ExecuteCommand's
// result consumers do.
type storeSource struct {
	cmd.Source
	store func(success bool, result int)
}

// Unwrap returns the source of the command, whose level the storeSource has.
func (s *storeSource) Unwrap() cmd.Source { return s.Source }

// OnCommandOutput is invoked by cmd.Later before delivering deferred output.
func (s *storeSource) OnCommandOutput(o *cmd.Output) {
	if o == nil || s.store == nil {
		return
	}
	s.store(o.Success(), o.Result())
}

func (s *storeSource) SendCommandOutput(o *cmd.Output) {
	s.OnCommandOutput(o)
	s.Source.SendCommandOutput(o)
}

// resolveExecuteStoreSink parses `store (result|success) <destination>` for
// score, bossbar and storage destinations, resolving score selectors across
// worlds when needed.
func resolveExecuteStoreSink(
	words []string, src cmd.Source, ctx cmd.ExecutionContext, out *cmd.Output, tx *world.Tx,
	then func(sink func(bool, int), used int, err error, nextSrc cmd.Source, nextTx *world.Tx),
) {
	if len(words) < 2 || words[0] != "result" && words[0] != "success" {
		then(nil, 0, errExecuteSyntax, src, tx)
		return
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
	storeWorld := tx.World()
	switch words[1] {
	case "score":
		if len(words) < 4 {
			then(nil, 0, errExecuteSyntax, src, tx)
			return
		}
		m := storeWorld.Scoreboard()
		objective := words[3]
		resolveScoreHolders(words[2], cmd.WithContext(src, ctx), out, tx, m, func(nSrc cmd.Source, _ *cmd.Output, nTx *world.Tx, holders []string, err error) {
			if err != nil {
				then(nil, 0, err, unwrapContextSource(nSrc), nTx)
				return
			}
			holders = slices.Clone(holders)
			sink := func(success bool, result int) {
				v := value(success, result)
				for _, holder := range holders {
					_ = m.SetScore(objective, holder, v)
				}
				storeWorld.Do(func(rtx *world.Tx) { refreshScoreboard(rtx, m) })
			}
			then(sink, 4, nil, unwrapContextSource(nSrc), nTx)
		})
	case "bossbar":
		if len(words) < 4 || words[3] != "value" && words[3] != "max" {
			then(nil, 0, errExecuteSyntax, src, tx)
			return
		}
		m := storeWorld.Scoreboard()
		id, property := words[2], words[3]
		sink := func(success bool, result int) {
			if m.ModifyBossBar(id, property, strconv.Itoa(value(success, result))) == nil {
				storeWorld.Do(func(rtx *world.Tx) { refreshBossBar(rtx, m, id) })
			}
		}
		then(sink, 4, nil, src, tx)
	case "storage":
		if len(words) < 6 {
			then(nil, 0, errExecuteSyntax, src, tx)
			return
		}
		key, path, kind := words[2], words[3], words[4]
		scale, err := cmd.ParseNumber(words[5], 64)
		if err != nil {
			then(nil, 0, err, src, tx)
			return
		}
		sink := func(success bool, result int) {
			storeDataValueWorld(storeWorld, key, path, kind, float64(value(success, result))*scale)
		}
		then(sink, 6, nil, src, tx)
	default:
		then(nil, 0, fmt.Errorf("unsupported store destination '%s'", words[1]), src, tx)
	}
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
	want, _, err := commandBlock(tx.World(), spec)
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
			if value, err = cmd.ParseNumber(text, 64); err != nil {
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
// path using tx's world.
func storeDataValue(tx *world.Tx, key, path, kind string, v float64) {
	storeDataValueWorld(tx.World(), key, path, kind, v)
}

// storeDataValueWorld writes a stored number into command storage at a dotted
// path, converting it to the requested NBT numeric type like Java's
// StoreResult: integral types truncate, float and double keep fractions.
func storeDataValueWorld(w *world.World, key, path, kind string, v float64) {
	if w == nil {
		return
	}
	namespace := "cmd:storage:" + key
	legacyNamespace := "java:storage:" + key
	current := map[string]any{}
	data, err := w.LoadWorldData(namespace)
	if errors.Is(err, leveldb.ErrNotFound) {
		data, err = w.LoadWorldData(legacyNamespace)
	}
	if err == nil {
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
		_ = w.SaveWorldData(namespace, encoded)
	}
}
