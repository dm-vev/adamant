package builtin

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// executeStep is one branch of an execute chain as its modifiers work on it:
// the source and the context they narrow and the words that are left of the
// chain. A modifier takes the words it needs off the front of words.
type executeStep struct {
	src   cmd.Source
	ctx   cmd.ExecutionContext
	words []string
	out   *cmd.Output
	tx    *world.Tx
	forks *executeForks
}

// executeModifier works on the step at the keyword that is the first of its
// words, as one of Java's execute subcommands does. It reports whether the
// chain goes on with the words and the context the step has now: false means
// the modifier ran the rest of the chain, or failed, and the branch ends.
type executeModifier func(s *executeStep) bool

// executeModifiers are the modifiers of the chain by keyword. They are set in
// init, as the chain they run for each branch looks them up.
var executeModifiers map[string]executeModifier

func init() {
	executeModifiers = map[string]executeModifier{
		"as":         executeAs,
		"at":         executeAt,
		"positioned": executePositioned,
		"rotated":    executeRotated,
		"facing":     executeFacing,
		"align":      executeAlign,
		"anchored":   executeAnchored,
		"in":         executeIn,
		"on":         executeOn,
		"summon":     executeSummon,
		"store":      executeStore,
		"if":         func(s *executeStep) bool { return executeConditional(s, false) },
		"unless":     func(s *executeStep) bool { return executeConditional(s, true) },
		"run":        executeRun,
	}
}

// executeChain evaluates the remaining modifiers for one context branch.
func executeChain(src cmd.Source, ctx cmd.ExecutionContext, words []string, out *cmd.Output, tx *world.Tx, forks *executeForks) {
	s := &executeStep{src: src, ctx: ctx, words: words, out: out, tx: tx, forks: forks}
	for len(s.words) > 0 {
		modifier, ok := executeModifiers[s.words[0]]
		if !ok {
			out.Errorf("Unknown execute subcommand '%s'", s.words[0])
			return
		}
		if !modifier(s) {
			return
		}
	}
	out.Error("execute chain must end in run or a condition")
}

// at is the source with the context of the step.
func (s *executeStep) at() cmd.Source { return cmd.WithContext(s.src, s.ctx) }

// fork runs the chain that is left, rest, for a branch with ctx.
func (s *executeStep) fork(ctx cmd.ExecutionContext, rest []string) {
	executeChain(s.src, ctx, rest, s.out, s.tx, s.forks)
}

// forkTargets resolves selector for the branches of a modifier that forks
// per target, "as" and "at" among them, admits them as contexts, and runs the
// rest of the chain for each with the context that narrow gives, unless it
// reports false for the target. It reports false: the branch ends. A
// selector that fails to resolve is an error for the modifiers that say so
// and silent for the others.
func (s *executeStep) forkTargets(selector string, silent bool, rest []string, narrow func(next *cmd.ExecutionContext, t cmd.Target) bool) bool {
	targets, err := cmd.ResolveTargets(selector, s.at(), s.tx)
	if silent && (err != nil || len(targets) == 0) {
		return false
	}
	if err != nil {
		s.out.Error(err)
		return false
	}
	if !s.forks.admit(s.out, len(s.words), len(targets)) {
		return false
	}
	for _, t := range targets {
		next := s.ctx
		if narrow(&next, t) {
			s.fork(next, rest)
		}
	}
	return false
}

// executeAs is `as <targets>`: the executor becomes each of the targets.
func executeAs(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	return s.forkTargets(s.words[1], false, s.words[2:], func(next *cmd.ExecutionContext, t cmd.Target) bool {
		e, ok := t.(world.Entity)
		if ok {
			next.Executor = e
		}
		return ok
	})
}

// executeAt is `at <targets>`: the position and rotation become those of each
// of the targets.
func executeAt(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	return s.forkTargets(s.words[1], false, s.words[2:], func(next *cmd.ExecutionContext, t cmd.Target) bool {
		e, ok := t.(world.Entity)
		if ok {
			next.Position, next.Rotation = e.Position(), e.Rotation()
		}
		return ok
	})
}

// executePositioned is `positioned <x> <y> <z>` and `positioned as <targets>`.
func executePositioned(s *executeStep) bool {
	if len(s.words) >= 3 && s.words[1] == "as" {
		return s.forkTargets(s.words[2], true, s.words[3:], func(next *cmd.ExecutionContext, t cmd.Target) bool {
			next.Position = t.Position()
			return true
		})
	}
	if len(s.words) < 4 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	pos, err := cmd.ResolveCoordinates(s.words[1:4], s.at())
	if err != nil {
		s.out.Error(err)
		return false
	}
	s.ctx.Position, s.ctx.Anchor = pos, cmd.Feet
	s.words = s.words[4:]
	return true
}

