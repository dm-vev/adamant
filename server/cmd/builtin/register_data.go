package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerDataCommands registers Java 26.3 execute, data, item, loot and scheduling commands.
func registerDataCommands(srv serverAdapter) {
	cmd.Register(newCommand("execute", nil, executeCommand{}))
	cmd.Register(newCommand("data", nil, dataCommand{}))
	cmd.Register(newLootCommand())
	cmd.Register(newReturnCommand())
	cmd.Register(newItemCommand())
	_ = srv
}
