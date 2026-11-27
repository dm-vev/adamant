package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/item/recipe"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

// registerPlayerExperienceCommands registers Java 26.3 experience and recipe commands.
func registerPlayerExperienceCommands(srv serverAdapter) {
	cmd.Register(newExperienceCommand())
	cmd.Register(newRecipeCommand())
}

type experienceAddCommand struct {
	gameMaster
	Add     cmd.SubCommand               `cmd:"add"`
	Targets cmd.Players                  `cmd:"targets"`
	Amount  int                          `cmd:"amount"`
	Unit    cmd.Optional[experienceUnit] `cmd:"unit"`
}
type experienceSetCommand struct {
	gameMaster
	Set     cmd.SubCommand               `cmd:"set"`
	Targets cmd.Players                  `cmd:"targets"`
	Amount  int                          `cmd:"amount"`
	Unit    cmd.Optional[experienceUnit] `cmd:"unit"`
}
type experienceQueryCommand struct {
	gameMaster
	Query  cmd.SubCommand `cmd:"query"`
	Target cmd.OnePlayer  `cmd:"target"`
	Unit   experienceUnit `cmd:"unit"`
}
type experienceUnit string

func (experienceUnit) Type() string                { return "experienceunit" }
func (experienceUnit) Options(cmd.Source) []string { return []string{"points", "levels"} }
func newExperienceCommand() cmd.Command {
	return newCommand("experience", []string{"xp"}, experienceAddCommand{}, experienceSetCommand{}, experienceQueryCommand{})
}
func (c experienceAddCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if c.Amount < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Amount)
		return
	}
	unit, _ := c.Unit.Load()
	amount := c.Amount
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			if unit == "levels" {
				p.SetExperienceLevel(p.ExperienceLevel() + amount)
			} else {
				p.AddExperience(amount)
			}
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Added %d %s to %s.", amount, unitName(unit), joinNonZeroNames(rs))
		},
	)
}
func (c experienceSetCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if c.Amount < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Amount)
		return
	}
	unit, _ := c.Unit.Load()
	amount := c.Amount
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			if unit == "levels" {
				p.SetExperienceLevel(amount)
			} else {
				p.RemoveExperience(p.Experience())
				p.AddExperience(amount)
			}
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Set experience for %s to %d %s.", joinNonZeroNames(rs), amount, unitName(unit))
		},
	)
}

type xpQueryResult struct {
	ok  bool
	val int
}

func (c experienceQueryCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	unit := c.Unit
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, t cmd.Target) xpQueryResult {
			p, ok := t.(*player.Player)
			if !ok {
				return xpQueryResult{}
			}
			n := p.Experience()
			if unit == "levels" {
				n = p.ExperienceLevel()
			}
			return xpQueryResult{ok: true, val: n}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[xpQueryResult]) {
			if len(rs) == 0 || !rs[0].Value.ok {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(rs[0].Value.val)
			o.Printf("%s has %d experience %s.", rs[0].Name, rs[0].Value.val, unitName(unit))
		},
	)
}
func unitName(u experienceUnit) string {
	if u == "levels" {
		return "levels"
	}
	return "points"
}

type recipeGiveCommand struct {
	gameMaster
	Give    cmd.SubCommand `cmd:"give"`
	Targets cmd.Players    `cmd:"targets"`
	Recipe  string         `cmd:"recipe"`
}
type recipeTakeCommand struct {
	gameMaster
	Take    cmd.SubCommand `cmd:"take"`
	Targets cmd.Players    `cmd:"targets"`
	Recipe  string         `cmd:"recipe"`
}

func newRecipeCommand() cmd.Command {
	return newCommand("recipe", nil, recipeGiveCommand{}, recipeTakeCommand{})
}
func (c recipeGiveCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	changeRecipes(src, o, tx, c.Targets, c.Recipe, true)
}
func (c recipeTakeCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	changeRecipes(src, o, tx, c.Targets, c.Recipe, false)
}

type recipeChangeResult struct {
	ok      bool
	changed int
}

func changeRecipes(src cmd.Source, o *cmd.Output, tx *world.Tx, targets cmd.Players, id string, give bool) {
	ids := []string{id}
	if id == "*" {
		ids = ids[:0]
		for _, r := range recipe.Recipes() {
			ids = append(ids, recipe.RecipeIDs(r)...)
		}
	}
	if id != "*" && !validRecipeID(id) {
		o.Errort(cmd.MessageParameterInvalid, id)
		return
	}
	verb := "unlocked"
	if !give {
		verb = "locked"
	}
	cmd.ForEach(src, o, tx, targets,
		func(_ *world.Tx, t cmd.Target) recipeChangeResult {
			p, ok := t.(*player.Player)
			if !ok {
				return recipeChangeResult{}
			}
			if give {
				before := len(p.UnlockedRecipeIDs())
				p.UnlockRecipes(ids)
				return recipeChangeResult{ok: true, changed: len(p.UnlockedRecipeIDs()) - before}
			}
			return recipeChangeResult{ok: true, changed: p.LockRecipes(ids)}
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[recipeChangeResult]) {
			var names []string
			changed := 0
			for _, r := range rs {
				if !r.Value.ok {
					continue
				}
				names = append(names, r.Name)
				changed += r.Value.changed
			}
			if len(names) == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(changed)
			o.Printf("%s %d recipes for %s.", verb, changed, joinSortedStrings(names))
		},
	)
}
func validRecipeID(id string) bool {
	return recipe.KnownID(id)
}