// executeRotated is `rotated <yaw> <pitch>` and `rotated as <targets>`.
func executeRotated(s *executeStep) bool {
	if len(s.words) >= 3 && s.words[1] == "as" {
		return s.forkTargets(s.words[2], true, s.words[3:], func(next *cmd.ExecutionContext, t cmd.Target) bool {
			e, ok := t.(world.Entity)
			if ok {
				next.Rotation = e.Rotation()
			}
			return ok
		})
	}
	if len(s.words) < 3 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	yaw, pitch, err := parseRotation(s.words[1:3], s.ctx.Rotation)
	if err != nil {
		s.out.Error(err)
		return false
	}
	s.ctx.Rotation = cube.Rotation{yaw, pitch}
	s.words = s.words[3:]
	return true
}

// executeFacing is `facing <x> <y> <z>` and `facing entity <targets>
// (feet|eyes)`.
func executeFacing(s *executeStep) bool {
	if len(s.words) >= 4 && s.words[1] == "entity" {
		anchor := s.words[3]
		return s.forkTargets(s.words[2], true, s.words[4:], func(next *cmd.ExecutionContext, t cmd.Target) bool {
			look := t.Position()
			if anchor == "eyes" {
				if eyed, ok := t.(interface{ EyeHeight() float64 }); ok {
					look[1] += eyed.EyeHeight()
				}
			}
			next.Rotation = rotationTowards(anchorPosition(s.ctx), look)
			return true
		})
	}
	if len(s.words) < 4 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	pos, err := cmd.ResolveCoordinates(s.words[1:4], s.at())
	if err != nil {
		s.out.Error(err)
		return false
	}
	s.ctx.Rotation = rotationTowards(anchorPosition(s.ctx), pos)
	s.words = s.words[4:]
	return true
}

// executeAlign is `align <axes>`: it floors the position on the axes.
func executeAlign(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	for _, axis := range s.words[1] {
		if axis < 'x' || axis > 'z' {
			s.out.Error("invalid align axis")
			return false
		}
		s.ctx.Position[axis-'x'] = math.Floor(s.ctx.Position[axis-'x'])
	}
	s.words = s.words[2:]
	return true
}

// executeAnchored is `anchored (feet|eyes)`.
func executeAnchored(s *executeStep) bool {
	if len(s.words) < 2 || s.words[1] != "eyes" && s.words[1] != "feet" {
		s.out.Error("execute anchored requires feet or eyes")
		return false
	}
	s.ctx.Anchor = cmd.Feet
	if s.words[1] == "eyes" {
		s.ctx.Anchor = cmd.Eyes
	}
	s.words = s.words[2:]
	return true
}

// executeIn is `in <dimension>`: the context moves to the dimension, which
// scales the position as Java's withLevel does.
func executeIn(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	target, ok := dimensionWorld(s.words[1])
	if !ok {
		s.out.Errorf("Unknown dimension '%s'", s.words[1])
		return false
	}
	if target == s.ctx.World {
		s.words = s.words[2:]
		return true
	}
	// CommandSourceStack.withLevel scales X/Z by the coordinate scales.
	scale := dimensionScale(s.ctx.World.Dimension()) / dimensionScale(target.Dimension())
	s.ctx.Position[0], s.ctx.Position[2] = s.ctx.Position[0]*scale, s.ctx.Position[2]*scale
	s.ctx.World = target
	rest := s.words[2:]
	next := s.ctx
	// The remaining chain needs the target world's transaction.
	target.Do(func(other *world.Tx) { executeChain(s.src, next, rest, s.out, other, s.forks) })
	return false
}

// executeOn is `on <relation>`: the executor becomes each of the entities
// that stand in the relation to it.
func executeOn(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	related := relatedEntities(s.ctx.Executor, s.words[1], s.tx)
	if !s.forks.admit(s.out, len(s.words), len(related)) {
		return false
	}
	for _, e := range related {
		next := s.ctx
		next.Executor = e
		s.fork(next, s.words[2:])
	}
	return false
}

// executeSummon is `summon <type>`: the executor becomes a new entity.
func executeSummon(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	spawn := s.tx.World().EntityRegistry().Config().SpawnMob
	id := s.words[1]
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	if spawn == nil {
		s.out.Errorf("Unable to summon entity")
		return false
	}
	h := spawn(id, world.EntitySpawnOpts{Position: s.ctx.Position, SpawnReason: "command"})
	if h == nil {
		s.out.Errorf("Unable to summon entity")
		return false
	}
	s.ctx.Executor = s.tx.AddEntity(h)
	s.words = s.words[2:]
	return true
}

