package builtin

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player/chat"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// executeStep is one branch of an execute chain as its modifiers work on it:
// the source and the context they narrow and the words that are left of the
// chain. A modifier takes the words it needs off the front of words.
type executeStep struct {
	src       cmd.Source
	ctx       cmd.ExecutionContext
	eyeHeight float64
	stores    []func(success bool, result int)
	words     []string
	out       *cmd.Output
	tx        *world.Tx
	forks     *executeForks
}

type executeTargetSnap struct {
	isEntity  bool
	pos       mgl64.Vec3
	rot       cube.Rotation
	eyeHeight float64
	world     *world.World
}

// executeModifier works on the step at the keyword that is the first of its
// words, as one of Java's execute subcommands does. It reports whether the
// chain goes on with the words and the context the step has now: false means
// the modifier ran or scheduled the rest of the chain, or failed, and this
// loop ends.
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
// The caller must supply a tx belonging to ctx.World.
func executeChain(src cmd.Source, ctx cmd.ExecutionContext, eyeHeight float64, stores []func(bool, int), words []string, out *cmd.Output, tx *world.Tx, forks *executeForks) {
	s := &executeStep{src: src, ctx: ctx, eyeHeight: eyeHeight, stores: stores, words: words, out: out, tx: tx, forks: forks}
	for len(s.words) > 0 {
		modifier, ok := executeModifiers[s.words[0]]
		if !ok {
			s.errorf("Unknown execute subcommand '%s'", s.words[0])
			return
		}
		if !modifier(s) {
			return
		}
	}
	s.error("execute chain must end in run or a condition")
}

// at is the source with the context of the step.
func (s *executeStep) at() cmd.Source { return cmd.WithContext(s.src, s.ctx) }

func (s *executeStep) error(err any) {
	s.forks.mu.Lock()
	s.out.Error(err)
	s.forks.mu.Unlock()
}

func (s *executeStep) errorf(format string, a ...any) {
	s.forks.mu.Lock()
	s.out.Errorf(format, a...)
	s.forks.mu.Unlock()
}

func (s *executeStep) errort(t chat.Translation, a ...any) {
	s.forks.mu.Lock()
	s.out.Errort(t, a...)
	s.forks.mu.Unlock()
}

func (s *executeStep) printf(format string, a ...any) {
	s.forks.mu.Lock()
	s.out.Printf(format, a...)
	s.forks.mu.Unlock()
}

func (s *executeStep) setResult(v int) {
	s.forks.mu.Lock()
	s.out.SetResult(v)
	s.forks.mu.Unlock()
}

// forkTargets resolves selector across worlds via cmd.ForEach, admits the
// resulting branches against max_command_forks, and continues the chain for
// each branch in its resulting context world.
func (s *executeStep) forkTargets(
	selector string, silent bool, rest []string,
	narrow func(next *cmd.ExecutionContext, nextEye *float64, r cmd.Result[executeTargetSnap]) bool,
) bool {
	sel, err := cmd.ParseEntities(selector, s.at())
	if err != nil {
		if !silent {
			s.error(err)
		}
		return false
	}
	baseCtx := stripExecuteEntity(s.ctx)
	baseEye := s.eyeHeight
	stores := slices.Clone(s.stores)
	restCopy := slices.Clone(rest)
	remaining := len(s.words)
	forks := s.forks

	cmd.ForEach(s.at(), s.out, s.tx, sel,
		func(rtx *world.Tx, t cmd.Target) executeTargetSnap {
			snap := executeTargetSnap{pos: t.Position()}
			if rtx != nil {
				snap.world = rtx.World()
			}
			if e, ok := t.(world.Entity); ok {
				snap.isEntity = true
				snap.rot = e.Rotation()
			}
			if eyed, ok := t.(interface{ EyeHeight() float64 }); ok {
				snap.eyeHeight = eyed.EyeHeight()
			}
			return snap
		},
		func(nSrc cmd.Source, out *cmd.Output, tx *world.Tx, rs []cmd.Result[executeTargetSnap]) {
			if len(rs) == 0 {
				return
			}
			if !forks.admit(out, remaining, len(rs)) {
				return
			}
			baseSrc := unwrapContextSource(nSrc)
			for _, r := range rs {
				next := baseCtx
				nextEye := baseEye
				if !narrow(&next, &nextEye, r) {
					continue
				}
				nextClean := stripExecuteEntity(next)
				branchEye := nextEye
				cmd.InWorld(baseSrc, out, tx, nextClean.World, func(wSrc cmd.Source, wOut *cmd.Output, wTx *world.Tx) {
					executeChain(unwrapContextSource(wSrc), resolveExecuteCtx(nextClean, wTx), branchEye, stores, restCopy, wOut, wTx, forks)
				})
			}
		},
	)
	return false
}

