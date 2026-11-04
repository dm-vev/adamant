package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// gameMaster, admin and owner are embedded in the runnables of the
// built-in commands to declare the level they need, as Java 26.3's
// commands require it. config.toml may override the level of any command
// by its name.
type (
	// gameMaster declares cmd.LevelGameMaster: the commands of the game.
	gameMaster struct{}
	// admin declares cmd.LevelAdmin: moderation and the lists.
	admin struct{}
	// owner declares cmd.LevelOwner: the server itself.
	owner struct{}
)

func (gameMaster) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }
func (admin) RequiredLevel() cmd.Level      { return cmd.LevelAdmin }
func (owner) RequiredLevel() cmd.Level      { return cmd.LevelOwner }

// worldOpCommand is the base of the runnables of the game that need the
// server: it declares cmd.LevelGameMaster.
type worldOpCommand struct{ srv serverAdapter }

func (worldOpCommand) RequiredLevel() cmd.Level { return cmd.LevelGameMaster }

// worldAdminCommand is a worldOpCommand that needs cmd.LevelAdmin, as
// Java's /tick does.
type worldAdminCommand struct{ worldOpCommand }

func (worldAdminCommand) RequiredLevel() cmd.Level { return cmd.LevelAdmin }