// executeStore is `store (result|success) <destination>`: the output of what
// the chain ends in is stored.
func executeStore(s *executeStep) bool {
	sink, used, err := executeStoreSink(s.words[1:], s.src, s.ctx, s.tx)
	if err != nil {
		s.out.Error(err)
		return false
	}
	s.src = &storeSource{Source: s.src, store: sink}
	s.words = s.words[1+used:]
	return true
}

// executeConditional is `if <condition>` and, for unless, `unless
// <condition>`. A condition that ends the chain reports its result as Java
// does, and one that fails ends the branch.
func executeConditional(s *executeStep, unless bool) bool {
	if len(s.words) < 2 {
		s.out.Error(errExecuteSyntax)
		return false
	}
	count, used, err := executeCondition(s.words[1:], s.src, s.ctx, s.tx)
	if err != nil {
		s.out.Error(err)
		return false
	}
	pass := count > 0
	if unless {
		pass, count = !pass, 1
	}
	s.words = s.words[1+used:]
	if len(s.words) == 0 {
		// A trailing condition reports its result, as Java does.
		if !pass {
			s.out.Error("Test failed")
			return false
		}
		s.out.SetResult(count)
		s.out.Printf("Test passed")
		if store, ok := s.src.(*storeSource); ok {
			store.store(true, count)
		}
		return false
	}
	return pass
}

// executeRun is `run <command>`: the command runs with the context of the
// step.
func executeRun(s *executeStep) bool {
	if len(s.words) < 2 {
		s.out.Error("execute run requires a command")
		return false
	}
	command, ok := cmd.ByAlias(strings.TrimPrefix(s.words[1], "/"))
	if !ok {
		s.out.Errort(cmd.MessageUnknown, s.words[1])
		return false
	}
	line := strings.Join(s.words[2:], " ")
	if s.ctx.Executor != nil && s.ctx.Executor.H().World() != s.tx.World() {
		// Execute's level changes the command context, not the entity's
		// owning world. Run entity-mutating commands using a fresh Tx for
		// the executor's world while retaining the target-level context.
		src, ctx := s.src, s.ctx
		ctx.Executor.H().Do(func(entityTx *world.Tx, _ world.Entity) {
			command.Execute(line, cmd.WithContext(src, ctx), entityTx)
		})
		return false
	}
	command.Execute(line, cmd.WithContext(s.src, s.ctx), s.tx)
	return false
}

// executeTest is an if or unless predicate as its condition works on it: the
// words from its keyword on, the source and the context of the branch.
type executeTest struct {
	words []string
	src   cmd.Source
	ctx   cmd.ExecutionContext
	tx    *world.Tx
	at    cmd.Source
}

// executeConditionFn evaluates the predicate and returns Java's match count
// (entities matched, blocks matched or 1/0) and the words it consumed.
type executeConditionFn func(t executeTest) (count, used int, err error)

// executeConditions are the predicates of if and unless by keyword.
var executeConditions = map[string]executeConditionFn{
	"entity":    conditionEntity,
	"loaded":    conditionLoaded,
	"block":     conditionBlock,
	"dimension": conditionDimension,
	"biome":     conditionBiome,
	"blocks":    conditionBlocks,
	"score":     conditionScore,
}

// executeCondition evaluates an if/unless predicate and returns Java's match
// count (entities matched, blocks matched or 1/0) and the words consumed.
func executeCondition(words []string, src cmd.Source, ctx cmd.ExecutionContext, tx *world.Tx) (int, int, error) {
	if len(words) == 0 {
		return 0, 0, errExecuteSyntax
	}
	condition, ok := executeConditions[words[0]]
	if !ok {
		return 0, 0, fmt.Errorf("unknown execute condition '%s'", words[0])
	}
	return condition(executeTest{words: words, src: src, ctx: ctx, tx: tx, at: cmd.WithContext(src, ctx)})
}

// boolCount is Java's match count of a predicate that holds or does not.
func boolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

// blockPos resolves three coordinates to the position of a block.
func (t executeTest) blockPos(args []string) (cube.Pos, error) {
	pos, err := cmd.ResolveCoordinates(args, t.at)
	return cube.PosFromVec3(pos), err
}

// conditionEntity is `entity <targets>`: the number of the targets. A
// selector that fails counts none.
func conditionEntity(t executeTest) (int, int, error) {
	if len(t.words) < 2 {
		return 0, 0, errExecuteSyntax
	}
	targets, err := cmd.ResolveTargets(t.words[1], t.at, t.tx)
	if err != nil {
		return 0, 2, nil
	}
	return len(targets), 2, nil
}