// executeAs is `as <targets>`: the executor becomes each of the targets.
func executeAs(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	return s.forkTargets(s.words[1], false, s.words[2:], func(next *cmd.ExecutionContext, nextEye *float64, r cmd.Result[executeTargetSnap]) bool {
		if !r.Value.isEntity || r.Handle == nil {
			return false
		}
		next.Executor = nil
		next.ExecutorHandle = r.Handle
		*nextEye = r.Value.eyeHeight
		return true
	})
}

// executeAt is `at <targets>`: the position, rotation and dimension become
// those of each of the targets.
func executeAt(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	return s.forkTargets(s.words[1], false, s.words[2:], func(next *cmd.ExecutionContext, _ *float64, r cmd.Result[executeTargetSnap]) bool {
		if !r.Value.isEntity {
			return false
		}
		next.Position, next.Rotation = r.Value.pos, r.Value.rot
		if r.Value.world != nil {
			next.World = r.Value.world
		}
		return true
	})
}

// executePositioned is `positioned <x> <y> <z>` and `positioned as <targets>`.
func executePositioned(s *executeStep) bool {
	if len(s.words) >= 3 && s.words[1] == "as" {
		return s.forkTargets(s.words[2], true, s.words[3:], func(next *cmd.ExecutionContext, _ *float64, r cmd.Result[executeTargetSnap]) bool {
			next.Position = r.Value.pos
			return true
		})
	}
	if len(s.words) < 4 {
		s.error(errExecuteSyntax)
		return false
	}
	pos, err := cmd.ResolveCoordinates(s.words[1:4], s.at())
	if err != nil {
		s.error(err)
		return false
	}
	s.ctx.Position, s.ctx.Anchor = pos, cmd.Feet
	s.words = s.words[4:]
	return true
}

// executeRotated is `rotated <yaw> <pitch>` and `rotated as <targets>`.
func executeRotated(s *executeStep) bool {
	if len(s.words) >= 3 && s.words[1] == "as" {
		return s.forkTargets(s.words[2], true, s.words[3:], func(next *cmd.ExecutionContext, _ *float64, r cmd.Result[executeTargetSnap]) bool {
			if !r.Value.isEntity {
				return false
			}
			next.Rotation = r.Value.rot
			return true
		})
	}
	if len(s.words) < 3 {
		s.error(errExecuteSyntax)
		return false
	}
	yaw, pitch, err := parseRotation(s.words[1:3], s.ctx.Rotation)
	if err != nil {
		s.error(err)
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
		origin := anchorPosition(s.ctx, s.eyeHeight)
		return s.forkTargets(s.words[2], true, s.words[4:], func(next *cmd.ExecutionContext, _ *float64, r cmd.Result[executeTargetSnap]) bool {
			look := r.Value.pos
			if anchor == "eyes" {
				look[1] += r.Value.eyeHeight
			}
			next.Rotation = rotationTowards(origin, look)
			return true
		})
	}
	if len(s.words) < 4 {
		s.error(errExecuteSyntax)
		return false
	}
	pos, err := cmd.ResolveCoordinates(s.words[1:4], s.at())
	if err != nil {
		s.error(err)
		return false
	}
	s.ctx.Rotation = rotationTowards(anchorPosition(s.ctx, s.eyeHeight), pos)
	s.words = s.words[4:]
	return true
}

