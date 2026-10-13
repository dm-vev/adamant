package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// registerPlayerExperienceCommands registers Java 26.3 experience and recipe commands.
func registerPlayerExperienceCommands(srv serverAdapter) {
	cmd.Register(newExperienceCommand())
	cmd.Register(newRecipeCommand())
}

type experienceAddCommand struct {
	opGate
	Add     cmd.SubCommand               `cmd:"add"`
	Targets []cmd.Target                 `cmd:"targets"`
	Amount  int                          `cmd:"amount"`
	Unit    cmd.Optional[experienceUnit] `cmd:"unit"`
}
type experienceSetCommand struct {
	opGate
	Set     cmd.SubCommand               `cmd:"set"`
	Targets []cmd.Target                 `cmd:"targets"`
	Amount  int                          `cmd:"amount"`
	Unit    cmd.Optional[experienceUnit] `cmd:"unit"`
}
type experienceQueryCommand struct {
	opGate
	Query  cmd.SubCommand `cmd:"query"`
	Target []cmd.Target   `cmd:"target"`
	Unit   experienceUnit `cmd:"unit"`
}
type experienceUnit string

func (experienceUnit) Type() string                { return "experienceunit" }
func (experienceUnit) Options(cmd.Source) []string { return []string{"points", "levels"} }
func newExperienceCommand() cmd.Command {
	return cmd.New("experience", "Adds, sets, or queries player experience.", []string{"xp"}, experienceAddCommand{}, experienceSetCommand{}, experienceQueryCommand{})
}
func (c experienceAddCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ps := playersFromTargets(c.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if c.Amount < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Amount)
		return
	}
	unit, _ := c.Unit.Load()
	for _, p := range ps {
		if unit == "levels" {
			p.SetExperienceLevel(p.ExperienceLevel() + c.Amount)
		} else {
			p.AddExperience(c.Amount)
		}
	}
	o.Printf("Added %d %s to %s.", c.Amount, unitName(unit), joinNames(ps))
}
func (c experienceSetCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ps := playersFromTargets(c.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	if c.Amount < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Amount)
		return
	}
	unit, _ := c.Unit.Load()
	for _, p := range ps {
		if unit == "levels" {
			p.SetExperienceLevel(c.Amount)
		} else {
			p.RemoveExperience(p.Experience())
			p.AddExperience(c.Amount)
		}
	}
	o.Printf("Set experience for %s to %d %s.", joinNames(ps), c.Amount, unitName(unit))
}
func (c experienceQueryCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	p, ok := target.(*player.Player)
	if !ok {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	n := p.Experience()
	if c.Unit == "levels" {
		n = p.ExperienceLevel()
	}
	o.SetResult(n)
	o.Printf("%s has %d experience %s.", p.Name(), n, unitName(c.Unit))
}
func unitName(u experienceUnit) string {
	if u == "levels" {
		return "levels"
	}
	return "points"
}

type recipeGiveCommand struct {
	opGate
	Give    cmd.SubCommand `cmd:"give"`
	Targets []cmd.Target   `cmd:"targets"`
	Recipe  string         `cmd:"recipe"`
}
type recipeTakeCommand struct {
	opGate
	Take    cmd.SubCommand `cmd:"take"`
	Targets []cmd.Target   `cmd:"targets"`
	Recipe  string         `cmd:"recipe"`
}

func newRecipeCommand() cmd.Command {
	return cmd.New("recipe", "Gives or takes recipes.", nil, recipeGiveCommand{}, recipeTakeCommand{})
}
func (c recipeGiveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	changeRecipes(c.Targets, c.Recipe, true, o)
}
func (c recipeTakeCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	changeRecipes(c.Targets, c.Recipe, false, o)
}
func changeRecipes(targets []cmd.Target, id string, give bool, o *cmd.Output) {
	ps := playersFromTargets(targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	ids := []string{id}
	if id == "*" {
		ids = ids[:0]
		for _, r := range recipe.Recipes() {
			ids = append(ids, recipe.JavaRecipeIDs(r)...)
		}
	}
	if id != "*" && !validRecipeID(id) {
		o.Errort(cmd.MessageParameterInvalid, id)
		return
	}
	changed := 0
	for _, p := range ps {
		if give {
			before := len(p.UnlockedRecipeIDs())
			p.UnlockRecipes(ids)
			changed += len(p.UnlockedRecipeIDs()) - before
		} else {
			changed += p.LockRecipes(ids)
		}
	}
	verb := "unlocked"
	if !give {
		verb = "locked"
	}
	o.Printf("%s %d recipes for %s.", verb, changed, joinNames(ps))
}
func validRecipeID(id string) bool {
	for _, r := range recipe.Recipes() {
		for _, candidate := range recipe.JavaRecipeIDs(r) {
			if candidate == id {
				return true
			}
		}
	}
	return false
}