// conditionLoaded is `loaded <x> <y> <z>`.
func conditionLoaded(t executeTest) (int, int, error) {
	if len(t.words) < 4 {
		return 0, 0, errExecuteSyntax
	}
	pos, err := t.blockPos(t.words[1:4])
	if err != nil {
		return 0, 0, err
	}
	_, loaded := t.tx.BlockLoaded(pos)
	return boolCount(loaded), 4, nil
}

// conditionBlock is `block <x> <y> <z> <block>`.
func conditionBlock(t executeTest) (int, int, error) {
	if len(t.words) < 5 {
		return 0, 0, errExecuteSyntax
	}
	pos, err := t.blockPos(t.words[1:4])
	if err != nil {
		return 0, 0, err
	}
	match, err := blockMatches(t.tx, pos, t.words[4])
	return boolCount(match), 5, err
}

// conditionDimension is `dimension <dimension>`.
func conditionDimension(t executeTest) (int, int, error) {
	if len(t.words) < 2 {
		return 0, 0, errExecuteSyntax
	}
	w, ok := dimensionWorld(t.words[1])
	return boolCount(ok && w == t.ctx.World), 2, nil
}

// conditionBiome is `biome <x> <y> <z> <biome>`.
func conditionBiome(t executeTest) (int, int, error) {
	if len(t.words) < 5 {
		return 0, 0, errExecuteSyntax
	}
	pos, err := t.blockPos(t.words[1:4])
	if err != nil {
		return 0, 0, err
	}
	want := strings.TrimPrefix(t.words[4], "minecraft:")
	return boolCount(world.JavaBiomeName(t.tx.Biome(pos)) == want), 5, nil
}

// conditionBlocks is `blocks <start> <end> <destination> (all|masked)`: the
// number of blocks of the region that the destination repeats, none if one
// differs.
func conditionBlocks(t executeTest) (int, int, error) {
	if len(t.words) < 11 {
		return 0, 0, errExecuteSyntax
	}
	var corners [3]cube.Pos
	for i := range corners {
		pos, err := t.blockPos(t.words[1+3*i : 4+3*i])
		if err != nil {
			return 0, 0, err
		}
		corners[i] = pos
	}
	lo, hi := cube.Min(corners[0], corners[1]), cube.Max(corners[0], corners[1])
	if (hi[0]-lo[0]+1)*(hi[1]-lo[1]+1)*(hi[2]-lo[2]+1) > t.tx.World().GameRuleInt("max_block_modifications") {
		return 0, 0, fmt.Errorf("too many blocks in the specified area")
	}
	return regionMatches(t.tx, lo, hi, corners[2], t.words[10] == "masked"), 11, nil
}

// regionMatches counts the blocks of the region from lo to hi that dest
// repeats, and is 0 if one of them differs. Air is skipped if masked.
func regionMatches(tx *world.Tx, lo, hi, dest cube.Pos, masked bool) int {
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
					return 0
				}
				count++
			}
		}
	}
	return count
}

// scoreComparisons are the operators of `score <a> <objective> <operator> <b>
// <objective>`.
var scoreComparisons = map[string]func(a, b int) bool{
	"=":  func(a, b int) bool { return a == b },
	">":  func(a, b int) bool { return a > b },
	"<":  func(a, b int) bool { return a < b },
	">=": func(a, b int) bool { return a >= b },
	"<=": func(a, b int) bool { return a <= b },
}

// conditionScore is `score <holder> <objective> matches <range>` and `score
// <holder> <objective> <operator> <holder> <objective>`.
func conditionScore(t executeTest) (int, int, error) {
	if len(t.words) < 5 {
		return 0, 0, errExecuteSyntax
	}
	m := t.tx.World().Scoreboard()
	holders, err := scoreHolders(t.words[1], t.at, t.tx, m)
	if err != nil || len(holders) != 1 {
		return 0, 0, errExecuteSyntax
	}
	value, exists := m.Score(t.words[2], holders[0])
	if t.words[3] == "matches" {
		return boolCount(exists && scoreRangeMatches(value, t.words[4])), 5, nil
	}
	if len(t.words) < 6 {
		return 0, 0, errExecuteSyntax
	}
	others, err := scoreHolders(t.words[4], t.at, t.tx, m)
	if err != nil || len(others) != 1 {
		return 0, 0, errExecuteSyntax
	}
	other, ok := m.Score(t.words[5], others[0])
	if !exists || !ok {
		return 0, 6, nil
	}
	compare, ok := scoreComparisons[t.words[3]]
	if !ok {
		return 0, 0, errExecuteSyntax
	}
	return boolCount(compare(value, other)), 6, nil
}