// executeAlign is `align <axes>`: it floors the position on the axes.
func executeAlign(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	for _, axis := range s.words[1] {
		if axis < 'x' || axis > 'z' {
			s.error("invalid align axis")
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
		s.error("execute anchored requires feet or eyes")
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
		s.error(errExecuteSyntax)
		return false
	}
	target, ok := dimensionWorld(s.words[1])
	if !ok {
		s.errorf("Unknown dimension '%s'", s.words[1])
		return false
	}
	if target == s.ctx.World && s.tx != nil && s.tx.World() == target {
		s.words = s.words[2:]
		return true
	}
	// CommandSourceStack.withLevel scales X/Z by the coordinate scales.
	scale := dimensionScale(s.ctx.World.Dimension()) / dimensionScale(target.Dimension())
	next := stripExecuteEntity(s.ctx)
	next.Position[0], next.Position[2] = next.Position[0]*scale, next.Position[2]*scale
	next.World = target
	rest := slices.Clone(s.words[2:])
	stores := slices.Clone(s.stores)
	eyeHeight := s.eyeHeight
	forks := s.forks
	cmd.InWorld(s.src, s.out, s.tx, target, func(wSrc cmd.Source, wOut *cmd.Output, wTx *world.Tx) {
		executeChain(unwrapContextSource(wSrc), resolveExecuteCtx(next, wTx), eyeHeight, stores, rest, wOut, wTx, forks)
	})
	return false
}

type relatedHandleSnap struct {
	handle    *world.EntityHandle
	eyeHeight float64
}

// executeOn is `on <relation>`: the executor becomes each of the entities
// that stand in the relation to it.
func executeOn(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	relation := s.words[1]
	rest := slices.Clone(s.words[2:])
	if s.ctx.Executor != nil {
		related := relatedEntities(s.ctx.Executor, relation, s.tx)
		if !s.forks.admit(s.out, len(s.words), len(related)) {
			return false
		}
		for _, e := range related {
			next := s.ctx
			next.Executor, next.ExecutorHandle = e, e.H()
			executeChain(s.src, next, executorEyeHeight(e), s.stores, rest, s.out, s.tx, s.forks)
		}
		return false
	}
	if s.ctx.ExecutorHandle == nil {
		return false
	}
	sel, err := cmd.ParseEntities("@s", s.at())
	if err != nil {
		return false
	}
	baseCtx := stripExecuteEntity(s.ctx)
	stores := slices.Clone(s.stores)
	remaining := len(s.words)
	forks := s.forks
	cmd.ForEach(s.at(), s.out, s.tx, sel,
		func(rtx *world.Tx, t cmd.Target) []relatedHandleSnap {
			e, ok := t.(world.Entity)
			if !ok {
				return nil
			}
			rel := relatedEntities(e, relation, rtx)
			snaps := make([]relatedHandleSnap, len(rel))
			for i, re := range rel {
				snaps[i] = relatedHandleSnap{handle: re.H(), eyeHeight: executorEyeHeight(re)}
			}
			return snaps
		},
		func(nSrc cmd.Source, out *cmd.Output, tx *world.Tx, rs []cmd.Result[[]relatedHandleSnap]) {
			if len(rs) == 0 || len(rs[0].Value) == 0 {
				return
			}
			snaps := rs[0].Value
			if !forks.admit(out, remaining, len(snaps)) {
				return
			}
			baseSrc := unwrapContextSource(nSrc)
			for _, snap := range snaps {
				next := baseCtx
				next.Executor, next.ExecutorHandle = nil, snap.handle
				executeChain(baseSrc, resolveExecuteCtx(next, tx), snap.eyeHeight, stores, rest, out, tx, forks)
			}
		},
	)
	return false
}

// executeSummon is `summon <type>`: the executor becomes a new entity.
func executeSummon(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	spawn := s.tx.World().EntityRegistry().Config().SpawnMob
	id := s.words[1]
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	if spawn == nil {
		s.errorf("Unable to summon entity")
		return false
	}
	h := spawn(id, world.EntitySpawnOpts{Position: s.ctx.Position, SpawnReason: "command"})
	if h == nil {
		s.errorf("Unable to summon entity")
		return false
	}
	ent := s.tx.AddEntity(h)
	s.ctx.Executor, s.ctx.ExecutorHandle = ent, ent.H()
	s.eyeHeight = executorEyeHeight(ent)
	s.words = s.words[2:]
	return true
}

// executeStore is `store (result|success) <destination>`: the output of what
// the chain ends in is stored.
func executeStore(s *executeStep) bool {
	baseCtx := stripExecuteEntity(s.ctx)
	eyeHeight := s.eyeHeight
	stores := slices.Clone(s.stores)
	words := slices.Clone(s.words)
	forks := s.forks
	out := s.out
	resolveExecuteStoreSink(words[1:], s.src, s.ctx, s.out, s.tx, func(sink func(bool, int), used int, err error, nextSrc cmd.Source, nextTx *world.Tx) {
		if err != nil {
			forks.mu.Lock()
			out.Error(err)
			forks.mu.Unlock()
			return
		}
		nextStores := append(slices.Clone(stores), sink)
		executeChain(nextSrc, resolveExecuteCtx(baseCtx, nextTx), eyeHeight, nextStores, words[1+used:], out, nextTx, forks)
	})
	return false
}

// executeConditional is `if <condition>` and, for unless, `unless
// <condition>`. A condition that ends the chain reports its result as Java
// does, and one that fails ends the branch.
func executeConditional(s *executeStep, unless bool) bool {
	if len(s.words) < 2 {
		s.error(errExecuteSyntax)
		return false
	}
	condWords := s.words[1:]
	switch condWords[0] {
	case "entity":
		return executeConditionalEntity(s, unless)
	case "score":
		return executeConditionalScore(s, unless)
	}
	count, used, err := executeCondition(condWords, s.src, s.ctx, s.tx)
	if err != nil {
		s.error(err)
		return false
	}
	return finishConditionalSync(s, unless, count, s.words[1+used:])
}

func finishConditionalSync(s *executeStep, unless bool, count int, rest []string) bool {
	pass := count > 0
	if unless {
		pass, count = !pass, 1
	}
	s.words = rest
	if len(rest) == 0 {
		if !pass {
			s.error("Test failed")
			return false
		}
		s.setResult(count)
		s.printf("Test passed")
		for i := len(s.stores) - 1; i >= 0; i-- {
			s.stores[i](true, count)
		}
		return false
	}
	return pass
}

func executeConditionalEntity(s *executeStep, unless bool) bool {
	if len(s.words) < 3 {
		s.error(errExecuteSyntax)
		return false
	}
	rest := slices.Clone(s.words[3:])
	sel, err := cmd.ParseEntities(s.words[2], s.at())
	if err != nil {
		return finishConditionalSync(s, unless, 0, rest)
	}
	baseCtx := stripExecuteEntity(s.ctx)
	eyeHeight := s.eyeHeight
	stores := slices.Clone(s.stores)
	forks := s.forks
	cmd.ForEach(s.at(), s.out, s.tx, sel,
		func(_ *world.Tx, _ cmd.Target) int { return 1 },
		func(nSrc cmd.Source, out *cmd.Output, tx *world.Tx, rs []cmd.Result[int]) {
			step := &executeStep{
				src: unwrapContextSource(nSrc), ctx: resolveExecuteCtx(baseCtx, tx),
				eyeHeight: eyeHeight, stores: stores, out: out, tx: tx, forks: forks,
			}
			if finishConditionalSync(step, unless, len(rs), rest) {
				executeChain(step.src, step.ctx, step.eyeHeight, step.stores, step.words, out, tx, forks)
			}
		},
	)
	return false
}

func executeConditionalScore(s *executeStep, unless bool) bool {
	words := s.words[1:]
	if len(words) < 5 {
		s.error(errExecuteSyntax)
		return false
	}
	m := s.tx.World().Scoreboard()
	baseCtx := stripExecuteEntity(s.ctx)
	eyeHeight := s.eyeHeight
	stores := slices.Clone(s.stores)
	forks := s.forks
	wordsCopy := slices.Clone(words)
	resolveScoreHolders(wordsCopy[1], s.at(), s.out, s.tx, m, func(nSrc cmd.Source, out *cmd.Output, tx *world.Tx, holders []string, err error) {
		step := &executeStep{
			src: unwrapContextSource(nSrc), ctx: resolveExecuteCtx(baseCtx, tx),
			eyeHeight: eyeHeight, stores: stores, out: out, tx: tx, forks: forks,
		}
		if err != nil || len(holders) != 1 {
			step.error(errExecuteSyntax)
			return
		}
		value, exists := m.Score(wordsCopy[2], holders[0])
		if wordsCopy[3] == "matches" {
			count := boolCount(exists && scoreRangeMatches(value, wordsCopy[4]))
			if finishConditionalSync(step, unless, count, wordsCopy[5:]) {
				executeChain(step.src, step.ctx, step.eyeHeight, step.stores, step.words, out, tx, forks)
			}
			return
		}
		if len(wordsCopy) < 6 {
			step.error(errExecuteSyntax)
			return
		}
		compare, ok := scoreComparisons[wordsCopy[3]]
		if !ok {
			step.error(errExecuteSyntax)
			return
		}
		resolveScoreHolders(wordsCopy[4], step.at(), out, tx, m, func(nSrc2 cmd.Source, out2 *cmd.Output, tx2 *world.Tx, others []string, err2 error) {
			step2 := &executeStep{
				src: unwrapContextSource(nSrc2), ctx: resolveExecuteCtx(baseCtx, tx2),
				eyeHeight: eyeHeight, stores: stores, out: out2, tx: tx2, forks: forks,
			}
			if err2 != nil || len(others) != 1 {
				step2.error(errExecuteSyntax)
				return
			}
			other, hasOther := m.Score(wordsCopy[5], others[0])
			count := boolCount(exists && hasOther && compare(value, other))
			if finishConditionalSync(step2, unless, count, wordsCopy[6:]) {
				executeChain(step2.src, step2.ctx, step2.eyeHeight, step2.stores, step2.words, out2, tx2, forks)
			}
		})
	})
	return false
}

// executeRun is `run <command>`: the command runs in the context world's
// transaction with the context and store interceptors of the step.
func executeRun(s *executeStep) bool {
	if len(s.words) < 2 {
		s.error("execute run requires a command")
		return false
	}
	command, ok := cmd.ByAlias(strings.TrimPrefix(s.words[1], "/"))
	if !ok {
		s.errort(cmd.MessageUnknown, s.words[1])
		return false
	}
	line := strings.Join(s.words[2:], " ")
	runSrc := s.src
	for _, st := range s.stores {
		runSrc = &storeSource{Source: runSrc, store: st}
	}
	command.Execute(line, cmd.WithContext(runSrc, s.ctx), s.tx)
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

// executeConditions are the synchronous predicates of if and unless by keyword.
var executeConditions = map[string]executeConditionFn{
	"loaded":    conditionLoaded,
	"block":     conditionBlock,
	"dimension": conditionDimension,
	"biome":     conditionBiome,
	"blocks":    conditionBlocks,
}

// executeCondition evaluates a synchronous if/unless predicate and returns
// Java's match count and the words consumed.
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
	return boolCount(world.CanonicalBiomeName(t.tx.Biome(pos)) == want), 5, nil
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
